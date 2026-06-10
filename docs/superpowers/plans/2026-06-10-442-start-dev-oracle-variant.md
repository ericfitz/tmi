# start-dev Oracle Variant (`DB=oracle`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `make start-dev DB=oracle` so the in-cluster dev server runs the Oracle-enabled image (`Dockerfile.server-oracle`, `-tags oracle,dev`, CGO + Instant Client) with the OCI ADB wallet mounted, while `DB=postgres` (default) keeps Plan 1 behavior unchanged.

**Architecture:** Plan 1's `start-dev.py` gains a `--db {postgres,oracle}` selector that swaps (a) the server image build, (b) the dev overlay, and (c) wallet-secret creation. Oracle uses a separate server manifest (`server-oracle.yml`) that mounts a `tmi-oracle-wallet` Secret (containing `wallet.zip`) at `/wallet`; the image's existing entrypoint extracts it and sets `TNS_ADMIN`. The DB connection itself comes from the delivered `config-development.yml` ConfigMap (developer-owned), exactly as Postgres does.

**Tech Stack:** Python 3.11 + uv, kubectl/kustomize, Docker buildx (CGO Oracle Linux build), Oracle Instant Client 23ai, OCI ADB wallet.

**Spec:** `docs/superpowers/specs/2026-06-10-442-start-dev-kind-migration-design.md` (§ "Server image", "Tilt fast inner loop" excluded for Oracle).

**Depends on:** Plan 1 (core) — merged to `dev/1.4.0`. This plan modifies Plan 1's `start-dev.py`, `Makefile`, and `deployments/k8s/dev/`.

---

## File Structure

**Create:**
- `deployments/k8s/dev/server-oracle.yml` — Oracle server Deployment + Service (oracle image, wallet volume).
- `deployments/k8s/dev/oracle/kustomization.yaml` — oracle overlay: reuses parent `controller.yml`/`redis.yml`/component CRs, swaps in `server-oracle.yml`.

**Modify:**
- `Dockerfile.server-oracle` — add an `EXTRA_TAGS` build-arg so the dev build is `-tags "oracle dev"`.
- `scripts/start-dev.py` — add `--db {postgres,oracle}`; db-dependent image builds, overlay dir, and wallet-secret creation.
- `Makefile` — thread a `DB` variable into `start-dev`/`restart-dev`/`stop-dev`.

---

## Task 1: `EXTRA_TAGS` build-arg on `Dockerfile.server-oracle`

**Files:** Modify `Dockerfile.server-oracle` (the `go build` line).

- [ ] **Step 1: Add the arg and thread it into the build tags**

In the builder stage, just before the `RUN CGO_ENABLED=1 ... go build` line, add:
```dockerfile
# Extra Go build tags appended to "oracle" (e.g. "dev" for login_hint + the test OAuth provider)
ARG EXTRA_TAGS=""
```
Change the build's tag flag from:
```dockerfile
    -tags oracle \
```
to:
```dockerfile
    -tags "oracle ${EXTRA_TAGS}" \
```
(Go accepts space-separated build tags. Keep all other flags, the `-ldflags` block, `CGO_ENABLED=1`, and `-trimpath` exactly as-is.)

- [ ] **Step 2: Verify the build still parses (best-effort)**

The Oracle image is heavy (Oracle Linux + Instant Client + CGO) and pulls from `container-registry.oracle.com`. A full build may take many minutes and requires network access to Oracle's registry. Attempt:
```bash
docker build -f Dockerfile.server-oracle --build-arg EXTRA_TAGS=dev -t tmi-server-oracle:dev-check . 2>&1 | tail -20
```
Expected: build succeeds (image named). If the Oracle registry is unreachable in this environment, the build will fail at the `FROM container-registry.oracle.com/...` pull — in that case, confirm the Dockerfile change is syntactically correct (`docker build` reaches the `FROM` pull before failing) and report DONE_WITH_CONCERNS noting the build couldn't complete here. NOTE: staging `.docker-deps/tmi-client/` is required exactly as in the other image builds (the Dockerfile has the same `COPY .docker-deps/tmi-client/` line).

- [ ] **Step 3: Commit**
```bash
git add Dockerfile.server-oracle
git commit -m "build(dev): add EXTRA_TAGS arg to Dockerfile.server-oracle for -tags oracle,dev"
```

---

## Task 2: Oracle server manifest

**Files:** Create `deployments/k8s/dev/server-oracle.yml`.

- [ ] **Step 1: Write the manifest**

The Oracle image's entrypoint reads `/wallet/wallet.zip`, extracts it, and sets `TNS_ADMIN`. We mount the `tmi-oracle-wallet` Secret (key `wallet.zip`) at `/wallet`. Redis/NATS are injected like the Postgres server; the Oracle DB connection comes from the mounted config (`config-development.yml` → ConfigMap). `ENV=development` and the tmi provider are set so dev login works.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tmi-server
  namespace: tmi-platform
