# Dev Environment Rationalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the tangled, half-migrated set of dev-environment scripts and make targets with a single `scripts/devenv.py` orchestrator exposing a consistent `dev-*` verb set, backed by focused `scripts/lib/` modules, and delete every orphaned local-process path left over from the pre-kind era.

**Architecture:** All dev-lifecycle logic lives in three importable library modules — `lib/cluster.py` (kind cluster + local registry), `lib/deploy.py` (image build/push + kubectl apply/rollout/port-forward + teardown), `lib/database.py` (postgres container lifecycle, shared with the test path) — plus `lib/devstatus.py` (kind-aware status dashboard). `scripts/devenv.py` is a thin argparse dispatcher over these modules. Make targets are 1:1 wrappers (`make dev-up` → `devenv.py up`). The kind cluster is the only dev path; the database is an external postgres container (dev) or external ADB (oracle).

**Tech Stack:** Python 3.11+ run via `uv` (PEP 723 inline script metadata), `kind`, `kubectl`, `kustomize`, `docker`, GNU Make. Existing shared helpers in `scripts/lib/tmi_common.py`. Unit tests under `scripts/lib/tests/` (pytest-style, run via `uv run`).

## Global Constraints

- **Make is a thin wrapper only.** No lifecycle logic in the Makefile — every recipe is `@uv run scripts/devenv.py <verb> ...` (or another single script invocation). (User constraint.)
- **kind is the only dev server path.** The server, Redis, NATS, KEDA, controller, and both workers run in-cluster. The database is external: a local postgres container for `DB=postgres`, external Oracle ADB for `DB=oracle`. Never reintroduce a host `bin/tmiserver` process.
- **Logging:** Python scripts use `tmi_common` `log_info` / `log_success` / `log_warn` / `log_error`. Never `print`-based logging for status lines except the deliberate dashboard rendering in `lib/devstatus.py`. (Go logging rule from CLAUDE.md does not apply — no Go changes here.)
- **Inline script metadata:** every executable script keeps its `# /// script` PEP 723 header with `requires-python = ">=3.11"` and explicit `dependencies`.
- **Namespace / names (verbatim):** kind cluster `tmi-dev`; kube context `kind-tmi-dev`; platform namespace `tmi-platform`; registry container `tmi-dev-registry`; local registry `localhost:5000`; kind config `deployments/k8s/dev/kind-cluster.yml`; dev config file `config-development.yml`.
- **DB flavor parameter:** `DB=postgres|oracle`, default `postgres`. Oracle uses the `deployments/k8s/dev/oracle` overlay + `server-oracle.yml` and requires `TMI_ORACLE_WALLET_ZIP`.
- **Capability-preserving Oracle gate:** do NOT delete `start-dev-oci.sh` until `devenv.py up --db oracle` is proven to deploy in-cluster and reach the database (Task 9).
- **Testing make-target rule (CLAUDE.md):** never run `go test` directly; use `make test-unit`. The dev-script unit tests here are Python and run via `uv run` / the existing `make test-dev-scripts` target.
- **Tooling fix order (CLAUDE.md task-completion):** after any change run `make lint`; if Go changed, `make build-server` + `make test-unit`. (Only Python/Make/docs change here, so lint + the Python script tests are the gates.)

---

## File Structure

**Create:**
- `scripts/devenv.py` — orchestrator; argparse verbs `up down restart reset nuke status deploy logs cluster db`. The only script `make` calls for dev lifecycle.
- `scripts/lib/cluster.py` — kind cluster + local registry lifecycle (absorbs `scripts/dev-cluster.py` + registry/image helpers from `lib/devenv.py`).
- `scripts/lib/deploy.py` — image build/push, kubectl apply/kustomize, rollout wait, port-forward, teardown, oracle overlay (absorbs `scripts/start-dev.py` + manifest/configmap/context helpers from `lib/devenv.py`).
- `scripts/lib/database.py` — postgres container lifecycle + migrate (single source; shared by `devenv.py` and `manage-database.py`).
- `scripts/lib/devstatus.py` — kind-aware status dashboard (replaces `scripts/status.py`).
- `scripts/lib/tests/test_cluster.py`, `test_deploy.py`, `test_devstatus.py` — unit tests for the pure functions in each module.

**Modify:**
- `scripts/manage-database.py` — refactor command bodies to delegate to `lib/database.py` (keeps `--test`, `--config`, all existing subcommands).
- `scripts/clean.py` — drop `manage-server.py` / `manage-workers.py` calls and the `_server_state` import; keep logs/files/containers cleanup.
- `Makefile` — replace dev-lifecycle target block with `dev-*` wrappers + deprecated aliases; rewire `stop-all`, `dev`, `status`; delete `start-server`/`stop-server`/`start-workers`/`stop-workers`/`start-dev-oci` recipes.
- `scripts/test-framework.mk` — update comment/warning references (`make start-dev` → `make dev-up`).
- `Tiltfile` — update comment references to new target names.

**Delete:**
- `scripts/dev-cluster.py`, `scripts/start-dev.py`, `scripts/start-dev-oci.sh`, `scripts/status.py`, `scripts/manage-server.py`, `scripts/manage-workers.py`, `scripts/lib/devenv.py`, `scripts/lib/_server_state.py`, `scripts/lib/tests/test_devenv.py` (its pure-function tests move into `test_cluster.py`/`test_deploy.py`).

**Phasing:** Tasks 1–6 build the new modules + orchestrator while the old scripts still work (nothing deleted). Task 7 cuts the Makefile over. Tasks 8–9 verify (incl. the Oracle gate) and delete the old scripts. Task 10 is final verification + DB-review + docs.

---

## Task 1: Extract `lib/cluster.py` (kind cluster + registry)

**Files:**
- Create: `scripts/lib/cluster.py`
- Create: `scripts/lib/tests/test_cluster.py`
- Reference (move FROM, do not delete yet): `scripts/dev-cluster.py:21-141`, `scripts/lib/devenv.py:15-35`

**Interfaces:**
- Produces (constants): `CLUSTER_NAME="tmi-dev"`, `REGISTRY_CONTAINER="tmi-dev-registry"`, `REGISTRY_IMAGE="registry:2"`, `REGISTRY_PORT=5000`, `LOCAL_REGISTRY="localhost:5000"`, `KIND_CONFIG` (abs path to `deployments/k8s/dev/kind-cluster.yml`).
- Produces (pure): `local_image_ref(name, tag="dev", registry=LOCAL_REGISTRY) -> str`, `is_local_kube_context(name: str) -> bool`.
- Produces (shell wrappers): `ensure_registry() -> None`, `connect_registry_to_kind() -> None`, `cluster_exists() -> bool`, `up() -> None`, `stop() -> None`, `down() -> None`, `is_registry_running() -> bool` (NEW thin wrapper around `container_is_running(REGISTRY_CONTAINER)`, consumed by `devstatus`).

