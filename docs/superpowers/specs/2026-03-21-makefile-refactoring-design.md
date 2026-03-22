# Makefile Refactoring Design

**Date:** 2026-03-21
**Status:** Draft
**Scope:** Makefile, scripts/test-framework.mk

## Summary

Refactor the TMI Makefile to remove dead code, fix bugs, consolidate duplicate targets, standardize naming, extract reusable macros, and fix the `test-coverage` trap. No new functionality is added.

## 1. Remove Dead Code

### 1a. Targets referencing missing scripts

Remove these targets entirely — they reference scripts that no longer exist:

| Target | Missing Script | Notes |
|--------|---------------|-------|
| `cats-fuzz-prep` | `scripts/cats-prepare-database.sh` | Already marked DEPRECATED |
| `cats-set-max-quotas` | `scripts/cats-set-max-quotas.sh` | Already marked DEPRECATED |

### 1b. Redundant aliases

Remove these targets that add no value beyond what already exists:

| Target | Points to | Why remove |
|--------|-----------|-----------|
| `build-everything` | `build-server` | `build` alias already exists |
| `build-server-sbom` | `build-with-sbom` | Exact duplicate alias |
| `clean-dev` | `clean-everything` | `clean` alias already exists |

Note: `start-service` and `stop-service` are kept as aliases for `start-server` and `stop-server`.

### 1c. Unused targets

| Target | Why remove |
|--------|-----------|
| `execute-tests-unit` | Not called by anything. `test-unit` is the real target |
| `check-syft` | Not called by any Makefile target. Container build scripts check for syft internally |
| Empty "DISTROLESS CONTAINER MANAGEMENT" section (lines 1506-1508) | No targets, just a header |

### 1d. Trivial variant targets to remove

These are thin wrappers that can be replaced by passing variables to the base target:

| Target | Replacement |
|--------|-------------|
| `test-api-oci` | `make test-api RESPONSE_TIME_MULTIPLIER=4` |
| `start-dev-0` | Remove entirely (not used) |
| `generate-sbom-all` | `make generate-sbom ALL=true` |
| `arazzo-all` | Functionally equivalent to `generate-arazzo` since `arazzo-install` is already a transitive dependency via `arazzo-scaffold` — the extra direct dependency adds no value. Remove and keep `generate-arazzo` |

Note: `start-dev-oci` is kept as-is because it has fundamentally different behavior (different config, Oracle build tags, OCI environment sourcing) that cannot be reduced to a single variable on `start-dev`.

## 2. Fix Bugs

### 2a. `deploy-heroku` uses undefined color variables

`deploy-heroku` uses `$(COLOR_BLUE)`, `$(COLOR_GREEN)`, `$(COLOR_RESET)` which are not defined anywhere. The Makefile defines `$(BLUE)`, `$(GREEN)`, `$(NC)`.

**Fix:** Rewrite `deploy-heroku` to use `$(call log_info,...)` / `$(call log_success,...)` macros like all other targets.

### 2b. `deploy-oci` default TF_ENV doesn't match any directory

`TF_ENV` defaults to `oci-production` but no such directory exists. The actual directories are `oci-public` and `oci-private`.

**Fix:** Change `TF_ENV ?= oci-production` to `TF_ENV ?= oci-public`. Also update the `deploy-oci` and `deploy-oci-plan` target-specific variable assignments from `TF_ENV=oci-production` to `TF_ENV=oci-public`.

### 2c. `test-framework-check` references wrong OpenAPI spec path

Line 160 checks for `docs/reference/apis/tmi-openapi.json` which doesn't exist. The spec is at `api-schema/tmi-openapi.json`.

**Fix:** Update the path.

### 2d. `help` text references non-existent config file

Help text lists `config/coverage-report.yml` which doesn't exist.

**Fix:** Remove it from the help output.

### 2e. `scan-containers` references non-existent `Dockerfile.dev`

The application scan block builds from `Dockerfile.dev` which doesn't exist.

**Fix:** Remove the `Dockerfile.dev` conditional block. The existing container images can be scanned directly.

### 2f. `cats-fuzz-path` uses raw ANSI codes instead of variables

Line 1119 has hardcoded `\033[0;31m` instead of using `$(RED)`.

**Fix:** Use `$(call log_error,...)`. (This target is being removed in Section 3, so this is moot — listed for completeness.)

## 3. Consolidate CATS Targets

Reduce 7 CATS fuzz targets to 2. Keep `cats-fuzz` (PostgreSQL) and `cats-fuzz-oci` (Oracle). Remove:

- `cats-fuzz-user`
- `cats-fuzz-server`
- `cats-fuzz-custom`
- `cats-fuzz-path`
- `cats-fuzz-full`

Modify `cats-fuzz` to accept optional variables and pass them through to `run-cats-fuzz.sh`.

