# CLAUDE.md

## Project overview

TMI is a Go service implementing the REST API and store for a security review process, from request (intake) through analysis and followup, centered on threat modeling with collaborative data flow diagrams. Artifacts can be created, read, or updated by machines or humans interchangeably, and the service is designed to be integrated with and extended without code changes. The REST API is defined by an OpenAPI 3 spec (`api-schema/tmi-openapi.json`), which is the source of truth; real-time collaborative diagram editing runs over WebSockets, defined by an AsyncAPI spec. Auth is OAuth or SAML with JWT, RBAC assigns roles to users or groups, and persistence is via GORM.

Sibling projects (client, wiki, etc.) are registered in `.local/repos.json`; check it for local paths before fetching from GitHub.

## Code structure

- `api/` — handlers, server, storage: `api/server.go` (OpenAPI server with single router and WebSocket support), `api/store.go` (generic `Store[T]`), `api/websocket.go` (hub), `api/*_middleware.go` (resource authorization), `api/request_tracing.go` (module-tagged request logging), `api/cache_service.go` (Redis caching with invalidation, warming, metrics)
- `auth/` — OAuth, JWT, RBAC; `auth/handlers.go` exposes auth endpoints via the auth service adapter
- `cmd/` — `server`, `migrate`, `dbtool`; worker/extraction subsystem (`chunkembed`, `extractor`, `component-controller`, `worker-probe`); config generators (`genconfig`, `genconfigdocs`)
- `internal/` — `slogging`, `config`, `crypto`, `dberrors`, `dbschema`, `otel`, `platform`, `worker`, `secrets`, ...
- `docs/` — deprecated (see Documentation); `scripts/` — dev setup scripts; `Makefile` — all build/dev targets

### Storage

Use the generic `Store[T]` from `api/store.go`; each entity type has its own store instance (DiagramStore, ThreatModelStore) with CRUD and concurrency control. Validate entity fields before storing; use the `WithTimestamps` interface for entities with `created_at`/`modified_at`.

PostgreSQL for persistence, Redis for caching and sessions, in-memory storage for tests. Schema is managed by GORM `AutoMigrate()` from struct tags in `api/models/*.go` (single source of truth). **Oracle ADB is a supported production target**; any DB-touching change must be reviewed by the `oracle-db-admin` subagent (see Policies).

### WebSocket

Real-time collaboration at `/ws/diagrams/{id}` (diagrams only, not threat models) over Gorilla WebSocket. `WebSocketHub` manages connections and broadcasts. Sessions go Active → Terminating → Terminated; only the host manages participants; inactivity timeout is configurable (default 300s, minimum 15s); removed participants go on a session deny list.

### OpenAPI and code generation

- `make generate-api` runs oapi-codegen v2 (`oapi-codegen-config.yml`, gin-middleware package) to produce `api/api.go`: Gin server handlers and the embedded spec. Gin, not Echo.
- OpenAPI validation middleware clears security schemes; auth is the JWT middleware's job.
- `make validate-openapi` (jq + Vacuum with OWASP rules) writes `api-schema/openapi-validation-report.json`. `make validate-asyncapi` for the WS spec.
- Public endpoints (OAuth, OIDC, SAML) carry the `x-public-endpoint` vendor extension and are intentionally unauthenticated per their RFCs.

**Discriminator union type safety — CRITICAL:** never call the generated `FromNode`, `MergeNode`, `FromMinimalNode`, or `MergeMinimalNode` in non-generated code. They hardcode the `shape` discriminator to one fixed value and corrupt cell shapes (e.g. every node becomes `text-box`), an oapi-codegen limitation when several discriminator values map to one type. Use `SafeFromNode()` / `SafeFromEdge()` from `api/cell_union_helpers.go`. `FromEdge`/`MergeEdge` (one edge shape, `flow`) and `FromDfdDiagram` (one type, `DFD-1.0.0`) are safe. Affected unions: `DfdDiagram_Cells_Item`, `DfdDiagramInput_Cells_Item`, `MinimalCell`. `make check-unsafe-union-methods` enforces this (part of `make lint`).