- [ ] **Step 1: Write failing unit tests for the pure helpers**

Create `scripts/lib/tests/test_cluster.py`:

```python
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import cluster  # noqa: E402


def test_local_image_ref_default_tag():
    assert cluster.local_image_ref("tmi-server") == "localhost:5000/tmi-server:dev"


def test_local_image_ref_custom_tag():
    assert cluster.local_image_ref("tmi-server", tag="x") == "localhost:5000/tmi-server:x"


def test_is_local_kube_context_kind_prefix():
    assert cluster.is_local_kube_context("kind-tmi-dev") is True


def test_is_local_kube_context_known_exact():
    assert cluster.is_local_kube_context("docker-desktop") is True


def test_is_local_kube_context_remote_false():
    assert cluster.is_local_kube_context("arn:aws:eks:us-east-1:123:cluster/prod") is False


def test_is_local_kube_context_empty_false():
    assert cluster.is_local_kube_context("") is False
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_cluster.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'cluster'`.

- [ ] **Step 3: Create `lib/cluster.py`**

Build the module from the existing code:
- Copy the constants block and `local_image_ref` / `is_local_kube_context` from `scripts/lib/devenv.py:15-35` and the `_LOCAL_CONTEXT_*` sets from `:19-21`.
- Copy `ensure_registry` (use `scripts/dev-cluster.py:29-48` — the version that *creates* if missing), `cluster_exists` (`:51-58`), `_kind_node_containers` (`:61-75`), `start_stopped_nodes` (`:78-89`), `connect_registry_to_kind` (`:92-97`), `up` (`:100-115`), `stop` (`:118-135`), `down` (`:138-141`).
- Add the `KIND_CONFIG` constant (from `dev-cluster.py:22-23`).
- Add the new helper:

```python
from tmi_common import container_is_running

def is_registry_running() -> bool:
    """True if the local dev registry container is currently running."""
    return container_is_running(REGISTRY_CONTAINER)
```

Header + imports:

```python
"""kind cluster + local dev-registry lifecycle.

Pure helpers (local_image_ref, is_local_kube_context) are unit-tested in
scripts/lib/tests/test_cluster.py. Shell wrappers delegate to tmi_common.run_cmd
and are exercised against a live cluster by scripts/devenv.py.
"""
from __future__ import annotations

from pathlib import Path

from tmi_common import (
    check_tool, container_exists, container_is_running,
    log_info, log_success, run_cmd,
)

CLUSTER_NAME = "tmi-dev"
REGISTRY_CONTAINER = "tmi-dev-registry"
REGISTRY_IMAGE = "registry:2"
REGISTRY_PORT = 5000
LOCAL_REGISTRY = "localhost:5000"
_PROJECT_ROOT = Path(__file__).resolve().parents[2]
KIND_CONFIG = str(_PROJECT_ROOT / "deployments/k8s/dev/kind-cluster.yml")
```

Replace every `devenv.REGISTRY_CONTAINER` reference in the moved `dev-cluster.py` bodies with the local `REGISTRY_CONTAINER` constant.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_cluster.py -v`
Expected: PASS (6 passed).

- [ ] **Step 5: Commit**

```bash
git add scripts/lib/cluster.py scripts/lib/tests/test_cluster.py
git commit -m "refactor(devenv): extract lib/cluster.py for kind cluster + registry"
```

---

## Task 2: Extract `lib/deploy.py` (build/push + k8s apply + teardown)

**Files:**
- Create: `scripts/lib/deploy.py`
- Create: `scripts/lib/tests/test_deploy.py`
- Reference (move FROM, do not delete yet): `scripts/start-dev.py:30-519`, `scripts/lib/devenv.py:17,29-81`

**Interfaces:**
- Consumes: `cluster.LOCAL_REGISTRY`, `cluster.REGISTRY_CONTAINER`, `cluster.local_image_ref`, `cluster.ensure_registry`, `cluster.is_local_kube_context`.
- Produces (constants): `PLATFORM_NAMESPACE="tmi-platform"` (as `NS`), `CONFIGMAP_NAME`, `DEV_DIR`, `PLATFORM_DIR`, `CONFIG_FILE`, port-forward pidfile paths.
- Produces (pure): `image_builds_for(db: str) -> list[tuple[str,str,dict]]`, `overlay_dir_for(db: str) -> str`, `_no_workers_files(db: str) -> tuple[str,...]`, `render_configmap_yaml(*, name, namespace, file_key, content) -> str`, `content_hash(text) -> str`, `current_kube_context() -> str`, `kubectl(args, *, check=True, input_text=None)`.
- Produces (orchestration): `start(*, db: str, no_workers: bool=False, skip_context_guard: bool=False) -> None` (= old `do_start`), `restart(*, db, no_workers=False, skip_context_guard=False) -> None` (= old `do_restart`), `teardown(*, db: str="postgres") -> None` (= old `do_stop`), `stop_port_forward() -> None`, `tail_server_logs() -> None` (NEW), `remove_local_images(db: str) -> None` (NEW), `server_http_status() -> tuple[bool, str]` (NEW, consumed by `devstatus`).

- [ ] **Step 1: Write failing unit tests for the pure helpers**

Create `scripts/lib/tests/test_deploy.py`:

```python
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import deploy  # noqa: E402


def test_image_builds_postgres_server_image():
    names = [n for n, _df, _a in deploy.image_builds_for("postgres")]
    assert names[0] == "tmi-server"
    assert "tmi-extractor" in names and "tmi-chunk-embed" in names


def test_image_builds_oracle_server_image():
    names = [n for n, _df, _a in deploy.image_builds_for("oracle")]
    assert names[0] == "tmi-server-oracle"


def test_overlay_dir_oracle():
    assert deploy.overlay_dir_for("oracle").endswith("/oracle")


def test_overlay_dir_postgres():
    assert not deploy.overlay_dir_for("postgres").endswith("/oracle")


def test_no_workers_files_oracle_uses_oracle_server():
    assert "server-oracle.yml" in deploy._no_workers_files("oracle")


def test_no_workers_files_postgres_uses_plain_server():
    assert "server.yml" in deploy._no_workers_files("postgres")


