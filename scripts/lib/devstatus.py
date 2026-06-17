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