### Request flow

All HTTP requests route through the OpenAPI spec (single router, single source of truth, no duplicate-registration panics):

```
HTTP Request -> OpenAPI Route Registration -> ServerInterface Implementation ->
JWT Middleware -> Auth Context -> Resource Middleware (ThreatModel/Diagram) -> Endpoint Handlers
```

## Commands

**MANDATORY: always use Make targets.** Never run `go run`, `go test`, `./bin/tmiserver`, `docker run`, or `docker exec` directly; the targets carry the required environment. `make list-targets` lists everything.

- Build: `make build-server` → `bin/tmiserver`. Lint: `make lint` (golangci-lint). Generate API: `make generate-api`.
- Dev environment: `make dev-up` deploys the full stack into a local Kubernetes cluster (`CLUSTER=docker-desktop` default, `CLUSTER=k3s` supported) in the `tmi-platform` namespace: server and Redis as Deployments, PostgreSQL and NATS as StatefulSets; server at localhost:8080. `DB=oracle` uses an external managed Oracle ADB instead. `make dev-down` (keeps DB data), `make dev-status`, `make dev-reset`/`make dev-nuke` (soft/hard reset), `make clean-everything`. Orchestration is `scripts/devenv.py`; manifests are under `deployments/k8s/dev/<cluster>/`. **PostgreSQL data lives in a Kubernetes PVC (`data-postgres-0`)**, not a host Docker volume; re-provisioning the PVC starts from an empty database.
- Health check: `curl http://localhost:8080/` (root endpoint; **there is no /health**).
- Config: `.env.dev` and `config-development.yaml`; `config-example.yml` (generated by `cmd/genconfig`) is the annotated template. Local Postgres credentials are under `database.postgres.*` in `config-development.yaml` (mirrored as `POSTGRES_*` in `.env.dev`). Dev/test logs go to `logs/tmi.log`. TLS is configurable.

### Containers

`scripts/build-app-containers.py` and `scripts/build-db-containers.py`, wrapped by make: `make build-server-container`, `make build-redis-container`, `make build-db`, `make build-all`, `make build-all-scan`, `make scan-containers`, `make start-containers-environment`; cloud build+push+scan via `make build-app-{oci,aws,azure,gcp,heroku}`. Use `make start-database`, `make start-redis`, `make dev-up` for container operations.

Local/generic builds use Chainguard images (`cgr.dev/chainguard/static`, `cgr.dev/chainguard/postgres`, Chainguard Redis) with `CGO_ENABLED=0` (~57MB total). OCI builds use Oracle Linux 9 with Oracle Instant Client for ADB.

- **SBOM:** `make generate-sbom` (cyclonedx-gomod) for the Go app; containers get one automatically with `--scan` (Syft). Output: `security-reports/sbom/` (CycloneDX 1.6 JSON + XML).
- **Arazzo:** `make generate-arazzo` / `make validate-arazzo` → `api-schema/tmi.arazzo.{yaml,json}`; docs in `api-schema/arazzo-generation.md`.

### Heroku (DESTRUCTIVE; both require a manual "yes")

- `make reset-db-heroku` (`scripts/heroku-reset-database.sh`) — drop schema → run migrations → verify critical columns (e.g. `issue_uri` in `threat_models`). For schema drift, migration errors, clean-deploy testing. Users must re-authenticate afterwards.
- `make drop-db-heroku` (`scripts/heroku-drop-database.sh`) — drop schema only, leaving an empty `public` schema for manual schema work or migration testing. Restore with `make reset-db-heroku` or restart the app to auto-migrate.

## Testing

**Never disable or skip failing tests; find and fix the root cause.**