def test_render_configmap_embeds_content_and_hash():
    out = deploy.render_configmap_yaml(
        name="cm", namespace="ns", file_key="config.yml", content="a: 1\n",
    )
    assert "kind: ConfigMap" in out
    assert "name: cm" in out and "namespace: ns" in out
    assert "tmi.dev/config-hash:" in out
    assert "    a: 1" in out  # 4-space block-scalar indent
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_deploy.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'deploy'`.

- [ ] **Step 3: Create `lib/deploy.py`**

Move the deployment logic out of `start-dev.py` into importable functions:
- Copy these helpers verbatim from `start-dev.py`: `image_builds_for` (`:43-58`), `overlay_dir_for` (`:61-62`), the registry-independent client-staging block `_resolve_client_path`/`_resolve_client_version`/`stage_tmi_client`/`unstage_tmi_client` (`:139-236`), `build_and_push` (`:243-272`), `apply_platform_base` (`:279-283`), `ensure_namespace` (`:286-290`), `deliver_config` (`:293-299`), `create_embedding_secret` (`:302-309`), `_no_workers_files` (`:312-320`), `create_oracle_wallet_secret` (`:323-342`), `apply_overlay` (`:345-368`), `wait_and_forward` (`:371-375`), `start_port_forward` (`:378-394`), `_stop_port_forward_pidfile` (`:397-406`), `stop_port_forward` (`:409-411`), the module constants (`:30-40`).
- Copy `render_configmap_yaml`, `content_hash`, `current_kube_context`, `kubectl` from `lib/devenv.py:38-81`, and `PLATFORM_NAMESPACE` from `:17`.
- Rename the three action functions and make them keyword-arg pure entry points (no argparse):

```python
def start(*, db: str, no_workers: bool = False, skip_context_guard: bool = False) -> None:
    # body of old do_start, but:
    #   - replace devenv.* with module-local names / cluster.*
    #   - replace guard_context(args.yes) with _guard_context(skip_context_guard)
    #   - replace args.db -> db, args.no_workers -> no_workers
    ...

def restart(*, db: str, no_workers: bool = False, skip_context_guard: bool = False) -> None:
    ...  # body of old do_restart

def teardown(*, db: str = "postgres") -> None:
    ...  # body of old do_stop (db unused today but kept for signature symmetry)
```

- Port `preflight` (`start-dev.py:90-95`) and `guard_context` (`:78-87`) as module-private `_preflight()` and `_guard_context(skip: bool)`; `_guard_context` calls `current_kube_context` + `cluster.is_local_kube_context`, and on the no-context error prints `Run 'make dev-cluster-up'`.
- Replace `ensure_registry`/registry constants usage with `cluster.ensure_registry()` and `cluster.REGISTRY_CONTAINER` (drop the duplicated `_REGISTRY_*` block at `start-dev.py:102-132`; use `cluster`).
- Replace `devenv.local_image_ref` with `cluster.local_image_ref`.
- Add the three NEW helpers:

```python
import subprocess
from tmi_common import run_cmd, log_info

def tail_server_logs() -> None:
    """Stream the tmi-server pod logs (Ctrl-C to stop)."""
    kubectl(["-n", NS, "logs", "-f", "deploy/tmi-server", "--tail=200"], check=False)

def remove_local_images(db: str) -> None:
    """Remove the locally-built dev images (used by `devenv.py nuke`)."""
    for name, _df, _args in image_builds_for(db):
        run_cmd(["docker", "rmi", "-f", cluster.local_image_ref(name)], check=False)

def server_http_status() -> tuple[bool, str]:
    """Return (reachable, http_code) for http://localhost:8080 via the port-forward."""
    r = subprocess.run(
        ["curl", "-s", "--connect-timeout", "2", "--max-time", "5",
         "-o", "/dev/null", "-w", "%{http_code}", "http://localhost:8080"],
        capture_output=True, text=True,
    )
    code = r.stdout.strip() or "000"
    return (code in ("200", "429"), code)
```

Header:

```python
"""TMI dev image build/push + in-cluster deploy + teardown.

Pure helpers are unit-tested in scripts/lib/tests/test_deploy.py; orchestration
functions (start/restart/teardown) are exercised against a live cluster by
scripts/devenv.py. Depends on lib/cluster.py for registry + image refs.
"""
from __future__ import annotations
import os, shutil, signal, subprocess, sys
from pathlib import Path
import cluster
from tmi_common import (
    check_tool, container_exists, container_is_running, get_project_root,
    log_error, log_info, log_success, run_cmd,
)
NS = cluster.PLATFORM_NAMESPACE if hasattr(cluster, "PLATFORM_NAMESPACE") else "tmi-platform"
```

(Define `PLATFORM_NAMESPACE` in `deploy.py` directly as `NS = "tmi-platform"`; do not depend on it living in `cluster`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_deploy.py -v`
Expected: PASS (7 passed).

- [ ] **Step 5: Commit**

```bash
git add scripts/lib/deploy.py scripts/lib/tests/test_deploy.py
git commit -m "refactor(devenv): extract lib/deploy.py for image build + k8s deploy/teardown"
```

---

## Task 3: Extract `lib/database.py` and delegate `manage-database.py`

**Files:**
- Create: `scripts/lib/database.py`
- Modify: `scripts/manage-database.py` (command bodies delegate to `lib/database.py`)
- Reference: `scripts/manage-database.py:131-230` (current command bodies), `scripts/lib/tmi_common.py` (`ensure_container`, `ensure_volume`, `wait_for_container_ready`, `stop_container`, `remove_container`, `config_get`, `load_config`).

**Interfaces:**
- Produces:
  - `DBProfile` — a small dataclass `{container: str, volume: str, port: int, config_path: str}`.
  - `dev_profile(config_path="config-development.yml") -> DBProfile` and `test_profile(config_path="config-test.yml") -> DBProfile`.
  - `up(profile: DBProfile) -> None` (start container + wait ready), `down(profile) -> None` (stop, keep volume), `destroy(profile) -> None` (remove container + volume — data wiped), `wait(profile) -> None`, `migrate(profile) -> None`, `is_running(profile) -> bool`.
- Consumes (in `devenv.py`): `database.up(database.dev_profile())`, `database.down(...)`, `database.destroy(...)`, `database.is_running(...)`.

- [ ] **Step 1: Write a failing unit test for profile construction**

Append to a new `scripts/lib/tests/test_database.py`:

```python
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import database  # noqa: E402


def test_dev_profile_container_name_is_not_test():
    p = database.dev_profile()
    assert "test" not in p.container
    assert p.config_path == "config-development.yml"


def test_test_profile_distinct_from_dev():
    assert database.test_profile().container != database.dev_profile().container
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_database.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'database'`.

- [ ] **Step 3: Create `lib/database.py`**

Move the container-lifecycle bodies out of `manage-database.py:131-230` into profile-parameterized functions. Read the current `cmd_start`/`cmd_stop`/`cmd_clean`/`cmd_wait`/`cmd_migrate`/`cmd_check` to learn the exact container name, volume name, port, image, and `docker run` args used for dev vs `--test`, and reproduce them via the `DBProfile`. Skeleton:

```python
"""PostgreSQL dev/test container lifecycle. Single source of truth shared by
scripts/devenv.py (dev) and scripts/manage-database.py (dev + --test)."""
from __future__ import annotations
from dataclasses import dataclass

from tmi_common import (
    container_is_running, ensure_container, ensure_volume, log_info, log_success,
    remove_container, stop_container, wait_for_container_ready,
)

@dataclass(frozen=True)
class DBProfile:
    container: str
    volume: str
    port: int
    config_path: str

def dev_profile(config_path: str = "config-development.yml") -> DBProfile:
    return DBProfile(container="tmi-postgresql", volume="tmi-postgresql-data",
                     port=5432, config_path=config_path)

def test_profile(config_path: str = "config-test.yml") -> DBProfile:
    return DBProfile(container="tmi-postgresql-test", volume="tmi-postgresql-test-data",
                     port=5433, config_path=config_path)

def is_running(profile: DBProfile) -> bool:
    return container_is_running(profile.container)

def up(profile: DBProfile) -> None: ...      # body from cmd_start
def down(profile: DBProfile) -> None: ...     # body from cmd_stop
def destroy(profile: DBProfile) -> None: ...  # body from cmd_clean (remove container + volume)
def wait(profile: DBProfile) -> None: ...     # body from cmd_wait
def migrate(profile: DBProfile) -> None: ...  # body from cmd_migrate
```

**IMPORTANT:** copy the *exact* container name, volume name, port, image reference, env vars, and `docker run` flags from the current `manage-database.py` bodies — do not invent values. The dev profile values above (`tmi-postgresql`, port `5432`) match `scripts/status.py:117` (`name=tmi-postgresql`); confirm the volume name and the `--test` port against `manage-database.py` before finalizing and adjust the literals if they differ.

- [ ] **Step 4: Refactor `manage-database.py` to delegate**

In `manage-database.py`, replace the container-lifecycle bodies of `cmd_start`/`cmd_stop`/`cmd_clean`/`cmd_wait`/`cmd_migrate` with calls into `lib/database.py`, building the profile from `args.test`:

```python
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import database  # noqa: E402

def _profile(args) -> "database.DBProfile":
    cfg = args.config or ("config-test.yml" if args.test else "config-development.yml")
    return database.test_profile(cfg) if args.test else database.dev_profile(cfg)

def cmd_start(cfg, args): database.up(_profile(args))
def cmd_stop(cfg, args): database.down(_profile(args))
def cmd_clean(cfg, args): database.destroy(_profile(args))
def cmd_wait(cfg, args): database.wait(_profile(args))
def cmd_migrate(cfg, args): database.migrate(_profile(args))
```

Leave `cmd_reset`, `cmd_dedup`, `cmd_check` as-is (they are dev/admin one-offs, out of scope). Keep all existing argparse arguments and the `SUBCOMMANDS` table unchanged.

- [ ] **Step 5: Run unit tests + smoke the delegation**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_database.py -v`
Expected: PASS (2 passed).

Run (verifies the refactored CLI still parses + starts the dev container):
`make start-database && make check-database && make stop-database`
Expected: container starts, check reports healthy, container stops. No traceback.

- [ ] **Step 6: Commit**

```bash
git add scripts/lib/database.py scripts/lib/tests/test_database.py scripts/manage-database.py
git commit -m "refactor(devenv): extract lib/database.py shared by manage-database and devenv"
```

---

## Task 4: Create `lib/devstatus.py` (kind-aware dashboard)

**Files:**
- Create: `scripts/lib/devstatus.py`
- Create: `scripts/lib/tests/test_devstatus.py`
- Reference (replace): `scripts/status.py` (entire file — the local-process scan is being thrown away).

**Interfaces:**
- Consumes: `cluster.CLUSTER_NAME`, `cluster.is_registry_running`, `database.dev_profile`, `database.is_running`, `deploy.server_http_status`, `deploy.NS`.
- Produces: `deployment_readiness(json_text: str) -> list[tuple[str, int, int]]` (pure parser: name, ready, desired), `print_dashboard() -> None`.

- [ ] **Step 1: Write a failing unit test for the deployment parser**

Create `scripts/lib/tests/test_devstatus.py`:

```python
import json, sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import devstatus  # noqa: E402


def test_deployment_readiness_parses_ready_and_desired():
    payload = json.dumps({"items": [
        {"metadata": {"name": "tmi-server"},
         "spec": {"replicas": 1},
         "status": {"readyReplicas": 1}},
        {"metadata": {"name": "redis"},
         "spec": {"replicas": 1},
         "status": {}},  # zero ready
    ]})
    out = dict((n, (r, d)) for n, r, d in devstatus.deployment_readiness(payload))
    assert out["tmi-server"] == (1, 1)
    assert out["redis"] == (0, 1)


def test_deployment_readiness_empty():
    assert devstatus.deployment_readiness('{"items": []}') == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_devstatus.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'devstatus'`.

- [ ] **Step 3: Create `lib/devstatus.py`**

```python
"""kind-aware status dashboard for the TMI dev environment.

Replaces the old host-process scan. Reports the kind cluster, local registry,
external db container, in-cluster Deployments, server reachability, and the
optional OAuth stub. Pure parser deployment_readiness() is unit-tested.
"""
from __future__ import annotations
import json
import sys
from pathlib import Path

import cluster
import database
import deploy
from tmi_common import GREEN, NC, RED, YELLOW, run_cmd

_WANT = ["tmi-server", "redis", "tmi-component-controller"]


def deployment_readiness(json_text: str) -> list[tuple[str, int, int]]:
    """Parse `kubectl get deploy -o json` into (name, ready, desired) rows."""
    data = json.loads(json_text or '{"items": []}')
    rows = []
    for item in data.get("items", []):
        name = item.get("metadata", {}).get("name", "?")
        desired = item.get("spec", {}).get("replicas", 0) or 0
        ready = item.get("status", {}).get("readyReplicas", 0) or 0
        rows.append((name, ready, desired))
    return rows


def _cluster_present() -> bool:
    r = run_cmd(["kind", "get", "clusters"], check=False, capture=True)
    return cluster.CLUSTER_NAME in r.stdout.split()


def _row(ok: bool, label: str, detail: str) -> None:
    mark = f"{GREEN}✓{NC}" if ok else f"{RED}✗{NC}"
    print(f"{mark} {label:<26} {detail}")


