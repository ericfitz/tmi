# Universal Container Build System

## Context

The TMI project currently has 5 shell build scripts (~1,300 lines), 6 Dockerfiles, and ~25 Makefile container build targets. Each cloud target (local, OCI, multi-arch) has its own script with significant duplication of version extraction, docker build invocation, Grype scanning, and SBOM generation. Adding a new cloud target requires creating a new shell script and new Makefile targets.

This design replaces all container build shell scripts and Makefile build targets with a unified Python-based system that handles any deployment target through a single entry point per container category.

## Architecture

### File Structure

```
scripts/
  build-app-containers.py      # Entry point: server, redis, promtail
  build-db-containers.py       # Entry point: postgres
  container_build_helpers.py   # Shared utilities (not run directly)
```

Entry-point scripts use UV inline TOML with `requires-python = ">=3.11"` and no external dependencies. The helpers module is imported via a one-line `sys.path.insert(0, Path(__file__).parent)` — this works with `uv run` because UV does not sandbox the filesystem; it only isolates the dependency environment. The helpers module has no `# /// script` block since it is never invoked directly.

### CLI Interface

#### build-app-containers.py

```
uv run scripts/build-app-containers.py \
  --target local|oci|aws|azure|gcp|heroku \
  --component server|redis|promtail|all \
  --arch arm64|amd64|both \
  --db-backend postgresql|oracle-adb \
  --registry REGISTRY_URL \
  --push \
  --scan \
  --scan-only \
  --no-cache
```

**Defaults:**
- `--target local`
- `--component all`
- `--arch` auto-detected from local platform when target is `local`; cloud targets have per-provider defaults (overridable)
- `--db-backend postgresql` (only affects server component Dockerfile selection)
- `--registry` auto-determined from target
- `--push` off
- `--scan` off

**Flag behaviors:**
- `--push` with `--target local`: error with message "Cannot push to local Docker daemon. Use a cloud target or specify --registry."
- `--scan-only`: skip building, scan existing images by name. Used by the `scan-containers` Makefile target.
- `--component all` with `--target heroku`: auto-skips redis and promtail with a warning ("Heroku uses addons for Redis; only building server"). Does not error.

#### build-db-containers.py

```
uv run scripts/build-db-containers.py \
  --target local|aws|azure|gcp \
  --arch arm64|amd64|both \
  --registry REGISTRY_URL \
  --push \
  --scan \
  --scan-only \
  --no-cache
```

`--target oci` and `--target heroku` exit with error: "OCI uses Oracle ADB (managed service); no database container to build" / "Heroku uses Postgres addon; no database container to build."

The DB container is always PostgreSQL (Chainguard `Dockerfile.postgres`). There is no `--db-backend` flag on this script — the database engine choice affects only the server container. No per-cloud Makefile targets exist for DB containers because cloud deployments use managed database services.

### Helpers Module (container_build_helpers.py)

#### TargetConfig dataclass

```python
@dataclass
class TargetConfig:
    registry: str              # e.g., "tmi", "{region}.ocir.io/{ns}/{repo}"
    platform: str              # e.g., "linux/arm64", "linux/amd64", "linux/amd64,linux/arm64"
    use_buildx: bool           # True for multi-arch or remote push
    auth_commands: list[list[str]]  # Shell commands to authenticate to registry
    dockerfile_map: dict[str, str]  # Component -> Dockerfile name
    image_name_prefix: str     # e.g., "tmi/tmi-" or full registry path
    labels: dict[str, str]     # Additional OCI labels
```

#### Key Functions

- `get_target_config(target, component, arch, db_backend, registry_override) -> TargetConfig`
  - Match statement dispatching to per-target config logic
  - Each case is ~15-25 lines setting TargetConfig fields

- `read_version(project_root) -> dict`
  - Reads `.version` JSON, returns `{major, minor, patch, prerelease}`
  - If file is missing or malformed: error with "Cannot read .version file; ensure it exists and contains valid JSON with major, minor, patch, prerelease keys"

- `get_build_args(version, git_commit) -> list[str]`
  - Returns `["--build-arg", "BUILD_DATE=...", "--build-arg", "GIT_COMMIT=...", ...]`

