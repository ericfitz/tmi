#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Force one tmi-rotator rotation now (#965).

`kubectl create job --from=cronjob/...` cannot set env, so this renders the
CronJob's job template with --dry-run, injects ROTATE=<name>, applies it and
waits for the Job to finish. Every kubectl call is pinned with --context (#955):
resolved from --cluster for docker-desktop/k3s, explicit --context for aws.
Usage: make rotate-secret name=redis-password CLUSTER=docker-desktop
"""
import argparse
import json
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import cluster  # noqa: E402

NS = "tmi-platform"
VALID = ("redis-password", "settings-key")


# SEM@3b682947: resolve the kube context for a cluster target, never the ambient one (pure)
def resolve_context(cluster_target: str, context: str | None) -> str:
    """Kube context for this run; never the ambient one (pure). aws needs an explicit one."""
    if cluster_target == "aws":
        if not context:
            raise ValueError("CLUSTER=aws requires an explicit --context (CONTEXT=...)")
        return context
    return cluster.expected_context(cluster_target)


# SEM@3b682947: run kubectl against a given context and namespace
def _kubectl(ctx: str, args: list[str], **kw) -> subprocess.CompletedProcess:
    return subprocess.run(["kubectl", "--context", ctx, "-n", NS, *args], text=True, **kw)


# SEM@3b682947: list names of secret rotator jobs that are still running (pure)
def active_rotator_jobs(jobs: dict) -> list[str]:
    """Names of tmi-rotator Jobs that are still running (pure)."""
    return [j["metadata"]["name"] for j in jobs.get("items", [])
            if j["metadata"]["name"].startswith("tmi-rotator-") and j.get("status", {}).get("active", 0) > 0]


# SEM@3b682947: handle the secret rotation command-line entry point
def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--cluster", required=True, choices=["docker-desktop", "k3s", "aws"])
    ap.add_argument("--context")
    ap.add_argument("name", choices=VALID)
    a = ap.parse_args()
    try:
        ctx = resolve_context(a.cluster, a.context)
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        return 2
    jobs = json.loads(_kubectl(ctx, ["get", "jobs", "-o", "json"], check=True, capture_output=True).stdout)
    busy = active_rotator_jobs(jobs)
    if busy:
        print(f"error: a tmi-rotator Job is already running: {', '.join(busy)}", file=sys.stderr)
        return 3
    job_name = f"tmi-rotator-{a.name}-{int(time.time())}"
    raw = _kubectl(ctx, ["create", "job", job_name, "--from=cronjob/tmi-rotator", "--dry-run=client", "-o", "json"],
                   check=True, capture_output=True).stdout
    job = json.loads(raw)
    container = job["spec"]["template"]["spec"]["containers"][0]
    container.setdefault("env", []).append({"name": "ROTATE", "value": a.name})
    _kubectl(ctx, ["apply", "-f", "-"], input=json.dumps(job), check=True)
    deadline = time.time() + 40 * 60
    ok = False
    while time.time() < deadline:
        st = json.loads(_kubectl(ctx, ["get", f"job/{job_name}", "-o", "json"], check=True,
                                 capture_output=True).stdout).get("status", {})
        if st.get("succeeded", 0) > 0:
            ok = True
            break
        if st.get("failed", 0) > 0:
            break
        time.sleep(5)
    _kubectl(ctx, ["logs", f"job/{job_name}"], check=False)
    if not ok:
        print(f"error: Job {job_name} did not complete successfully", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