def print_dashboard() -> None:
    print("TMI Dev Environment Status")
    print("==========================\n")

    _row(_cluster_present(), f"kind cluster ({cluster.CLUSTER_NAME})",
         "present" if _cluster_present() else "absent — run 'make dev-up'")
    _row(cluster.is_registry_running(), "local registry", cluster.REGISTRY_CONTAINER)

    db = database.dev_profile()
    _row(database.is_running(db), "database (postgres)",
         f"container: {db.container}" if database.is_running(db) else "stopped")

    # In-cluster deployments
    r = run_cmd(["kubectl", "get", "deploy", "-n", deploy.NS, "-o", "json"],
                check=False, capture=True)
    if r.returncode != 0:
        print(f"{YELLOW}⦿{NC} in-cluster deployments     unreachable (no cluster/context)")
    else:
        rows = {n: (ready, desired) for n, ready, desired in deployment_readiness(r.stdout)}
        for name in _WANT:
            ready, desired = rows.get(name, (0, 0))
            present = name in rows
            _row(present and ready == desired and desired > 0,
                 f"  deploy/{name}",
                 f"{ready}/{desired} ready" if present else "not deployed")

    reachable, code = deploy.server_http_status()
    _row(reachable, "server http (:8080)", f"HTTP {code}")

    # OAuth stub (optional, informational)
    scripts_dir = Path(__file__).resolve().parents[1]
    oauth = run_cmd(["uv", "run", str(scripts_dir / "manage-oauth-stub.py"), "status"],
                    check=False, capture=True)
    print(f"\nOAuth stub: {'running' if oauth.returncode == 0 else 'not running'} "
          f"(make oauth-stub-up to start)")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_devstatus.py -v`
Expected: PASS (2 passed).

- [ ] **Step 5: Commit**

```bash
git add scripts/lib/devstatus.py scripts/lib/tests/test_devstatus.py
git commit -m "feat(devenv): add kind-aware status dashboard (lib/devstatus.py)"
```

---

## Task 5: Create the `scripts/devenv.py` orchestrator

**Files:**
- Create: `scripts/devenv.py`
- Create: `scripts/lib/tests/test_devenv_cli.py`

**Interfaces:**
- Consumes: `cluster.up/down`, `deploy.start/restart/teardown/tail_server_logs/remove_local_images`, `database.up/down/destroy/dev_profile`, `devstatus.print_dashboard`.
- Produces (CLI): `up down restart reset nuke status deploy logs cluster db`, global `--db postgres|oracle` (default postgres), `--no-workers`, `--yes` (forwarded as `skip_context_guard`). `cluster` and `db` take a positional `up|down`.

- [ ] **Step 1: Write a failing test for the arg parser / verb table**

Create `scripts/lib/tests/test_devenv_cli.py`:

```python
import importlib.util, sys
from pathlib import Path

_spec = importlib.util.spec_from_file_location(
    "devenv_cli", Path(__file__).resolve().parents[2] / "devenv.py")
devenv_cli = importlib.util.module_from_spec(_spec)
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
_spec.loader.exec_module(devenv_cli)


def test_all_verbs_registered():
    expected = {"up", "down", "restart", "reset", "nuke",
                "status", "deploy", "logs", "cluster", "db"}
    assert expected <= set(devenv_cli.VERBS)


def test_parse_up_defaults_to_postgres():
    args = devenv_cli.build_parser().parse_args(["up"])
    assert args.verb == "up" and args.db == "postgres"


def test_parse_up_oracle():
    args = devenv_cli.build_parser().parse_args(["up", "--db", "oracle"])
    assert args.db == "oracle"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_devenv_cli.py -v`
Expected: FAIL (file `scripts/devenv.py` does not exist).

- [ ] **Step 3: Create `scripts/devenv.py`**

```python
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Single orchestrator for the kind-based TMI dev environment.

Verbs (the make targets are 1:1 thin wrappers):
  up       create cluster + registry, start db, build+push images, deploy, wait
  down     tear down the in-cluster stack + cluster + registry; KEEP db data
  restart  rebuild server image + roll the server pod (cluster + db untouched)
  reset    soft known-state: redeploy the stack with fresh images; KEEP db data
  nuke     hard known-state: destroy everything incl. db data + images, rebuild
  status   kind-aware status dashboard
  deploy   (re)apply manifests + rollout without recreating cluster/db
  logs     stream the tmi-server pod logs
  cluster  up|down the kind cluster only
  db       up|down the postgres container only

