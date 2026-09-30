#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.11"
# ///
"""Force one tmi-rotator rotation now (#965).

`kubectl create job --from=cronjob/...` cannot set env, so this renders the
CronJob's job template with --dry-run, injects ROTATE=<name>, applies it and
follows the logs. Usage: make rotate-secret name=redis-password
"""
import json
import subprocess
import sys
import time

NS = "tmi-platform"
VALID = {"redis-password", "settings-key"}


def main() -> int:
    if len(sys.argv) != 2 or sys.argv[1] not in VALID:
        print(f"usage: rotate-secret.py <{'|'.join(sorted(VALID))}>", file=sys.stderr)
        return 2
    name = sys.argv[1]
    job_name = f"tmi-rotator-{name}-{int(time.time())}"
    raw = subprocess.run(["kubectl", "-n", NS, "create", "job", job_name, "--from=cronjob/tmi-rotator",
                          "--dry-run=client", "-o", "json"], check=True, capture_output=True, text=True).stdout
    job = json.loads(raw)
    container = job["spec"]["template"]["spec"]["containers"][0]
    container.setdefault("env", []).append({"name": "ROTATE", "value": name})
    subprocess.run(["kubectl", "-n", NS, "apply", "-f", "-"], input=json.dumps(job), check=True, text=True)
    subprocess.run(["kubectl", "-n", NS, "wait", "--for=condition=complete", f"job/{job_name}", "--timeout=40m"], check=False)
    subprocess.run(["kubectl", "-n", NS, "logs", f"job/{job_name}"], check=False)
    return 0


if __name__ == "__main__":
    sys.exit(main())