spec:
  replicas: 1
  selector:
    matchLabels: { app: tmi-server }
  template:
    metadata:
      labels: { app: tmi-server }
    spec:
      containers:
        - name: server
          image: localhost:5000/tmi-server-oracle:dev
          args: ["--config=/etc/tmi/config.yml"]
          ports:
            - containerPort: 8080
          env:
            - { name: ENV, value: "development" }
            - { name: TMI_SERVER_INTERFACE, value: "0.0.0.0" }
            - { name: TMI_SERVER_PORT, value: "8080" }
            - { name: TMI_REDIS_HOST, value: "redis" }
            - { name: TMI_NATS_URL, value: "nats://nats.tmi-platform.svc:4222" }
            - { name: TMI_LOG_DIR, value: "/tmp/logs" }
            - { name: OAUTH_PROVIDERS_TMI_ENABLED, value: "true" }
          volumeMounts:
            - name: config
              mountPath: /etc/tmi
              readOnly: true
            - name: wallet
              mountPath: /wallet
              readOnly: true
          readinessProbe:
            httpGet: { path: /, port: 8080 }
            initialDelaySeconds: 5
            periodSeconds: 5
          livenessProbe:
            httpGet: { path: /, port: 8080 }
            initialDelaySeconds: 20
            periodSeconds: 20
          resources:
            requests: { cpu: 100m, memory: 256Mi }
            limits: { cpu: 1000m, memory: 768Mi }
      volumes:
        - name: config
          configMap:
            name: tmi-server-config
        - name: wallet
          secret:
            secretName: tmi-oracle-wallet
---
apiVersion: v1
kind: Service
metadata:
  name: tmi-server
  namespace: tmi-platform
spec:
  selector: { app: tmi-server }
  ports:
    - port: 8080
      targetPort: 8080
```

Sanity checks (read code; only change the manifest on a real mismatch):
- Confirm how the Oracle build expects the DB connection: `Dockerfile.server-oracle` documents `TMI_DATABASE_URL=oracle://user:password@tns_alias`. Confirm whether the server (with `-tags oracle`) reads the Oracle DSN from `config-development.yml` `database.*` or from `TMI_DATABASE_URL`. Grep `internal/config` and the oracle-tagged DB init (`internal/dbschema` / wherever `-tags oracle` selects the Oracle driver). If it needs `TMI_DATABASE_URL`, document that the developer sets it in `config-development.yml` (or note an env addition) — do NOT hardcode credentials in the manifest.
- Confirm `/wallet` mount is read-only-safe: the entrypoint copies the zip to `/tmp/wallet` before extraction (it does), so a read-only `/wallet` is correct.
- The Oracle runtime image runs as user `tmi` (not 65532); no explicit `runAsUser` is needed (no `runAsNonRoot` is set on this pod). Leave securityContext unset unless a live deploy rejects it.

- [ ] **Step 2: Validate schema (client-side)**
```bash
kubectl --context kind-tmi-dev apply --dry-run=client -f deployments/k8s/dev/server-oracle.yml
```
Expected: `deployment.apps/tmi-server` + `service/tmi-server` `(dry run)`, no error. (Needs the kind-tmi-dev cluster from Plan 1 reachable; if down, `kubectl kustomize`-style client validation or `kubeconform` is acceptable.)

- [ ] **Step 3: Commit**
```bash
git add deployments/k8s/dev/server-oracle.yml
git commit -m "feat(dev): Oracle server manifest (wallet-mounted, oracle image)"
```

---

## Task 3: Oracle overlay (kustomize)

**Files:** Create `deployments/k8s/dev/oracle/kustomization.yaml`.

- [ ] **Step 1: Write the overlay**

It reuses the parent controller/redis/component CRs and swaps the server for the Oracle one. Paths are relative to `deployments/k8s/dev/oracle/`.

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: tmi-platform
resources:
  - ../controller.yml
  - ../redis.yml
  - ../server-oracle.yml
  - ../../platform/components/tmi-extractor.yml
  - ../../platform/components/tmi-chunk-embed.yml
patches:
  - path: ../patches/extractor-image.yaml
    target:
      group: tmi.dev
      version: v1alpha1
      kind: TMIComponent
      name: tmi-extractor
  - path: ../patches/chunkembed-image.yaml
    target:
      group: tmi.dev
      version: v1alpha1
      kind: TMIComponent
      name: tmi-chunk-embed