- `run_docker_build(config, dockerfile, context, tags, build_args, push, no_cache) -> None`
  - Selects `docker build` vs `docker buildx build` based on `config.use_buildx`
  - When `push=True`: uses `--push`
  - When `push=False` and platform contains comma (multi-arch): errors with "Multi-arch builds require --push (cannot load multi-platform images into local Docker daemon)"
  - When `push=False` and single platform: uses `--load` (buildx) or default (plain docker build)
  - Streams output to stdout

- `authenticate_registry(config) -> None`
  - Runs auth commands from config
  - Skips if `auth_commands` is empty (local target)

- `scan_image(image_name, reports_dir) -> bool`
  - Runs Grype scan, generates SARIF + text reports in `security-reports/`
  - Generates SBOM via Syft (CycloneDX JSON/XML) in `security-reports/sbom/`
  - Generates markdown summary in `security-reports/security-summary.md`
  - Returns True if within CVE thresholds (0 critical, 5 high)
  - This replaces both the old `scan-containers` and `report-containers` targets — scanning and reporting are now a single operation

- `check_prerequisites(need_buildx, need_scan) -> None`
  - Checks docker daemon is running (not just CLI installed): `docker info` must succeed
  - If `need_buildx`: checks `docker buildx version` and verifies a builder exists that supports the target platforms
  - If `need_scan`: checks grype and syft are available
  - Reports ALL missing prerequisites at once, then exits

- `run(cmd, check=True, capture=False) -> subprocess.CompletedProcess`
  - Subprocess wrapper with colored logging

- `log_info/log_warn/log_error/log_success(msg)`
  - Colored console output

### Cloud Target Configurations

#### local
- Platform: auto-detect (`platform.machine()` mapped to `linux/arm64` or `linux/amd64`)
- Registry: `tmi` (local Docker daemon)
- Dockerfile: Chainguard variants (server: `Dockerfile.server`, redis: `Dockerfile.redis`)
- Auth: none
- Build method: `docker build` (no buildx needed)
- `--push`: errors (see CLI behaviors above)

#### oci
- Platform: `linux/arm64` (OKE Ampere A1.Flex nodes) by default
- Registry: `{region}.ocir.io/{namespace}/{repo}` (from `--registry`, or auto-discovered)
- Dockerfile: Oracle variants (server: `Dockerfile.server-oracle`, redis: `Dockerfile.redis-oracle`)
- Auth: `docker login {region}.ocir.io` with OCI auth token
- Build method: `docker buildx build`
- Promtail: uses `Dockerfile.promtail` (Chainguard-based, same as local). Promtail is a logging sidecar and does not need Oracle Linux base.

**OCI auto-discovery logic** (when `--registry` is not provided):
1. Read `OCI_REGION` env var, or default to `us-ashburn-1`
2. Read OCI CLI profile from `OCI_CLI_PROFILE` env var, or default to `tmi`
3. Get namespace: `oci os ns get --profile {profile}` → parse JSON `.data`
4. Look for `CONTAINER_REPO_OCID` env var; if set, use it
5. Otherwise, look in `terraform/environments/oci-*/terraform.tfvars` for `container_repo_ocid`
6. Otherwise, search via OCI CLI: `oci artifacts container repository list --compartment-id {compartment}` and prompt user to select
7. Construct registry URL: `{region}.ocir.io/{namespace}/{repo_name}`

#### aws
- Platform: `linux/amd64` by default
- Registry: `{account}.dkr.ecr.{region}.amazonaws.com/tmi` (from `--registry`, or auto-discovered via `aws sts get-caller-identity` + `aws configure get region`)
- Dockerfile: Chainguard variants
- Auth: `aws ecr get-login-password --region {region} | docker login --username AWS --password-stdin {registry}`
- Build method: `docker buildx build`
- Auto-creates ECR repos if they don't exist (`aws ecr create-repository`)

#### azure
- Platform: `linux/amd64` by default
- Registry: `{acr_name}.azurecr.io/tmi` (from `--registry`)
- Dockerfile: Chainguard variants
- Auth: `az acr login --name {acr_name}`
- Build method: `docker buildx build`
- `--registry` is required (no auto-discovery); errors if not provided