**Important:** Use `FUZZ_USER` instead of `USER` to avoid conflict with the shell's built-in `$USER` environment variable (which is always set, e.g., `USER=efitz`). Using `$(USER)` directly would always pass `-u efitz` even when no user is specified.

```makefile
# Usage:
#   make cats-fuzz                                       # defaults (charlie, localhost:8080)
#   make cats-fuzz FUZZ_USER=alice                       # custom user
#   make cats-fuzz FUZZ_SERVER=http://host:8080          # custom server
#   make cats-fuzz ENDPOINT=/addons                      # specific endpoint
#   make cats-fuzz BLACKBOX=true                         # blackbox mode
#   make cats-fuzz FUZZ_USER=alice ENDPOINT=/addons      # combine any options
cats-fuzz: cats-seed
	$(call log_info,"Running CATS API fuzzing...")
	@ARGS=""; \
	if [ -n "$(FUZZ_USER)" ]; then ARGS="$$ARGS -u $(FUZZ_USER)"; fi; \
	if [ -n "$(FUZZ_SERVER)" ]; then ARGS="$$ARGS -s $(FUZZ_SERVER)"; fi; \
	if [ -n "$(ENDPOINT)" ]; then ARGS="$$ARGS -p $(ENDPOINT)"; fi; \
	if [ "$(BLACKBOX)" = "true" ]; then ARGS="$$ARGS -b"; fi; \
	./scripts/run-cats-fuzz.sh $$ARGS
```

`cats-fuzz-oci` gets the same treatment but with `cats-seed-oci` as its prerequisite.

## 4. Remove `start-dev-0`

Remove `start-dev-0` entirely. It is unused.

`start-dev-oci` is kept as-is — it uses a different config file, build tags, OCI environment sourcing, and runs a separate shell script.

## 5. Consolidate SBOM Targets

Modify `generate-sbom` to accept an optional `ALL` variable:

```makefile
generate-sbom: check-cyclonedx
	# ... generate app SBOM ...
	@if [ "$(ALL)" = "true" ]; then \
		# ... also generate module SBOM ...
	fi
```

## 6. Naming Consistency

### 6a. Clean targets: standardize on `clean-*` prefix

| Current Name | New Name |
|-------------|----------|
| `test-clean` | `clean-test-outputs` |
| `test-outputs-clean-integration` | Remove — `clean-test-outputs` already cleans integration outputs along with everything else |

Add temporary backward-compatibility alias with deprecation warning:

```makefile
test-clean: clean-test-outputs
	$(call log_warning,"'test-clean' is deprecated. Use 'clean-test-outputs'.")
```

### 6b. Check targets: keep namespace prefix

The `fn-check` and `tf-check` naming is appropriate (scoped to their namespace). But `test-framework-check` breaks the pattern.

| Current Name | New Name |
|-------------|----------|
| `test-framework-check` | `check-test-framework` |

Add backward-compatibility alias:

```makefile
test-framework-check: check-test-framework
	$(call log_warning,"'test-framework-check' is deprecated. Use 'check-test-framework'.")
```

## 7. Extract Reusable Macros

All macros omit `@` inside `define` blocks — the `@` belongs on the recipe line at the call site (e.g., `@$(call graceful_kill,...)`). Inside a `define`, the `@` would be passed as a literal character to the shell.

### 7a. `graceful_kill` — process kill with SIGTERM→SIGKILL

```makefile
# Usage: @$(call graceful_kill,PID_VALUE)
define graceful_kill
PID=$(1); \
if [ -n "$$PID" ] && ps -p $$PID > /dev/null 2>&1; then \
	kill $$PID 2>/dev/null || true; \
	sleep 1; \
	if ps -p $$PID > /dev/null 2>&1; then \
		kill -9 $$PID 2>/dev/null || true; \
	fi; \
fi
endef
```

Apply to: `stop-server`, `clean-process`, `stop-oauth-stub`.

### 7b. `kill_port` — kill all processes on a port

```makefile
# Usage: @$(call kill_port,PORT_NUMBER)
define kill_port
PIDS=$$(lsof -ti :$(1) 2>/dev/null || true); \
if [ -n "$$PIDS" ]; then \
	for PID in $$PIDS; do \
		kill $$PID 2>/dev/null || true; \
	done; \
	sleep 1; \
	PIDS=$$(lsof -ti :$(1) 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		for PID in $$PIDS; do \
			kill -9 $$PID 2>/dev/null || true; \
		done; \
	fi; \
fi
endef
```

Apply to: `stop-server` Layer 3, `stop-process`, `kill-oauth-stub`.

### 7c. `ensure_container` — start or create Docker container