Global: --db postgres|oracle (default postgres), --no-workers, --yes
"""
import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import cluster      # noqa: E402
import database     # noqa: E402
import deploy       # noqa: E402
import devstatus    # noqa: E402
from tmi_common import (  # noqa: E402
    add_verbosity_args, apply_verbosity, log_info, log_success,
)

VERBS = ["up", "down", "restart", "reset", "nuke",
         "status", "deploy", "logs", "cluster", "db"]


def _db_profile():
    return database.dev_profile()


def cmd_up(args) -> None:
    cluster.up()
    if args.db == "postgres":
        database.up(_db_profile())
    deploy.start(db=args.db, no_workers=args.no_workers, skip_context_guard=args.yes)


def cmd_down(args) -> None:
    deploy.teardown(db=args.db)
    if args.db == "postgres":
        database.down(_db_profile())  # keep volume
    cluster.down()
    log_success("dev environment down (db data preserved)")


def cmd_restart(args) -> None:
    deploy.restart(db=args.db, no_workers=args.no_workers, skip_context_guard=args.yes)


def cmd_reset(args) -> None:
    log_info("dev-reset: redeploying the in-cluster stack (keeping cluster + db data)")
    deploy.teardown(db=args.db)
    if args.db == "postgres":
        database.up(_db_profile())  # ensure db is up (no data loss)
    deploy.start(db=args.db, no_workers=args.no_workers, skip_context_guard=args.yes)
    log_success("dev-reset complete")


def cmd_nuke(args) -> None:
    log_info("dev-nuke: destroying EVERYTHING incl. db data + built images")
    deploy.teardown(db=args.db)
    cluster.down()
    if args.db == "postgres":
        database.destroy(_db_profile())   # removes container + volume (data wiped)
    deploy.remove_local_images(args.db)
    _clean_logs_and_files()
    # rebuild from scratch
    cluster.up()
    if args.db == "postgres":
        database.up(_db_profile())
    deploy.start(db=args.db, no_workers=args.no_workers, skip_context_guard=args.yes)
    log_success("dev-nuke complete (fresh environment up)")


def _clean_logs_and_files() -> None:
    scripts_dir = Path(__file__).resolve().parent
    from tmi_common import run_cmd
    run_cmd(["uv", "run", str(scripts_dir / "clean.py"), "files"], check=False)


def cmd_status(args) -> None:
    devstatus.print_dashboard()


def cmd_deploy(args) -> None:
    deploy.start(db=args.db, no_workers=args.no_workers, skip_context_guard=args.yes)


def cmd_logs(args) -> None:
    deploy.tail_server_logs()


def cmd_cluster(args) -> None:
    {"up": cluster.up, "down": cluster.down}[args.action]()


def cmd_db(args) -> None:
    if args.action == "up":
        database.up(_db_profile())
    else:
        database.down(_db_profile())


_DISPATCH = {
    "up": cmd_up, "down": cmd_down, "restart": cmd_restart, "reset": cmd_reset,
    "nuke": cmd_nuke, "status": cmd_status, "deploy": cmd_deploy, "logs": cmd_logs,
    "cluster": cmd_cluster, "db": cmd_db,
}


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="TMI dev environment orchestrator.")
    add_verbosity_args(p)
    p.add_argument("--db", choices=["postgres", "oracle"], default="postgres")
    p.add_argument("--no-workers", action="store_true")
    p.add_argument("--yes", action="store_true",
                   help="Skip the local-kube-context safety check")
    sub = p.add_subparsers(dest="verb", required=True)
    for v in VERBS:
        sp = sub.add_parser(v)
        if v in ("cluster", "db"):
            sp.add_argument("action", choices=["up", "down"])
    return p


def main() -> None:
    args = build_parser().parse_args()
    apply_verbosity(args)
    _DISPATCH[args.verb](args)


if __name__ == "__main__":
    main()
```

NOTE: argparse places global options (`--db`, `--no-workers`, `--yes`) *before* the verb (e.g. `devenv.py --db oracle up`). The Makefile wrappers (Task 7) pass them in that order. The unit test uses `["up"]` and `["up","--db","oracle"]`; if argparse rejects the trailing-option form, define `--db/--no-workers/--yes` on both the top parser and each subparser, or have the Makefile always pass globals first. Make the test reflect the chosen order.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd scripts/lib && uv run --with pytest -m pytest tests/test_devenv_cli.py -v`
Expected: PASS (3 passed). Adjust the parser/test for option order per the NOTE if needed.

- [ ] **Step 5: Smoke the orchestrator end to end (live cluster)**

Run:
```bash
uv run scripts/devenv.py up
uv run scripts/devenv.py status
uv run scripts/devenv.py down
```
Expected: `up` creates the cluster, starts postgres, builds+pushes images, deploys, and reports `Dev environment ready at http://localhost:8080`; `status` shows the cluster present, db running, `deploy/tmi-server 1/1 ready`, `server http (:8080) HTTP 200`; `down` tears the stack down and deletes the cluster with `db data preserved`. No traceback at any step.

- [ ] **Step 6: Commit**

```bash
git add scripts/devenv.py scripts/lib/tests/test_devenv_cli.py
git commit -m "feat(devenv): add devenv.py orchestrator (up/down/restart/reset/nuke/status/...)"
```

---

## Task 6: Run the full unit-test suite for the new modules

**Files:**
- Reference: `scripts/lib/tests/` (all new test files), `Makefile` target `test-dev-scripts`.

- [ ] **Step 1: Find the dev-scripts test target**

Run: `grep -n "test-dev-scripts" Makefile`
Expected: a target that runs the `scripts/lib/tests/` suite via `uv run`.

- [ ] **Step 2: Run the dev-scripts test suite**

Run: `make test-dev-scripts`
Expected: all tests pass, including `test_cluster.py`, `test_deploy.py`, `test_database.py`, `test_devstatus.py`, `test_devenv_cli.py`. If `test_devgenv`/`test_devenv.py` (old) still exists and references the deleted `devenv` helper, leave it for Task 8 (it is deleted there); if it breaks the suite now, temporarily skip it and note it — Task 8 removes it.

- [ ] **Step 3: Commit (if the target file or any test needed adjustment)**

```bash
git add -A scripts/lib/tests
git commit -m "test(devenv): wire new lib module tests into test-dev-scripts"
```

---

## Task 7: Cut the Makefile over to `dev-*` wrappers (+ deprecated aliases)

**Files:**
- Modify: `Makefile` (blocks at lines ~29, ~189-216, ~309-368, ~423, ~513, ~601, ~724-725)

**Interfaces:**
- Produces (new targets): `dev-up dev-down dev-restart dev-reset dev-nuke dev-status dev-logs dev-deploy dev-cluster-up dev-cluster-down dev-db-up dev-db-down`.
- Produces (deprecated aliases): `start-dev stop-dev restart-dev start-dev-oci status dev`.

- [ ] **Step 1: Add the new dev-lifecycle block**

Replace the `start-dev` / `start-dev-oci` / `restart-dev` / `stop-dev` / `dev-cluster-*` block (`Makefile:309-368`) with:

```make
# ============================================================================
# DEV ENVIRONMENT — single orchestrator (scripts/devenv.py). DB=postgres|oracle
# ============================================================================
.PHONY: dev-up dev-down dev-restart dev-reset dev-nuke dev-status dev-logs \
        dev-deploy dev-cluster-up dev-cluster-down dev-db-up dev-db-down

dev-up:  ## Bring up the full kind dev environment (cluster + db + deploy). DB=postgres|oracle
	@uv run scripts/devenv.py --db $(DB) up

dev-down:  ## Tear down the kind dev environment; KEEP db data
	@uv run scripts/devenv.py --db $(DB) down

dev-restart:  ## Rebuild the server image + roll the server pod (cluster + db untouched)
	@uv run scripts/devenv.py --db $(DB) restart

dev-reset:  ## Soft known-state: redeploy the stack with fresh images; KEEP db data
	@uv run scripts/devenv.py --db $(DB) reset

dev-nuke:  ## Hard known-state: destroy everything incl. db data + images, rebuild
	@uv run scripts/devenv.py --db $(DB) nuke

dev-status:  ## kind-aware dev environment status dashboard
	@uv run scripts/devenv.py status

dev-logs:  ## Stream the tmi-server pod logs
	@uv run scripts/devenv.py logs

dev-deploy:  ## (Re)apply manifests + rollout without recreating cluster/db
	@uv run scripts/devenv.py --db $(DB) deploy

dev-cluster-up:  ## Create the local kind cluster + registry only
	@uv run scripts/devenv.py cluster up

dev-cluster-down:  ## Delete the local kind cluster only
	@uv run scripts/devenv.py cluster down

dev-db-up:  ## Start the postgres dev container only
	@uv run scripts/devenv.py db up

dev-db-down:  ## Stop the postgres dev container only (keep data)
	@uv run scripts/devenv.py db down

# --- deprecated aliases (removable next release) ---
start-dev:  ## DEPRECATED alias for dev-up
	@echo "note: 'make start-dev' is renamed to 'make dev-up'"; $(MAKE) dev-up DB=$(DB)

stop-dev:  ## DEPRECATED alias for dev-down
	@echo "note: 'make stop-dev' is renamed to 'make dev-down'"; $(MAKE) dev-down DB=$(DB)

restart-dev:  ## DEPRECATED alias for dev-restart
	@echo "note: 'make restart-dev' is renamed to 'make dev-restart'"; $(MAKE) dev-restart DB=$(DB)

start-dev-oci:  ## DEPRECATED — Oracle now runs in-cluster: use 'make dev-up DB=oracle'
	@echo "note: 'make start-dev-oci' is removed; use 'make dev-up DB=oracle'"; $(MAKE) dev-up DB=oracle
```

Keep `tilt-up` / `tilt-down` as-is for now (they reference `deployments/k8s/dev/server.yml`, still valid).

- [ ] **Step 2: Delete the orphaned local-process recipes**

In `Makefile`:
- Remove `start-workers` / `stop-workers` recipes and drop them from the `.PHONY` at line 29.
- Remove the entire Process Management block `start-server` / `stop-server` / `start-service` / `stop-service` / `stop-process` / `wait-process` (`Makefile:189-216`) and its `.PHONY`.

- [ ] **Step 3: Rewire `stop-all`, `dev`, and `status`**

- Replace `stop-all` (`:423`) with:

```make
stop-all: stop-oauth-stub dev-down  ## Stop the OAuth stub and tear down the kind dev environment
```

- Replace `dev: start-dev` (`:601`) with `dev: dev-up`.
- Replace the `status` recipe (`:724-725`) body `@uv run scripts/status.py` with `@uv run scripts/devenv.py status`, and add a deprecated note line, OR repoint `status` as a thin alias to `dev-status`:

```make
status:  ## DEPRECATED alias for dev-status
	@uv run scripts/devenv.py status
```

- Update the top-level `.PHONY` list at `Makefile:244` to drop `start-dev-oci` only if you also drop the alias; since the alias is kept, leave `start-dev start-dev-oci restart-dev stop-dev` in `.PHONY` and add `dev-up dev-down dev-restart dev-reset dev-nuke dev-status dev-logs dev-deploy dev-db-up dev-db-down`.

- [ ] **Step 4: Verify the wrappers resolve and lint passes**

Run:
```bash
make -n dev-up
make -n dev-up DB=oracle
make -n dev-down
make -n dev-status
make -n start-dev   # deprecated alias forwards to dev-up
make lint
```
Expected: `make -n dev-up` prints `uv run scripts/devenv.py --db postgres up`; `DB=oracle` prints `--db oracle up`; the alias prints the deprecation note then the `dev-up` recipe; `make lint` passes.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "refactor(make): rationalize dev targets behind devenv.py (dev-* verbs + aliases)"
```

---

## Task 8: Rework `clean.py` and delete the orphaned scripts

**Files:**
- Modify: `scripts/clean.py`
- Delete: `scripts/dev-cluster.py`, `scripts/start-dev.py`, `scripts/status.py`, `scripts/manage-server.py`, `scripts/manage-workers.py`, `scripts/lib/devenv.py`, `scripts/lib/_server_state.py`, `scripts/lib/tests/test_devenv.py`

**Interfaces:**
- `clean.py` keeps subcommands `logs files build containers all`; drops `process` (it only stopped the now-deleted local server/workers) — or keeps `process` reduced to the OAuth stub + wstest only.

- [ ] **Step 1: Edit `clean.py` — remove local-server/worker coupling**

- Delete the import `from _server_state import running_server_pid` (`clean.py:30`).
- In `clean_logs` (`:37-73`), remove the running-server guard (`:47-52`) — there is no host server anymore. Keep the rest (it just deletes log/PID files).
- Replace `clean_process` (`:111-127`) body to stop only the OAuth stub + wstest (drop the `manage-server.py` and `manage-workers.py` calls):

```python
def clean_process() -> None:
    """Stop the OAuth stub and any wstest processes (no host server post-kind)."""
    scripts_dir = get_project_root() / "scripts"
    run_cmd(["uv", "run", str(scripts_dir / "manage-oauth-stub.py"), "stop"], check=False)
    run_cmd(["pkill", "-f", "wstest"], check=False)
```

- In `clean_all` (`:130-153`), keep the test-container cleanup and `clean_files`; `clean_process` now only stops the stub/wstest. Leave the `manage-nats.py` / `manage-redis.py` / `manage-database.py --test` calls (still valid for test infra).

- [ ] **Step 2: Run the script tests to confirm nothing imports the deleted helper**

Run: `make test-dev-scripts`
Expected: PASS. (The old `test_devenv.py` is deleted in Step 3; if still present and failing, proceed to Step 3.)

- [ ] **Step 3: Delete the orphaned scripts**

```bash
git rm scripts/dev-cluster.py scripts/start-dev.py scripts/status.py \
       scripts/manage-server.py scripts/manage-workers.py \
       scripts/lib/devenv.py scripts/lib/_server_state.py \
       scripts/lib/tests/test_devenv.py
```

- [ ] **Step 4: Confirm no dangling references remain**

Run:
```bash
grep -rEn "manage-server|manage-workers|dev-cluster\.py|start-dev\.py|status\.py|_server_state|import devenv\b" \
  scripts/ Makefile scripts/test-framework.mk Tiltfile
```
Expected: NO matches (every hit must be fixed before continuing). If any pure-function tests from `test_devenv.py` (e.g. `is_local_kube_context`, `render_configmap_yaml`) are not yet covered, confirm they now live in `test_cluster.py` / `test_deploy.py` (they do, from Tasks 1–2).

- [ ] **Step 5: Run lint + script tests**

Run: `make lint && make test-dev-scripts`
Expected: both pass.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(devenv): drop local-process scripts; clean.py no longer manages a host server"
```

---

## Task 9: Oracle in-cluster gate + delete `start-dev-oci.sh`

**Files:**
- Delete: `scripts/start-dev-oci.sh`
- Reference: `deployments/k8s/dev/oracle/`, `deployments/k8s/dev/server-oracle.yml`, `scripts/oci-env.sh`.

**This task is GATED:** do not delete `start-dev-oci.sh` until `dev-up DB=oracle` is proven to work in-cluster.

- [ ] **Step 1: Prepare the Oracle prerequisites**

Confirm the developer has: an ADB wallet `.zip` and `TMI_ORACLE_WALLET_ZIP` pointing at it; the Oracle overlay present (`ls deployments/k8s/dev/oracle/ deployments/k8s/dev/server-oracle.yml`).

- [ ] **Step 2: Bring up the Oracle dev environment in-cluster**

Run:
```bash
export TMI_ORACLE_WALLET_ZIP=/path/to/wallet.zip
make dev-up DB=oracle
```
Expected: `devenv.py --db oracle up` builds `tmi-server-oracle`, creates `Secret/tmi-oracle-wallet`, applies the oracle overlay, and rolls out `deploy/tmi-server`. No traceback.

- [ ] **Step 3: Verify the server reached the database**

Run:
```bash
make dev-status
curl -s http://localhost:8080/ | head
kubectl -n tmi-platform logs deploy/tmi-server --tail=50
```
Expected: `dev-status` shows `deploy/tmi-server 1/1 ready` and `server http (:8080) HTTP 200`; the root endpoint returns version JSON; the server logs show a successful Oracle connection + migration with no `ORA-`/connection errors.

- [ ] **Step 4: Tear down**

Run: `make dev-down DB=oracle`
Expected: clean teardown.

- [ ] **Step 5: Delete `start-dev-oci.sh` once the gate passes**

```bash
git rm scripts/start-dev-oci.sh
```

Then in `Makefile`, update the comment block above the `start-dev-oci` alias (`:313-318`) to say Oracle dev runs in-cluster via `make dev-up DB=oracle`. (The deprecated `start-dev-oci` alias from Task 7 already forwards there.)

- [ ] **Step 6: Confirm no references to the deleted script**

Run: `grep -rEn "start-dev-oci\.sh" scripts/ Makefile Tiltfile`
Expected: NO matches.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(devenv): Oracle dev runs in-cluster (dev-up DB=oracle); drop start-dev-oci.sh"
```

> **If the Oracle gate FAILS** (server cannot reach ADB in-cluster): do NOT delete `start-dev-oci.sh`. Stop, file a GitHub issue describing the in-cluster Oracle deployment gap (wallet secret, overlay, or networking), and report back — the local Oracle path stays until the in-cluster path works.

---

## Task 10: Update references, final verification, DB review, docs

**Files:**
- Modify: `scripts/test-framework.mk` (comments), `Tiltfile` (comments)
- Reference: `Makefile:513` (`@$(MAKE) start-database`), `scripts/run-api-tests.py` (`--start-server` flag)

- [ ] **Step 1: Update comment references to renamed targets**

- `scripts/test-framework.mk:108` and `:153`: change `make start-dev` → `make dev-up`.
- `Tiltfile:3,8,10,17`: change `make start-dev` → `make dev-up`, `make restart-dev` → `make dev-restart`, `make start-dev DB=oracle` → `make dev-up DB=oracle` (leave `make tilt-down` as-is).

- [ ] **Step 2: Check the two remaining `start-*` references are unrelated**

Run: `grep -n "start-database\|start-server" Makefile scripts/run-api-tests.py`
Confirm: `Makefile:513` `@$(MAKE) start-database` refers to the still-valid `start-database` target (postgres container) — leave it. `run-api-tests.py`'s `--start-server` is the API-test harness's own flag, NOT `manage-server.py` — confirm by reading the flag's handler; if it shells to the deleted `manage-server.py`, repoint it to `devenv.py deploy` or document that the API test harness now expects `make dev-up` first. Record the finding.

- [ ] **Step 3: Run the full local quality gates**

Run:
```bash
make lint
make test-dev-scripts
make help | grep -E "dev-(up|down|restart|reset|nuke|status)"
```
Expected: lint clean; all script tests pass; `make help` lists the new `dev-*` targets with their `##` descriptions.

- [ ] **Step 4: Full round-trip smoke (postgres)**

Run:
```bash
make dev-up
make dev-status
make dev-restart
make dev-reset
make dev-down
```
Expected: each completes without traceback; `dev-status` after `dev-up` is all-green; `dev-down` reports db data preserved.

- [ ] **Step 5: Oracle DB-compatibility review**

`lib/database.py` reorganizes db-container/migration management. Per CLAUDE.md, invoke the `oracle-db-admin` skill and dispatch the subagent on the diff (`lib/database.py`, `manage-database.py`). Expected verdict: APPROVED (logic is moved, not changed; container lifecycle is postgres-only and Oracle stays external). Address any BLOCKING finding before finishing; fold APPROVED-WITH-NOTES items in or file follow-ups.

- [ ] **Step 6: Update the wiki / onramp doc references**

Per CLAUDE.md, dev-environment docs live in the GitHub Wiki, not `docs/`. List the wiki pages that reference the old targets (`start-dev`, `start-server`, `start-dev-oci`, `make status`) — e.g. the dev-onramp page from issue #447 — and update them to the new `dev-*` commands. (Do not edit `docs/` except `docs/superpowers/*`.) If wiki edits are out of band for this session, file a follow-up issue listing the pages to update.

- [ ] **Step 7: Final commit + landing**

```bash
git add -A
git commit -m "docs(devenv): update test-framework + Tiltfile references to dev-* targets"
```

Then follow the CLAUDE.md "Landing the Plane" workflow: ensure `make lint` + `make test-dev-scripts` are green, run the `security-regression` skill if any security-adjacent path was touched (none expected here), commit locally, and **postpone the push** until the user explicitly approves it (per the user's global git-push rule). Report the branch state and any filed follow-up issues.

---

## Self-Review (completed by plan author)

**Spec coverage:**
- §1 command surface → Tasks 5 (orchestrator verbs) + 7 (make wrappers). ✔
- §2 script architecture (`devenv.py` + `lib/cluster|deploy|database`) → Tasks 1–5. ✔
- §3 Oracle in-cluster + capability-preserving gate → Task 9. ✔
- §4 kind-aware `dev-status` → Task 4. ✔
- §5 naming/cleanup/back-compat aliases → Task 7; `clean.py` rework → Task 8. ✔
- "Keep test infra + manage-redis/nats/database" → Task 3 (database refactor keeps `--test`); manage-redis/nats untouched. ✔
- Delete list (manage-server/workers, dev-cluster, start-dev, start-dev-oci, status, lib/devenv, _server_state) → Tasks 8–9. ✔
- Risks: hidden consumers (Task 8 Step 4 + Task 10 Step 2 greps), shared-db divergence (Task 3 single source), name collision (lib/devenv split into cluster+deploy, Tasks 1–2). ✔

**Placeholder scan:** Bulk code moves cite exact source `file:line` ranges instead of re-pasting 500 lines (a refactor, not invention); all genuinely-new code (devenv.py, devstatus.py, Makefile block, clean.py edits, every test) is shown in full. No "TBD"/"add error handling"/"similar to Task N".

**Type consistency:** `deploy.start/restart/teardown` keyword signatures match their callers in `devenv.py`; `database.up/down/destroy/dev_profile/is_running` match `devenv.py` and `devstatus.py` usage; `cluster.is_registry_running` / `deploy.server_http_status` / `deploy.NS` match `devstatus.py`; `VERBS` list matches the test and dispatch table.