#### gcp
- Platform: `linux/amd64` by default
- Registry: `{region}-docker.pkg.dev/{project}/{repo}` (from `--registry`)
- Dockerfile: Chainguard variants
- Auth: `gcloud auth configure-docker {region}-docker.pkg.dev`
- Build method: `docker buildx build`
- `--registry` is required (no auto-discovery); errors if not provided

#### heroku
- Platform: `linux/amd64`
- Registry: `registry.heroku.com/{app}/web`
- Dockerfile: Chainguard variants
- Auth: `heroku container:login`
- Build method: `docker build` then `docker push registry.heroku.com/{app}/web`
- Only server component is relevant; redis/promtail auto-skipped with warning
- Heroku app name from `--registry` (interpreted as app name) or `HEROKU_APP` env var; errors if neither set

### Dockerfile Selection Logic

The script selects Dockerfiles based on target and component:

| Component | local/aws/azure/gcp/heroku | oci |
|-----------|---------------------------|-----|
| server    | `Dockerfile.server` | `Dockerfile.server-oracle` |
| redis     | `Dockerfile.redis` | `Dockerfile.redis-oracle` |
| promtail  | `Dockerfile.promtail` | `Dockerfile.promtail` |
| postgres  | `Dockerfile.postgres` | N/A (managed service) |

**`--db-backend` interaction with `--target`:** The `--db-backend oracle-adb` flag selects `Dockerfile.server-oracle` regardless of target. This supports the use case of running an Oracle ADB-connected server on any cloud (e.g., `--target aws --db-backend oracle-adb` builds the Oracle client-enabled server and pushes to ECR). When `--db-backend postgresql` (default), the target determines the Dockerfile. `--db-backend` only affects the server component; redis/promtail/postgres are unaffected.

**Dockerfiles are NOT consolidated.** The Chainguard Redis (29 lines, pre-built image) and Oracle Redis (267 lines, compiled from source with entrypoint) are too different to merge with build args.

### Tagging Strategy

Each component image is named `{image_name_prefix}{component}` and tagged with:
- `:latest` (e.g., `tmi/tmi-server:latest`)
- `:v{major}.{minor}.{patch}` (e.g., `tmi/tmi-server:v0.10.3`)
- `:{git_short_hash}` (e.g., `tmi/tmi-server:abc1234`)

For cloud targets, `{image_name_prefix}` includes the full registry path (e.g., `us-ashburn-1.ocir.io/namespace/tmi/tmi-`).

### Makefile Targets

Replace ~25 container build targets with:

```makefile
# ---- App Container Builds ----
build-app:                    ## Build app containers for local development
	@uv run scripts/build-app-containers.py --target local

build-app-scan:               ## Build app containers locally with security scanning
	@uv run scripts/build-app-containers.py --target local --scan

build-app-oci:                ## Build and push app containers for OCI
	@uv run scripts/build-app-containers.py --target oci --push --scan

build-app-aws:                ## Build and push app containers for AWS
	@uv run scripts/build-app-containers.py --target aws --push --scan

build-app-azure:              ## Build and push app containers for Azure
	@uv run scripts/build-app-containers.py --target azure --push --scan

build-app-gcp:                ## Build and push app containers for GCP
	@uv run scripts/build-app-containers.py --target gcp --push --scan

build-app-heroku:             ## Build and push server container for Heroku
	@uv run scripts/build-app-containers.py --target heroku --component server --push

# ---- DB Container Builds ----
build-db:                     ## Build database containers for local development
	@uv run scripts/build-db-containers.py --target local

build-db-scan:                ## Build database containers locally with security scanning
	@uv run scripts/build-db-containers.py --target local --scan

# ---- Individual Component Builds (convenience) ----
build-server:                 ## Build only the TMI server container locally
	@uv run scripts/build-app-containers.py --target local --component server

build-redis:                  ## Build only the Redis container locally
	@uv run scripts/build-app-containers.py --target local --component redis

# ---- Combined Builds ----
build-all: build-db build-app ## Build all containers for local development

build-all-scan: build-db-scan build-app-scan ## Build all containers with scanning

# ---- Scanning ----
scan-containers:              ## Scan existing container images for vulnerabilities
	@uv run scripts/build-app-containers.py --scan-only $(if $(TARGET),--target $(TARGET),)

# ---- Dev Environment ----
start-containers-environment: build-all ## Build containers then start dev environment
	@$(MAKE) start-database
	@$(MAKE) start-redis

# ---- Backward Compatibility (deprecated, will be removed) ----
build-container-db: build-db
build-container-redis: build-redis
build-container-tmi: build-server
build-containers: build-all
build-containers-all: build-all-scan
build-container-oracle: build-app-oci
build-containers-oracle-push: build-app-oci
containers-dev: start-containers-environment
report-containers: scan-containers
```