```

- [ ] **Step 2: Verify the render**
```bash
kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/oracle | grep -E "image:" | sort -u
kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/oracle | grep -c "kind: TMIComponent"
```
Expected image set includes `localhost:5000/tmi-server-oracle:dev`, `localhost:5000/tmi-component-controller:dev`, `cgr.dev/chainguard/redis:latest`, and the two `localhost:5000/tmi-extractor:dev` / `tmi-chunk-embed:dev`. TMIComponent count `2`. Fix relative paths until it renders.

- [ ] **Step 3: Commit**
```bash
git add deployments/k8s/dev/oracle/kustomization.yaml
git commit -m "feat(dev): oracle dev overlay (reuses controller/redis/components, oracle server)"
```

---

## Task 4: `--db` selector + wallet secret in `start-dev.py`

**Files:** Modify `scripts/start-dev.py`.

- [ ] **Step 1: Add the `--db` arg**

In `parse_args`, add:
```python
    p.add_argument("--db", choices=["postgres", "oracle"], default="postgres",
                   help="Which database flavor to deploy (selects the server image + overlay)")
```

- [ ] **Step 2: Make image builds and overlay db-dependent**

Replace the module-level `IMAGE_BUILDS` constant usage with a function. Add near the top:
```python
def image_builds_for(db: str):
    """Return the (name, dockerfile, build_args) tuples for the chosen DB flavor.

    The controller and the two workers are identical across DB flavors; only the
    server image differs (static Postgres image vs. Oracle CGO image).
    """
    if db == "oracle":
        server = ("tmi-server-oracle", "Dockerfile.server-oracle", {"EXTRA_TAGS": "dev"})
    else:
        server = ("tmi-server", "Dockerfile.server", {"BUILD_TAGS": "dev"})
    return [
        server,
        ("tmi-component-controller", "Dockerfile.controller", {}),
        ("tmi-extractor", "Dockerfile.extractor", {}),
        ("tmi-chunk-embed", "Dockerfile.chunkembed", {}),
    ]


def overlay_dir_for(db: str) -> str:
    return f"{DEV_DIR}/oracle" if db == "oracle" else DEV_DIR
```
Change `build_and_push()` to take `db`:
```python
def build_and_push(db: str) -> None:
    ...
    created = stage_tmi_client()
    try:
        for name, dockerfile, build_args_map in image_builds_for(db):
            ref = devenv.local_image_ref(name)
            ...
    finally:
        unstage_tmi_client(created)
```
Change `apply_overlay(no_workers)` to take `db` and use `overlay_dir_for(db)` instead of the hardcoded `DEV_DIR` in the kustomize render path (the `--no-workers` branch should apply `controller.yml`, `redis.yml`, and the db-appropriate server manifest — `server.yml` for postgres, `server-oracle.yml` for oracle).

- [ ] **Step 3: Add wallet-secret creation (oracle only)**

Add:
```python
def create_oracle_wallet_secret() -> None:
    """Create the tmi-oracle-wallet Secret from the developer's wallet zip.

    Path comes from TMI_ORACLE_WALLET_ZIP (a path to the OCI ADB wallet .zip).
    The Oracle image entrypoint reads /wallet/wallet.zip and extracts it.
    """
    wallet = os.environ.get("TMI_ORACLE_WALLET_ZIP", "")
    if not wallet or not Path(wallet).is_file():
        log_error("DB=oracle requires TMI_ORACLE_WALLET_ZIP to point at your ADB wallet .zip")
        sys.exit(1)
    rendered = run_cmd(
        ["kubectl", "create", "secret", "generic", "tmi-oracle-wallet", "-n", NS,
         f"--from-file=wallet.zip={wallet}", "--dry-run=client", "-o", "yaml"],
        capture=True,
    ).stdout
    devenv.kubectl(["apply", "-f", "-"], input_text=rendered)
    log_success("oracle wallet delivered as Secret/tmi-oracle-wallet")