```makefile
# Usage: @$(call ensure_container,NAME,HOST_PORT,CONTAINER_PORT,IMAGE,EXTRA_DOCKER_ARGS)
# EXTRA_DOCKER_ARGS: env vars, volume mounts, etc. (can be empty)
define ensure_container
if ! docker ps -a --format "{{.Names}}" | grep -q "^$(1)$$"; then \
	echo -e "$(BLUE)[INFO]$(NC) Creating container $(1)..."; \
	docker run -d --name $(1) -p 127.0.0.1:$(2):$(3) $(5) $(4); \
elif ! docker ps --format "{{.Names}}" | grep -q "^$(1)$$"; then \
	echo -e "$(BLUE)[INFO]$(NC) Starting container $(1)..."; \
	docker start $(1); \
fi; \
echo "✅ $(1) running on port $(2)"
endef
```

Parameters: `(1)` name, `(2)` host port, `(3)` container port, `(4)` image, `(5)` extra docker args (env vars, volumes, etc.).

Note: `start-database` has significant per-target logic (reading config variables for user/password/database, volume mounts, data directory creation). The macro handles the container create-or-start pattern, but each target will still need wrapper logic to construct the `EXTRA_DOCKER_ARGS` from its config variables. This is still a net improvement — the idempotent start-or-create logic is no longer duplicated.

Apply to: `start-database`, `start-redis`, `start-test-database`, `start-test-redis`, `start-promtail`.

### 7d. `wait_for_ready` — poll until a health check passes

A generalized macro that accepts a health check command, applicable to both HTTP checks and Docker container checks:

```makefile
# Usage: @$(call wait_for_ready,HEALTH_CHECK_CMD,TIMEOUT_SECONDS,SERVICE_NAME)
# HEALTH_CHECK_CMD: shell command that returns 0 when ready
define wait_for_ready
timeout=$(2); \
while [ $$timeout -gt 0 ]; do \
	if $(1) >/dev/null 2>&1; then \
		echo -e "$(GREEN)[SUCCESS]$(NC) $(3) is ready!"; \
		break; \
	fi; \
	sleep 2; \
	timeout=$$((timeout - 2)); \
done; \
if [ $$timeout -le 0 ]; then \
	echo -e "$(RED)[ERROR]$(NC) $(3) failed to start within $(2) seconds"; \
	exit 1; \
fi
endef
```

Usage examples:
```makefile
# HTTP health check (wait-process):
@$(call wait_for_ready,curl -s http://localhost:8080/,300,Server)

# Docker container health check (wait-database):
@$(call wait_for_ready,docker exec tmi-postgresql pg_isready -U tmi_dev,300,Database)
```

Apply to: `wait-database`, `wait-test-database`, `wait-process`.

### 7e. Compose `clean-process` from existing targets

Instead of duplicating the 3-layer kill logic, `clean-process` composes from existing targets. `stop-server` already handles PID file, process name, and port cleanup. `stop-oauth-stub` already handles graceful shutdown and port cleanup. No additional `kill_port` calls are needed — the prerequisites are sufficient.

```makefile
clean-process: stop-server stop-oauth-stub
```

Note: The current `clean-process` also references `$(CLEANUP_PROCESSES)` for additional ports. This variable is not set anywhere in the Makefile or config files — it has no consumers. It is removed as dead code. If a future need arises for cleaning additional ports, it can be re-added.

## 8. Fix `test-coverage` Trap

Current: `trap 'clean-everything' EXIT` — destroys the entire dev environment.

**Fix:** Only clean what `test-coverage` creates:

```makefile
test-coverage:
	@trap '$(MAKE) clean-test-infrastructure' EXIT; \
	...
```

This cleans the test containers but leaves the dev database, dev Redis, and OAuth stub intact.

## 9. Update `help` Target

After all changes:
- Remove references to deleted targets (`cats-fuzz-prep`, `cats-set-max-quotas`, `build-everything`, `build-server-sbom`, `clean-dev`, `test-api-oci`, `start-dev-0`, `generate-sbom-all`, `arazzo-all`, `check-syft`, and the 5 removed CATS targets)
- Add documentation for variable-based usage (e.g., `cats-fuzz FUZZ_USER=alice`)
- Remove the `config/coverage-report.yml` line
- Update integration test comments to reflect that server is no longer started/stopped by test scripts

## 10. Update `.PHONY` Declarations

Remove deleted targets from all `.PHONY` lines. Ensure new/renamed targets are declared. Add backward-compat aliases to `.PHONY`.

## 11. Update `test-help` in test-framework.mk

After renames, update the help text in `test-help` to use new target names and remove references to `stop-oauth-stub` (since scripts no longer stop it).

## Files Changed

| File | Changes |
|------|---------|
| `Makefile` | All of the above |
| `scripts/test-framework.mk` | Rename `test-clean` → `clean-test-outputs`, `test-outputs-clean-integration` → `clean-test-outputs-integration`, `test-framework-check` → `check-test-framework`, fix OpenAPI path, add deprecation aliases, update `test-help` |

## What Is NOT Changing

- Shell scripts (already refactored in prior commit)
- Go source code
- OpenAPI spec
- Container build scripts
- Test collections
- `start-dev-oci` (kept as-is — fundamentally different from `start-dev`)
- Any functionality — this is purely structural cleanup
