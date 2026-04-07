# Design: Move Makefile Logic to Scripts

**Issue:** [#215](https://github.com/ericfitz/tmi/issues/215)
**Date:** 2026-04-06
**Status:** Approved

## Problem

The TMI Makefile is 1682 lines with substantial inline shell logic in many targets. This makes it hard to test, debug, and reuse the logic outside of Make. The project wants to be able to switch build frameworks (e.g., to `just`) without rewriting all the logic.

## Design Decisions

1. **Scripting language:** Python by default (via `uv run`), shell only for trivially simple tasks.
2. **Shared library:** `scripts/lib/tmi_common.py` provides logging, config loading, Docker management, process lifecycle, command execution, and CLI helpers.
3. **Configuration:** Scripts read defaults from YAML config files (e.g., `config-development.yml`). CLI flags override config values.
4. **Makefile macros:** Remove entirely (`graceful_kill`, `kill_port`, `ensure_container`, `wait_for_ready`, color/logging defines) once all consumers are migrated.
5. **Framework-agnostic scripts:** Each script is a standalone entry point. Make is a thin pass-through. No script depends on being called from Make.
6. **Migration approach:** Bottom-up by domain. Each domain is a self-contained commit.

## Shared Library: `scripts/lib/tmi_common.py`

A single Python module providing:

- **Logging** — colored output matching current `[INFO]`, `[SUCCESS]`, `[WARNING]`, `[ERROR]` format with emoji indicators. Functions: `log_info()`, `log_success()`, `log_warning()`, `log_error()`.
- **Config loading** — reads YAML config files, exposes values with dot-path access. Handles missing keys gracefully with defaults. Function: `load_config(path, overrides)`.
- **Docker helpers** — `ensure_container(name, port, image, env_vars, volumes)`, `stop_container(name)`, `remove_container(name, volumes)`, `container_is_running(name)`, `wait_for_container_ready(health_cmd, timeout, label)`.
- **Process management** — `graceful_kill(pid)`, `kill_port(port)`, `is_port_in_use(port)`, `wait_for_port(port, timeout, label)`, `read_pid_file(path)`, `write_pid_file(path, pid)`.
- **Command execution** — thin wrapper around `subprocess.run` with logging, error handling, and optional output capture. Function: `run_cmd(cmd, check, capture, env)`.
- **CLI helpers** — common argparse patterns: `add_config_arg(parser)` (adds `--config` flag defaulting to `config-development.yml`), `add_verbosity_args(parser)` (adds `--verbose`/`--quiet` flags).

All scripts use inline uv TOML for dependency management (per project convention). The shared library is imported by adding the `scripts/lib` directory to `sys.path` at the top of each script:

```python
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import log_info, load_config, ...
```

The `scripts/lib/` directory will contain an `__init__.py` (empty) alongside `tmi_common.py`.

## Script Inventory

### Domain 1: Infrastructure Management

| Makefile Target | Script | What Moves |
|---|---|---|
| `start-database` | `scripts/manage-database.py start` | Container creation, volume management, port binding, env vars |
| `stop-database` | `scripts/manage-database.py stop` | Container stop |
| `clean-database` | `scripts/manage-database.py clean` | Container + volume removal |
| `start-redis` | `scripts/manage-redis.py start` | Container creation, port binding |
| `stop-redis` | `scripts/manage-redis.py stop` | Container stop |
| `clean-redis` | `scripts/manage-redis.py clean` | Container removal |
| `start-test-database` | `scripts/manage-database.py start --test` | Ephemeral test container on port 5433 |
| `stop-test-database` | `scripts/manage-database.py stop --test` | Test container stop |
| `clean-test-database` | `scripts/manage-database.py clean --test` | Test container removal |
| `start-test-redis` | `scripts/manage-redis.py start --test` | Test Redis on port 6380 |
| `stop-test-redis` | `scripts/manage-redis.py stop --test` | Test container stop |
| `clean-test-redis` | `scripts/manage-redis.py clean --test` | Test container removal |
| `wait-database` | `scripts/manage-database.py wait` | pg_isready polling loop |
| `migrate-database` | `scripts/manage-database.py migrate` | Run Go migration tool |
| `reset-database` | `scripts/manage-database.py reset` | Drop + recreate + migrate |
| `wait-test-database` | `scripts/manage-database.py wait --test` | Test DB readiness |
| `migrate-test-database` | `scripts/manage-database.py migrate --test` | Test DB migrations |

### Domain 2: Process Management

| Makefile Target | Script | What Moves |
|---|---|---|
| `start-server` | `scripts/manage-server.py start` | Port check, binary launch, PID file, startup verification |
| `stop-server` | `scripts/manage-server.py stop` | 3-layer kill (PID file, process name, port), verification |
| `wait-process` | `scripts/manage-server.py wait` | Curl polling loop |
| `start-dev` | `scripts/start-dev.py` | Orchestrates DB + Redis + wait + server start |
| `restart-dev` | `scripts/start-dev.py --restart` | Stop + rebuild + clean + start |

### Domain 3: Testing

| Makefile Target | Script | What Moves |
|---|---|---|
| `test-unit` | `scripts/run-unit-tests.py` | Test execution, output formatting, summary generation |
| `test-coverage` | `scripts/run-coverage.py` | Full coverage orchestration (unit + integration + merge + generate) |
| `test-coverage-unit` | `scripts/run-coverage.py --unit-only` | Unit coverage |
| `test-coverage-integration` | `scripts/run-coverage.py --integration-only` | Integration coverage |
| `merge-coverage` | `scripts/run-coverage.py --merge-only` | Coverage merging |
| `generate-coverage` | `scripts/run-coverage.py --generate-only` | HTML/text report generation |

### Domain 4: OAuth Stub

| Makefile Target | Script | What Moves |
|---|---|---|
| `start-oauth-stub` | `scripts/manage-oauth-stub.py start` | Kill existing, launch daemon, readiness poll |
| `stop-oauth-stub` | `scripts/manage-oauth-stub.py stop` | Graceful shutdown (magic URL, SIGTERM, SIGKILL) |
| `kill-oauth-stub` | `scripts/manage-oauth-stub.py kill` | Force kill port 8079 |
| `check-oauth-stub` | `scripts/manage-oauth-stub.py status` | PID file + process check |

### Domain 5: CATS Fuzzing

| Makefile Target | Script | What Moves |
|---|---|---|
| `cats-fuzz` | `scripts/run-cats-fuzz.sh` (enhance existing) | Argument building, tool check, seed + fuzz orchestration |
| `cats-seed` | `scripts/run-cats-seed.py` | Build seed tool, run it |
| `cats-seed-oci` | `scripts/run-cats-seed.py --oci` | OCI variant |

### Domain 6: Validation

| Makefile Target | Script | What Moves |
|---|---|---|
| `validate-openapi` | `scripts/validate-openapi.py` (new, replaces inline logic) | jq syntax check + Vacuum + error reporting |
| `check-unsafe-union-methods` | `scripts/check-unsafe-union-methods.py` | Grep for unsafe method calls |

### Domain 7: Status & Utilities

| Makefile Target | Script | What Moves |
|---|---|---|
| `status` | `scripts/status.py` | Service status checking (server, DB, Redis, app, OAuth stub) |
| `clean-files` | `scripts/clean.py files` | File + PID + CATS artifact cleanup |
| `clean-logs` | `scripts/clean.py logs` | Log cleanup |
| `clean-everything` | `scripts/clean.py all` | Orchestrates full cleanup |

### Domain 8: Remaining

| Makefile Target | Script | What Moves |
|---|---|---|
| `build-server` | `scripts/build-server.py` | Version reading from `.version`, ldflags, go build |
| `build-migrate` | `scripts/build-server.py --component migrate` | Migration tool build |
| `generate-api` | `scripts/generate-api.py` | oapi-codegen invocation |
| `generate-sbom` | `scripts/generate-sbom.py` | cyclonedx-gomod + optional module SBOMs |
| `deploy-heroku` | `scripts/deploy-heroku.py` | Build + commit + push |
| `lint` | `scripts/lint.py` | Unsafe union check + golangci-lint |
| `wstest` | `scripts/run-wstest.py` | Terminal spawning for WS test |
| `help` | `scripts/help.py` | Help text generation |

## Not Extracted

These targets already delegate to scripts or are trivial one-liner commands:

- `test-integration-pg`, `test-integration-oci` — already call shell scripts
- `build-app*`, `build-db*`, `scan-containers` — already call Python scripts
- `setup-heroku*`, `reset-db-heroku`, `drop-db-heroku` — already call scripts
- `tf-*` — simple one-liner terraform commands
- `fn-*` — simple one-liner fn CLI commands
- `deploy-oci*`, `deploy-aws*` — already call scripts
- `arazzo-*` — already call scripts (except `arazzo-install` which is just `pnpm install`)
- Backward compatibility aliases (`build`, `test`, `lint`, `clean`, `dev`) — remain as Make aliases pointing to the primary targets

## Migration Strategy

Each domain follows this pattern:

1. Create/extend the shared library with capabilities the domain needs
2. Write the new script(s) with inline uv TOML, importing from the shared library
3. Update the Makefile target to call the script
4. Test that `make <target>` still works identically
5. Commit the domain as one conventional commit

**Domain ordering:** 1 through 8 as listed above. Infrastructure first (establishes library patterns), process management second (used by testing and CATS), then the rest in dependency order.

**Final cleanup:** After all domains are migrated, remove the Makefile macros (`graceful_kill`, `kill_port`, `ensure_container`, `wait_for_ready`), color/logging `define` blocks, and any dead variable definitions.

## End State

- **Makefile:** ~200-300 lines of thin one-liner targets plus `.PHONY` declarations and a few variable definitions (`VERSION`, `COMMIT`, etc.)
- **Shared library:** `scripts/lib/tmi_common.py` with all reusable logic
- **Scripts:** ~20 new Python scripts in `scripts/`, each standalone and framework-agnostic
- **Existing scripts:** Unchanged (already properly delegated)