```
NOTE: first check whether an existing convention provides the wallet path (e.g. `scripts/oci-env.sh` may export a wallet/`TNS_ADMIN` variable used by `make test-integration-oci` / `start-dev-oci`). If such a variable already exists, reuse its name instead of inventing `TMI_ORACLE_WALLET_ZIP`, and document the chosen name. Do not invent a second convention if one exists.

- [ ] **Step 4: Wire db through `do_start` / `do_restart` / `do_stop`**

`do_start(args)`: pass `args.db` to `build_and_push`, call `create_oracle_wallet_secret()` when `args.db == "oracle"` (before `apply_overlay`), and pass `args.db` to `apply_overlay`. `do_restart(args)`: same (build + re-apply the db overlay + roll). `do_stop`: also delete the oracle wallet secret (`kubectl -n tmi-platform delete secret tmi-oracle-wallet --ignore-not-found`) and delete from `overlay_dir_for(args.db)` — but since `--stop` may not know the db, delete both the base server and `tmi-oracle-wallet` defensively (server Deployment name is `tmi-server` for both, so the existing deletes already cover the Deployment; just add the wallet-secret delete).

- [ ] **Step 5: `py_compile` + a `--db oracle` dry check (no Oracle creds needed)**
```bash
python3 -m py_compile scripts/start-dev.py
# Without TMI_ORACLE_WALLET_ZIP set, oracle start must fail fast with the wallet error:
TMI_ORACLE_WALLET_ZIP= uv run scripts/start-dev.py --db oracle --yes 2>&1 | grep -i wallet || echo "expected wallet fast-fail not observed"
```
Expected: the wallet fast-fail message appears (proves the guard) — it should exit before building images. (Run this only far enough to hit the guard; Ctrl-C if it proceeds to builds.)

- [ ] **Step 6: Commit**
```bash
git add scripts/start-dev.py
git commit -m "feat(dev): start-dev --db {postgres,oracle}; oracle wallet secret + overlay"
```

---

## Task 5: Thread `DB` through the Makefile

**Files:** Modify `Makefile`.

- [ ] **Step 1: Pass `DB` (default postgres) into the targets**
```makefile
DB ?= postgres

start-dev:  ## Deploy the dev environment (DB=postgres|oracle)
	@uv run scripts/start-dev.py --db $(DB)

restart-dev:  ## Rebuild+push server, redeliver config, roll server (DB=postgres|oracle)
	@uv run scripts/start-dev.py --restart --db $(DB)

stop-dev:  ## Tear down everything start-dev deployed
	@uv run scripts/start-dev.py --stop --db $(DB)
```
(`DB ?= postgres` makes `make start-dev` unchanged and `make start-dev DB=oracle` select Oracle.)

- [ ] **Step 2: Verify `make start-dev` still defaults to postgres**
```bash
grep -n "db postgres\|DB ?=" Makefile
make -n start-dev   # dry-run: shows the recipe resolves to --db postgres
make -n start-dev DB=oracle   # shows --db oracle
```
Expected: default resolves to `--db postgres`; override resolves to `--db oracle`.

- [ ] **Step 3: Commit**
```bash
git add Makefile
git commit -m "build(dev): thread DB var into start-dev/restart-dev/stop-dev"
```

---

## Task 6: Acceptance (Oracle)

**Files:** none (verification).

- [ ] **Step 1: Wallet fast-fail (no creds needed)** — confirmed in Task 4 Step 5; re-verify `make start-dev DB=oracle` without `TMI_ORACLE_WALLET_ZIP` exits with the wallet error before building.

- [ ] **Step 2: Live ADB round-trip (requires ADB creds + wallet)** — only where Oracle creds exist:
  - Set `config-development.yml`'s database section to the ADB connection (per the oracle DSN convention confirmed in Task 2), export `TMI_ORACLE_WALLET_ZIP=/path/to/wallet.zip`.
  - `make dev-cluster-up` (if not up), then `make start-dev DB=oracle`.
  - Expected: the `tmi-server` pod (oracle image) reaches Ready; `curl -s http://localhost:8080/` returns version JSON; server logs show a successful Oracle connection + `AutoMigrate`. Confirm with an authenticated call as in Plan 1 (OAuth login → `GET /threat_models` → 200).
  - If no ADB creds are available in this environment, mark DEFERRED and note that the wiring (image build, wallet secret, overlay, manifest) is verified but the live ADB connection wasn't exercised.

- [ ] **Step 3: Confirm Postgres path is unbroken**
```bash
make start-dev          # default postgres
curl -s http://localhost:8080/ | head -c 80; echo
make stop-dev
```
Expected: the default Postgres flow still works end-to-end (no regression from the `--db` refactor).

- [ ] **Step 4: Commit any fixups**
```bash
git add -A && git commit -m "test(dev): oracle variant acceptance fixups" || echo "nothing to commit"
```

---

## Self-Review Notes (for the implementer)

- **Oracle build is heavy and network-dependent** (Oracle registry + Instant Client). If it can't complete in this environment, verify Dockerfile correctness and defer the live build — don't fake it.
- **DB DSN mechanism** is the main unknown: confirm whether `-tags oracle` reads the connection from `config-development.yml` `database.*` or `TMI_DATABASE_URL`, and document it. Never hardcode ADB credentials in a committed manifest.
- **Wallet path convention:** reuse an existing `oci-env.sh`/`test-integration-oci` variable if one exists rather than inventing `TMI_ORACLE_WALLET_ZIP`.
- **Don't regress Postgres:** the `--db` refactor touches shared functions (`build_and_push`, `apply_overlay`, `do_*`); Task 6 Step 3 guards against that.
- Oracle is intentionally excluded from the Tilt fast loop (Plan 3) — the CGO image is not the live-update target.