- Unit: `make test-unit` (`name=TestName` for one test; options `count1=true passfail=true`)
- Integration: `make test-integration` / `make test-integration-pg` (PostgreSQL); `make test-integration-oci` (Oracle ADB, needs `scripts/oci-env.sh`)
- Coverage: `make test-coverage`

### CATS API fuzzing

CATS fuzzes the API via the portable `cats@efitz-skills` plugin, configured per repo in `.local/cats/config.yaml` (gitignored). `make cats-fuzz` / `/cats:run`, `make analyze-cats-results` / `/cats:analyze`, `make cats-report` / `/cats:report`, `/cats:fp` for false-positive rules.

- **The make targets and `/cats:*` skills are the same engine.** The Makefile resolves `CATS_TOOL` to the installed plugin, falling back to a `~/Projects/skills/cats` dev checkout (what the skills use via `${CLAUDE_PLUGIN_ROOT}`). Never hardcode either path; `make ... CATS_TOOL=/path/to/cats_tool.py` overrides. Two copies of the run-validity gates disagreeing is exactly the failure the gates exist to catch.
- **Identity:** comes from `identities:` in the config (`token_cmd` prints a bearer token), selected with `--identity <name>` on the plugin or `/cats:run`, or by setting the default identity for `make cats-fuzz`. `CATS_USER`/`CATS_SERVER`/`CATS_PROVIDER` control only `make cats-seed`.
- **Output:** `test/results/cats/` — one SQLite database per run, `latest.db` → most recent completed run. Analyze by querying SQLite; don't read the HTML or JSON.
- **Fuzz the cluster directly** (`http://rp2:30080`, the k3s-rp NodePort), never through `kubectl port-forward`: a port-forwarded campaign loses ~46% of requests to connection errors that the `CONNECTION_ERROR_999` rule absorbs, so it looks clean while most of the API was never reached (#463/#578). The plugin refuses such a run at preflight (`--allow-port-forward` overrides).
- **Run-validity gates** (a failing run exits 3 and never becomes `latest.db`): transport-error rate (`max_connection_error_pct`, default 1%) and non-false-positive 401 rate (`max_unauthenticated_pct`, default 5%). The second catches a campaign that revokes its own token by fuzzing a logout endpoint and then runs unauthenticated while reporting complete (#591). Such endpoints belong in `cats.skip_paths` (TMI skips `/me/logout`) and can be fuzzed alone with `run --path`.
- Configs must send `If-Match: *` (`cats.headers` in the config, a first-class key since #599) so optimistic-locking preconditions pass instead of tripping the `If-Match` schema (#581).
- **Seeding** runs over loopback (`http://localhost:8080` behind a port-forward), not the NodePort, because macOS can block a freshly built unsigned Go binary such as `tmi-dbtool` from opening TCP connections to a LAN host (#595). Both the `tmi-server` and `postgres` port-forwards must be up before `make cats-seed` or the plugin's seed hook.
- **False positives:** public (21) and cacheable (7) endpoints use `x-public-endpoint` / `x-cacheable-endpoint` to skip inapplicable fuzzers. The rest (e.g. OAuth 401/403) are classified by `test/cats/false-positives.yaml` (file order, first match wins; see `test/cats/README.md`) into the results DB's `is_false_positive` column. Manage rules through `/cats:fp`, not by editing Python; the legacy `detect_false_positive()` no longer exists.

### OAuth callback stub

OAuth 2.0 + PKCE test harness for manual and automated flows (`scripts/oauth-client-callback-stub.py`; logs at `/tmp/oauth-stub.log`). Always use a normal OAuth login with the `tmi` provider for any dev or test task needing auth. `make start-oauth-stub` / `make stop-oauth-stub`.

| Endpoint | Purpose |
|----------|---------|
| `POST /oauth/init` | Initialize OAuth flow, returns authorization URL |
| `POST /flows/start` | Start automated e2e flow, returns flow_id |
| `GET /flows/{id}` | Poll flow status and retrieve tokens |
| `GET /creds?userid=X` | Retrieve saved credentials for user |
| `POST /refresh` | Refresh access token |

```bash
make start-oauth-stub
curl -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid": "alice"}'
curl "http://localhost:8079/creds?userid=alice" | jq '.access_token'   # after the flow completes
```

By convention `charlie` is the administrator user; `alice`, `bob`, etc. are ordinary users.

### WebSocket test harness

`wstest/` is a standalone Go app: `make wstest`, `make monitor-wstest`.

```bash
./wstest --user alice --host --participants "bob,charlie"  # host
./wstest --user bob                                        # participant
```

## Authentication

**login_hint** on `/oauth2/authorize?idp=tmi` gives predictable test users (TMI provider only, not in production builds). Format `^[a-zA-Z0-9-]{3,20}$`, case-insensitive.

```bash
curl "http://localhost:8080/oauth2/authorize?idp=tmi&login_hint=alice"     # alice@tmi.local, "Alice (TMI User)"
curl "http://localhost:8080/oauth2/authorize?idp=tmi"                      # random testuser-12345678@tmi.local
curl "http://localhost:8080/oauth2/authorize?idp=tmi&login_hint=alice&client_callback=http://localhost:8079/"
```

**Client credentials grant** (RFC 6749 §4.4) for webhooks, addons, automation. Like GitHub PATs: the secret is shown once at creation; the token acts as the creating user **except on `/admin/*`**, where service-account tokens are denied (403) — admin operations require interactive PKCE auth (#399). Client IDs are `tmi_cc_*`, secrets bcrypt-hashed, tokens live 1 hour with JWT subject `sa:{id}:{owner}`.

| Endpoint | Purpose |
|----------|---------|
| `POST /me/client_credentials` | Create credential (returns secret once) |
| `GET /me/client_credentials` | List credentials (no secrets) |
| `DELETE /me/client_credentials/{id}` | Delete and revoke credential |

```bash
curl -X POST http://localhost:8080/oauth2/token \
  -d "grant_type=client_credentials" -d "client_id=tmi_cc_..." -d "client_secret=..."
# {"access_token": "...", "token_type": "Bearer", "expires_in": 3600}
```

## Policies

**Zero 500 errors.** Every 500 found in any testing (unit, integration, API, CATS) is investigated and fixed before release: file a `bug` issue in the current milestone and replace the unhandled condition with the right 4xx or graceful handling. Never dismiss one as an edge case or fuzzer artifact; if the server can return 500, it will in production.

**Documented status codes only.** The server must never return a status code the OpenAPI spec doesn't document for that operation, including 4xx. If a handler legitimately needs a new code, add it to the spec and regenerate; that is expected and encouraged. CATS "undocumented response code" true positives are handled like 500s.

**Client bug triage.** If the root cause of a problem is in the client ([tmi-ux](https://github.com/ericfitz/tmi-ux)) rather than the server — the server follows the spec but the client mishandles the response, sends malformed requests, misuses the auth flow, or mishandles documented errors — stop, explain the evidence, and ask: "This appears to be a client bug. Would you like me to file a bug against tmi-ux?" If confirmed, file it with the `/file-client-bug` skill, then resume remaining server work or report the task blocked.

**Oracle compatibility review.** PostgreSQL in development, Oracle ADB in production; they diverge subtly (cascade semantics, identifier limits, types, error codes, isolation, upsert syntax) and Oracle-only bugs are expensive. Any change that can affect Oracle must be reviewed by the **`oracle-db-admin` subagent** (invoke the `oracle-db-admin` skill; definition in `.claude/agents/oracle-db-admin.md`) before the task is complete. What counts: migrations, GORM models/tags, `*_repository.go`, `*_store_gorm.go`, raw SQL, transaction/locking patterns, FKs/cascades, JSON/CLOB handling, retry logic, `internal/dberrors/`, schema-affecting config; when in doubt, dispatch. A **minor or patch** bump of `github.com/godror/godror` or any other DB driver does **not** need review (no TMI code changes; normal gates still apply); a **major** bump, or any bump with accompanying code changes, does. Verdicts: `APPROVED` (proceed, note it in the summary); `APPROVED WITH NOTES` (fix the easy items now, file follow-ups); `BLOCKING ISSUES` (fix every item, or get an explicit user waiver with reasoning). Don't argue with findings in your own head; if one seems wrong, ask the user to adjudicate.

## Task completion checklist

1. `make lint`
2. If `api-schema/tmi-openapi.json` changed: `make validate-openapi`, then `make generate-api`
3. If any Go file changed (including regenerated `api/api.go`): `make build-server`, `make test-unit`, and `make test-integration` for API functionality. Not required when only non-Go files changed.
4. If DB-touching code changed: Oracle review (above); address every BLOCKING finding first
5. If the schema changed: update `cmd/dbtool/` to match
6. Suggest a conventional commit message
7. If tied to a GitHub issue: the resolving commit references it (`Fixes #123` / `Closes #123` in the body) and the issue is closed as done (manually, if the commit was not directly to main)

## Guidelines

**Go:** gofmt; imports grouped stdlib / external / internal; check errors and return them with context; prefer interfaces over concrete types; godoc on all exported symbols; structure by domain (auth, diagrams, threats).

**Logging — CRITICAL:** never import the standard `log` package or use print-based logging (`fmt.Println`). Always use `github.com/ericfitz/tmi/internal/slogging`: `slogging.Get()` globally or `slogging.Get().WithContext(c)` per request; levels `Debug/Info/Warn/Error`. For fatal startup errors, `slogging.Get().Error()` then `os.Exit(1)` instead of `log.Fatalf()`.

**Staticcheck:** `api/api.go` is generated and carries many expected ST1005 warnings (capitalized error strings); ignore them (`staticcheck ./... | grep -v "api/api.go"`). All expected issues are in that file.

**API design:** OpenAPI 3.0.3; `snake_case` JSON properties, path segments, and parameters (RFC-mandated `kebab-case` only under `/.well-known/`); `PascalCase` schema names; `camelCase` operation IDs; `Title Case` tags. Describe every property and endpoint; document error responses (401, 403, 404); UUID IDs, ISO8601 timestamps; reader/writer/owner roles; bearer JWT; JSON Patch for partial updates; limit/offset pagination.

**URL patterns** — pick the first that applies:

| Pattern | Authorization | Example | When |
|---|---|---|---|
| **Protocol** | Per RFC | `/oauth2/token`, `/.well-known/jwks.json` | Auth/identity endpoints defined by external standards |
| **User-scoped** | Current user | `/me/preferences` | Personal resources for the requester |
| **Resource-hierarchical** | Parent ownership (readers/writers/owners) | `/threat_models/{id}/assets/{id}` | A parent resource controls access |
| **Domain-segregated** | Workflow stage | `/admin/surveys/{id}`, `/intake/surveys/{id}` | Same resource, different capabilities per stage (default prefixes `/admin/`, `/intake/`, `/triage/`; new prefix only when the workflow doesn't fit) |
| **Cross-cutting** | Ownership + admin | `/projects/{id}`, `/addons/{id}` | Top-level resources with their own access control |

Key question: does authorization flow from a parent entity (hierarchical) or from workflow context (domain-segregated)?

## Git and versioning

**Conventional commits:** `<type>(<scope>): <description>`, imperative mood. Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`, `ci`, `build`, `revert`, `deps`. Scope optional (`api`, `auth`, `websocket`). Examples: `feat(api): add WebSocket heartbeat mechanism`, `fix(auth): correct JWT token expiration validation`, `deps: update Gin framework to v1.11.0`.

**Branching:** feature work happens in `dev/<semver>/<feature-name>` branches or `feature/<feature-name>` children of `dev/<semver>`. `main` only gets direct commits for patches, security fixes, and release merges.

**Versioning is automatic, per PR** (#627). `.github/workflows/version-bump.yml` computes the bump from the **PR title**: `feat:` (or `feat(scope)!:`) → MINOR with PATCH reset; everything else → PATCH. (A `main` post-commit hook could never fire: every merge is a server-side squash.)

- **Version Bump** (same-repo PRs): computes the expected version against base `main`'s `.version` and, if the branch doesn't match, bumps `.version`, `api/version.go`, and the OpenAPI `info.version`, regenerates the embedded spec (`make generate-api`), and pushes a `chore(version): bump to X.Y.Z` commit to the PR branch. No-op once the branch already matches, which also prevents loops.
- **Version Check** (all PRs, including forks): recomputes independently and fails if `.version`, the OpenAPI `info.version`, or the version in `api/api.go`'s embedded spec disagree. This is the required check in the ruleset.

Both share `scripts/ci-version-bump.sh` (`compute-version`, `apply-version`, `embedded-spec-version`, `self-test`) and pin `oapi-codegen` v2.7.1 for `make generate-api`; other versions silently miscompile `api/api.go`. Manual bumps are unnecessary but harmless; `scripts/update-version.sh` derives the type from the last commit message and is for one-off local use. Residual race: two concurrent PRs can compute the same next version; the second to merge looks stale until its branch is pushed again and rechecked.

**Bump exclusions:**

- `github.com/golang/protobuf` — deprecated transitive dependency; can't be pinned (`go mod tidy` removes it); ignored in Dependabot
- `github.com/mattn/go-sqlite3` — must stay on **v1.x**. The `v2.0.3+incompatible` tag is mis-tagged (older than v1.14.x, but Go orders `+incompatible` v2 as newest), so `go get -u`/`go mod tidy` will latch it and break SQLite-backed tests (e.g. `TestPruneAuditEntries_ManyRows`). Keep the `exclude github.com/mattn/go-sqlite3 v2.0.3+incompatible` directive in `go.mod`; never accept a v2 bump.

## Documentation

All project documentation lives in the GitHub Wiki (https://github.com/ericfitz/tmi/wiki). `docs/` is deprecated and will be removed: do not add or update anything there, **except** `docs/superpowers/`, where superpowers skills (brainstorming, writing-plans, etc.) write specs and plans. That subtree is only for superpowers-generated artifacts; hand-authored docs still go to the wiki.

## Python

Run Python scripts with uv; when creating one, add uv inline metadata for automatic package management.

## Subagents

Dispatch subagents whenever that is the most efficient route (parallel independent work, broad searches that would flood the main context, fresh-eyes review); no permission needed. Pick the model for the task the *agent* performs, per the global model-selection guidelines. `oracle-db-admin` for DB-touching changes is mandatory, not an efficiency choice.

## Session completion (landing the plane)

Work is NOT complete until `git push` succeeds. When ending a work session:

1. File issues for remaining work
2. Run formatters and linters (use Go's tools to fix formatting rather than editing by hand)
3. Code changes: build and unit tests; then run the `security-review` skill — if it reports issues, stop, report them, and ask the user what to do
4. API changes: integration tests, postman/newman API tests, and CATS fuzzing. Fix integration/postman failures; analyze CATS results with the make target and prepare a plan for true positives. Stop and review the plan with the user.
5. Update issue status: close finished work, update in-progress items
6. Commit locally with a conventional message
7. **Push:** `git pull --rebase && git push`, then `git status` must show up to date with origin. Never stop before pushing, never say "ready to push when you are", and if push fails, resolve and retry until it succeeds.
8. Clean up (clear stashes, prune remote branches), verify everything is committed and pushed, and hand off context for the next session

<!-- sem-markers -->
## SEM markers

This project uses `SEM@<sha>` intent markers: a one-line comment directly above each
function/method/class describing **what it does** (intent, not mechanism), plus the commit
its body last changed at. Example:

    // SEM@4abcf04: validate a JWT and return its claims; reject if expired (pure)

**Why they matter:** the descriptions are the signal the `dedupe` tool uses to find duplicate
code, and they make the codebase easier for both humans and `sem` to navigate. Keeping them
accurate is load-bearing — a stale description is worse than none.

**The rule:** when you add a new function/method/class, or change one's behavior, add or update
its `SEM@` marker. Run `/sem-annotate --update <changed files>` to (re)generate markers for the
files you touched, or `/sem-annotate <path>` to cover a whole scope. Only entities that are new
or whose logic changed get rewritten — unchanged siblings keep their markers.

**Writing the description** (so duplicates cluster reliably): one line, ≤ ~12 words, intent not
mechanism; lead with a canonical verb (validate, parse, fetch, store, build, convert, serialize,
register, handle, authenticate, …) and a canonical domain noun; abstract incidental
identifiers; don't restate the entity name; tag a strong side-effect when it discriminates
(`(pure)`, `(reads DB)`).

**Drift is handled for you:** the `@<sha>` lets `/sem-annotate` tell whether a marker is stale
via `sem diff` (formatting/reformatting never marks it stale), so you don't need to touch a
marker whose entity didn't logically change.
<!-- /sem-markers -->

# AWS guidance

- **Terraform is the only IaC tool.** Infrastructure lives in `terraform/environments/<env>` (`aws-public` is live) and `terraform/modules/<name>/aws`. Change infrastructure there, not with `aws` CLI mutations. There is no CDK or CloudFormation, so ignore advice that assumes either (including `{{resolve:secretsmanager:...}}`).
- **Workloads are not Terraform's job.** Terraform owns infra and bootstrap objects (namespace, ConfigMap, Secret, IRSA service account); every Deployment/Service/Ingress belongs to the kustomize overlay in `deployments/k8s/dev/aws`. Adding a workload to Terraform re-breaks that boundary.
- Run Terraform from the environment directory with the `tmi` profile (`cd terraform/environments/aws-public && AWS_PROFILE=tmi terraform plan`) and read the plan before applying. `terraform.tfvars` is generated and gitignored; `scripts/deploy-aws.sh` rewrites it on every run, so hand-edits are temporary.
- The `aws` CLI is for *reading* state and the few operations Terraform doesn't own (Route 53 record surgery during a cutover, ECR pushes, `eks update-kubeconfig`). Prefer the AWS MCP Server when it can do the job, for sandboxing and audit logging.
- Before an AWS task, check for a relevant AWS skill via `retrieve_skill` and prefer it over general knowledge; verify skill names, since the set here may not match AWS's catalogue. Verify uncertain details (API parameters, permissions, limits, error codes) against documentation and state uncertainty explicitly.
- Follow AWS Well-Architected Framework principles. No em dashes in AWS resource names or descriptions; use hyphens.

## Secret safety

Secret *values* must never reach a command line, environment variable, log, or the model's context. Use the sanctioned patterns rather than inventing one:

- **Never** echo, `cat`, `printf`, or interpolate a secret into a shell command, and never `set -x` in a script that handles one.
- Injecting a secret into the cluster: follow `scripts/set-oauth-secret.sh` / `scripts/set-embedding-secret.sh` — the operator writes the value to a `umask 077` file and `kubectl --from-file` reads it from disk, so it never appears in argv or the environment.
- Reading a secret for a deploy: `scripts/deploy-aws.sh` is the sanctioned caller of `aws secretsmanager get-secret-value`; it fetches DB credentials into a `umask 077` temp config and never prints them. Extend that script rather than adding ad-hoc callers.
- Terraform-managed secrets (`terraform/modules/secrets/aws`, `random_password`) land in remote state, which is why `encrypt = true` is pinned in `terraform/environments/aws-public/main.tf` while bucket/table come per-deployer from the gitignored `backend.hcl`. Never write state locally or paste it anywhere; mark secret outputs `sensitive`.
