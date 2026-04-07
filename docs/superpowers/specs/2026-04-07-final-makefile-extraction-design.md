# Design: Final Makefile Extraction

**Issue:** [#215](https://github.com/ericfitz/tmi/issues/215)
**Date:** 2026-04-07
**Status:** Approved
**Builds on:** [2026-04-06-makefile-to-scripts-design.md](2026-04-06-makefile-to-scripts-design.md)

## Problem

The Makefile is 923 lines. Most targets already delegate to Python scripts, but ~12 areas still have inline shell logic, tool checks, argument building, or human-facing output (color logging, echo statements). The goal is to eliminate all remaining inline logic so the Makefile is a thin pass-through with no human-facing output.

## Completion Criteria

- Every Make target is a one-liner calling a script, or a dependency chain of other targets
- No `echo`, `$(call log_*)`, color codes, or shell conditionals remain in the Makefile
- `kill_port` macro, color defines, and logging defines are removed
- `VERSION`, `COMMIT`, `BUILD_DATE` variables are removed (scripts read `.version` directly)
- `scripts/run-cats-fuzz.sh` and `scripts/oauth-stub-lib.sh` are deleted
- `scripts/query-cats-results.sh` is deleted (replaced by Python)

## New Scripts

### 1. `scripts/manage-terraform.py`

Single script for all Terraform operations. Replaces 8 Make targets that currently have inline tool checks, env var validation, and logging.

**Subcommands:** `init`, `plan`, `apply`, `validate`, `fmt`, `output`, `destroy`

**Flags:**
- `--environment ENV` (default: `oci-public`) — maps to `terraform/environments/<ENV>`
- `--auto-approve` — passes `-auto-approve` to terraform apply/destroy
- `--from-plan` — applies from saved `tfplan` file (used with `apply`)
- `-v`/`-q` — verbosity

**Behavior:**
- `init` is automatically called before `plan`, `apply`, `validate` (same as current Make dependencies)
- Tool check: verifies `terraform` is installed, gives install instructions if missing
- Sets `GODEBUG=x509negativeserial=1` for `plan` and `apply` (current behavior)
- `fmt` runs recursively on the `terraform/` directory

**Make target mapping:**

| Make Target | New Invocation |
|---|---|
| `tf-init` | `@uv run scripts/manage-terraform.py init` |
| `tf-plan` | `@uv run scripts/manage-terraform.py plan` |
| `tf-apply` | `@uv run scripts/manage-terraform.py apply` |
| `tf-apply-plan` | `@uv run scripts/manage-terraform.py apply --from-plan` |
| `tf-validate` | `@uv run scripts/manage-terraform.py validate` |
| `tf-fmt` | `@uv run scripts/manage-terraform.py fmt` |
| `tf-output` | `@uv run scripts/manage-terraform.py output` |
| `tf-destroy` | `@uv run scripts/manage-terraform.py destroy` |
| `tf-check` | Removed (internal to script) |

### 2. `scripts/run-cats-fuzz.py`

Replaces `scripts/run-cats-fuzz.sh`. Absorbs all CATS tool checks, argument building, OAuth authentication, rate limit management, and environment preparation from both the shell script and the Makefile `cats-fuzz`/`cats-fuzz-oci` targets.

After CATS completes, automatically imports and calls `parse_cats_results` (from `scripts/parse-cats-results.py`) as a module to parse results into the SQLite database. This eliminates the `parse-cats-results` Make target.

**Flags:**
- `--user USER` (default: `charlie`)
- `--server URL` (default: `http://localhost:8080`)
- `--path PATH` — restrict to specific endpoint
- `--rate N` — max requests per minute (default: `3000`)
- `--blackbox` — ignore all error codes other than 500
- `--config FILE` — config file for cats-seed (default: `config-development.yml`)
- `--oci` — use OCI cats-seed variant
- `--provider PROVIDER` (default: `tmi`)
- `--skip-seed` — skip the cats-seed step
- `--skip-parse` — skip auto-parsing results after fuzzing
- `-v`/`-q` — verbosity

**Behavior:**
1. Check prerequisites (CATS installed, server running, OpenAPI spec exists)
2. Run cats-seed (unless `--skip-seed`)
3. Ensure OAuth stub is running (replaces `oauth-stub-lib.sh` functionality)
4. Prepare test environment (clear old reports, clear Redis rate limits)
5. Authenticate user via OAuth stub flow
6. Build and execute CATS command with all fuzzer configuration
7. Parse results into SQLite (unless `--skip-parse`)

**OAuth stub management:** The `ensure_oauth_stub` logic from `oauth-stub-lib.sh` moves into `tmi_common.py` as a shared function, since integration test scripts also need it. It checks if the stub is responding on port 8079, and if not, calls `manage-oauth-stub.py start` via subprocess.

**Make target mapping:**

| Make Target | New Invocation |
|---|---|
| `cats-fuzz` | `@uv run scripts/run-cats-fuzz.py` |
| `cats-fuzz-oci` | `@uv run scripts/run-cats-fuzz.py --oci` |
| `parse-cats-results` | Removed (automatic after fuzzing) |
| `analyze-cats-results` | Becomes alias for `query-cats-results` |

### 3. `scripts/query-cats-results.py`

Replaces `scripts/query-cats-results.sh`. Same SQL queries, proper argparse, uses `tmi_common` logging.

**Flags:**
- `--db FILE` (default: `test/outputs/cats/cats-results.db`)
- `-v`/`-q` — verbosity

**Behavior:** Runs the same summary queries (results summary, false positives count, errors by path, warnings by path) and prints query examples. Checks that the database file exists and gives helpful error if not.

**Make target mapping:**

| Make Target | New Invocation |
|---|---|
| `query-cats-results` | `@uv run scripts/query-cats-results.py` |
| `analyze-cats-results` | Dependency: `query-cats-results` only |

### 4. `scripts/run-api-tests.py`

Single script for all Postman/Newman API testing. Replaces `test-api`, `test-api-collection`, and `test-api-list` Make targets.

**Flags:**
- `--collection NAME` — run a specific collection (if omitted, runs full suite)
- `--list` — list available collections and exit
- `--start-server` — auto-start server if needed
- `--response-time-multiplier N` (default: `1`)
- `-v`/`-q` — verbosity

**Behavior:**
- Tool check: verifies `newman` is installed
- Verifies `test/postman/run-tests.sh` exists
- In `--list` mode: lists `.json` files in `test/postman/`
- In `--collection` mode: verifies collection file exists, calls `run-postman-collection.sh`
- In default mode: calls `run-tests.sh` (with `--start-server` if specified)
- Passes `RESPONSE_TIME_MULTIPLIER` as environment variable

**Make target mapping:**

| Make Target | New Invocation |
|---|---|
| `test-api` | `@uv run scripts/run-api-tests.py` |
| `test-api-collection` | `@uv run scripts/run-api-tests.py --collection $(COLLECTION)` |
| `test-api-list` | `@uv run scripts/run-api-tests.py --list` |

### 5. `scripts/manage-oci-functions.py`

Single script for OCI Functions (fn CLI) operations. Replaces `fn-*` Make targets.

**Subcommands:** `build`, `deploy`, `invoke`, `logs`

**Flags:**
- `--app APP_NAME` — OCI Function Application name (required for deploy/invoke/logs)
- `--function NAME` (default: `certmgr`) — function name
- `-v`/`-q` — verbosity

**Behavior:**
- Tool check: verifies `fn` CLI is installed
- Each subcommand maps to the corresponding `fn` CLI command
- `build`: `cd functions/<name> && fn build`
- `deploy`: `cd functions/<name> && fn deploy --app <app>`
- `invoke`: `fn invoke <app> <name>`
- `logs`: `fn logs <app> <name>`

**Make target mapping:**

| Make Target | New Invocation |
|---|---|
| `fn-build-certmgr` | `@uv run scripts/manage-oci-functions.py build` |
| `fn-deploy-certmgr` | `@uv run scripts/manage-oci-functions.py deploy --app $(FN_APP)` |
| `fn-invoke-certmgr` | `@uv run scripts/manage-oci-functions.py invoke --app $(FN_APP)` |
| `fn-logs-certmgr` | `@uv run scripts/manage-oci-functions.py logs --app $(FN_APP)` |
| `fn-check` | Removed (internal to script) |

### 6. `scripts/manage-arazzo.py`

Single script for Arazzo workflow generation. Replaces inline logging in `arazzo-install`, `arazzo-scaffold`, `arazzo-enhance` Make targets.

**Subcommands:** `install`, `scaffold`, `enhance`, `generate` (all three in sequence)

**Flags:**
- `-v`/`-q` — verbosity

**Behavior:**
- `install`: runs `pnpm install`
- `scaffold`: calls `scripts/generate-arazzo-scaffold.sh`
- `enhance`: calls `uv run scripts/enhance-arazzo-with-workflows.py`
- `generate`: runs install + scaffold + enhance + validate (calls `validate-arazzo.py`)

**Make target mapping:**

| Make Target | New Invocation |
|---|---|
| `arazzo-install` | `@uv run scripts/manage-arazzo.py install` |
| `arazzo-scaffold` | `@uv run scripts/manage-arazzo.py scaffold` |
| `arazzo-enhance` | `@uv run scripts/manage-arazzo.py enhance` |
| `generate-arazzo` | `@uv run scripts/manage-arazzo.py generate` |
| `validate-arazzo` | Unchanged (already calls script, but remove `$(call log_success,...)` wrapper) |

## Modified Existing Scripts

### `scripts/run-coverage.py`

Add a `--full` mode that orchestrates the complete coverage workflow currently in the `test-coverage` Make target:
1. Run `clean.py all`
2. Run `manage-database.py start`
3. Run `manage-redis.py start`
4. Run `manage-database.py wait`
5. Run existing coverage logic
6. On exit (success or failure): run `manage-database.py --test clean` and `manage-redis.py --test clean`

The trap-based cleanup of test infrastructure moves into the script.

### `scripts/run-wstest.py`

- Build wstest on demand before running (absorbs `build-wstest` logic: `cd wstest && go mod tidy && go build -o wstest`)
- Add `--monitor` flag for monitor mode (absorbs `monitor-wstest`: checks server is running, runs `./wstest --user monitor`)
- `build-wstest` Make target removed
- `monitor-wstest` Make target becomes `@uv run scripts/run-wstest.py --monitor`

### `scripts/clean.py`

- Add wstest process cleanup to `process` scope (kill `wstest` processes)
- Add wstest log cleanup to `files` scope (remove `wstest/*.log`)
- Add build artifact cleanup to a new `build` scope (remove `./bin/*`, `migrate`)
- Add container cleanup to a new `containers` scope (stop and remove named containers)
- `clean-wstest` Make target removed
- `clean-build` Make target becomes `@uv run scripts/clean.py build`
- `clean-containers` Make target becomes `@uv run scripts/clean.py containers`

### `scripts/manage-database.py`

- Add `check` subcommand (runs `cd cmd/migrate && go run main.go --config <config> --validate`)
- `check-database` Make target becomes `@uv run scripts/manage-database.py check`

### `scripts/generate-sbom.py`

- Add internal tool checks for `cyclonedx-gomod` and `grype` with install instructions
- `check-cyclonedx` and `check-grype` Make targets removed (internal to script)

### `scripts/lib/tmi_common.py`

- Add `ensure_oauth_stub()` function — checks if OAuth stub is responding on port 8079, starts it via `manage-oauth-stub.py start` if not. Shared by `run-cats-fuzz.py` and integration test scripts.

## Deleted Files

- `scripts/run-cats-fuzz.sh` — replaced by `scripts/run-cats-fuzz.py`
- `scripts/oauth-stub-lib.sh` — functionality moved to `tmi_common.py`
- `scripts/query-cats-results.sh` — replaced by `scripts/query-cats-results.py`

## Makefile Final Cleanup

After all extractions, remove:

1. **`kill_port` macro** (lines 57-71) — only consumer was `stop-process`, which now uses a script
2. **Color defines** (lines 27-33) — `BLUE`, `GREEN`, `YELLOW`, `RED`, `NC`
3. **Logging defines** (lines 35-49) — `log_info`, `log_success`, `log_warning`, `log_error`
4. **Build variables** (lines 23-25) — `VERSION`, `COMMIT`, `BUILD_DATE` (scripts read `.version`)
5. **`stop-process` target** — replaced by `@uv run scripts/manage-server.py --port $(SERVER_PORT) kill-port`
6. **Stale backward compatibility aliases** — `build-container-*`, `containers-dev`, `report-containers` (these reference renamed targets)

### Remaining inline logging to remove

These targets already call scripts but still have `$(call log_*)` wrappers in the Makefile. Remove the logging — let the scripts handle their own output:

- `test-db-cleanup` — remove `$(call log_info,...)`
- `validate-asyncapi` — remove `$(call log_info,...)` and `$(call log_success,...)`, consolidate into a single `@uv run` call
- `parse-openapi-validation` — remove `$(call log_info,...)` and `$(call log_success,...)`
- `cats-seed` / `cats-seed-oci` — already clean (no inline logging)
- `setup-heroku` / `setup-heroku-dry-run` — remove `$(call log_info,...)`
- `reset-db-heroku` / `drop-db-heroku` — remove `$(call log_warning,...)`
- `dedup-group-members` — already clean
- `build-cats-seed` / `build-cats-seed-oci` — already clean

### Targets that keep inline logic (justified)

- **`test-integration-pg` / `test-integration-oci`** — single-line `if/else` passing `--cleanup` flag. These are already thin enough.
- **`start-containers-environment`** — `build-all` dependency + two `$(MAKE)` calls. Pure Make composition.
- **`build-with-sbom`** — dependency chain only: `build-server generate-sbom`
- **Backward compatibility aliases** (`build`, `test`, `lint`, `clean`, `dev`) — pure aliases.

## End State

The Makefile will be ~250-350 lines containing:
- `.PHONY` declarations
- One-liner targets calling `@uv run scripts/...` or `@./scripts/...`
- Composite targets as dependency chains
- `SERVER_PORT ?= 8080` and similar defaults passed as script arguments
- Backward compatibility aliases
- Zero human-facing output (no echo, no log macros, no color codes)