The `REGISTRY_PREFIX` Makefile variable is deprecated. Cloud targets now determine registry URLs via `--registry` flag or auto-discovery. For custom local image naming, use `--registry` directly.

### Files to Remove

After the new system is verified working:

**Shell scripts (replaced):**
- `scripts/build-containers.sh`
- `scripts/build-container-oracle.sh`
- `scripts/build-containers-multiarch.sh`
- `scripts/make-containers-dev-local.sh`
- `scripts/build-promtail-container.sh`

**Makefile targets (replaced):** All targets from the `Container Builds` and `Multi-Architecture` sections (~lines 980-1133), replaced by the new targets above. The `report-containers` target is also replaced (reporting is now part of `--scan`).

### Files Unchanged

- All 6 Dockerfiles remain as-is
- `.dockerignore` remains
- `docker-compose.prod.yml` remains
- Start/stop/clean container targets remain (they run containers, not build them)
- `check-grype`, `check-cyclonedx` targets remain (prerequisite checks, called by Python scripts internally but kept as standalone targets for manual use)
- `generate-sbom` target remains (Go-level SBOM, independent of container builds)

## Error Handling

Python provides structured error handling that shell scripts lack:

- **Missing tools**: `check_prerequisites()` verifies docker daemon is running (`docker info`), buildx builder availability (not just CLI), grype, and syft. Reports ALL missing tools at once.
- **Docker daemon not running**: Detected via `docker info` failure. Error: "Docker daemon is not running. Start Docker Desktop or the Docker service."
- **Buildx builder missing**: When multi-arch or cloud push is needed, verify a builder supporting the target platform exists. Error with instructions to create one.
- **Auth failures**: Catch auth command failures with specific messages per cloud provider, including common fixes (e.g., "Run `aws configure` to set up credentials" or "Run `oci session authenticate` to refresh session")
- **Build failures**: Capture docker build stderr, report the build stage that failed
- **Push failures**: Distinguish auth vs network vs registry errors
- **Version file**: Validate `.version` JSON structure has required keys. Error: "Cannot read .version file; ensure it exists and contains valid JSON with major, minor, patch, prerelease keys"
- **Multi-arch without push**: `--arch both` without `--push` errors: "Multi-arch builds require --push (cannot load multi-platform images into local Docker daemon). Use --push with a registry, or build for a single architecture."

All errors use `sys.exit(1)` with colored error messages. No silent failures.

## Verification

### Build Verification
1. `make build-server` — builds server container locally
2. `make build-redis` — builds redis container locally
3. `make build-db` — builds postgres container locally
4. `make build-all` — builds all containers
5. `make build-app-scan` — builds with Grype scanning and generates reports
6. `make scan-containers` — scans existing images without rebuilding
7. Verify backward-compatible aliases work: `make build-container-tmi`, `make build-containers`, `make containers-dev`

### Functional Verification
1. `make start-dev` — start development environment using locally-built containers
2. Verify server starts and responds on port 8080
3. Verify redis connection works
4. Verify database connection works

### Cloud Target Verification (requires credentials)
1. `make build-app-oci` — builds and pushes to OCI Container Registry
2. Verify images appear in OCIR with correct tags and architectures

### Regression Check
1. All existing `start-*`, `stop-*`, `clean-*` targets still work
2. `make test-unit` passes
3. `make test-integration` passes
