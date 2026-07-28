# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Report which seeded CATS fixtures a campaign destroyed.

A campaign deletes resources it was pointed at, and because CATS walks paths in
lexical order everything nested under a destroyed anchor is fuzzed against a 404
for the rest of the run (#608). Seeded decoys absorb the *direct* case, but not
every route: `InvalidReferencesFields` injects a `?` into a path parameter, so
`DELETE /threat_models/{real}?/assets/{id}` collapses onto the parent's route
and deletes the real fixture. The server is behaving correctly there — that is
what `?` means in a URL — so this cannot be closed server-side.

What it can be is *visible*. Run as the plugin's `post_run` hook, this probes
each anchor the reference file names and prints the casualties, so a run whose
nested coverage was quietly truncated says so instead of looking clean. It
never fails the run: the campaign is already over by the time it executes, and
the run-validity gates own the pass/fail decision.
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

import yaml

# refData key -> URL template for the resource that key identifies. Only
# anchors worth reporting: each one has paths nested beneath it, so losing it
# costs more than its own tests.
ANCHORS: dict[str, str] = {
    "threat_model_id": "/threat_models/{id}",
    "team_id": "/teams/{id}",
    "project_id": "/projects/{id}",
    "group_id": "/admin/groups/{id}",
    "user_id": "/admin/users/{id}",
    "survey_response_id": "/intake/survey_responses/{id}",
}


def probe(server: str, path: str, token: str) -> int:
    """Return the HTTP status for a GET, or 0 if the request never completed."""
    req = urllib.request.Request(server.rstrip("/") + path, method="GET")
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status
    except urllib.error.HTTPError as exc:
        return exc.code
    except (urllib.error.URLError, OSError):
        return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref-data", help="path to cats-test-data.yml")
    parser.add_argument("--server", help="base URL (defaults to $CATS_SERVER)")
    parser.add_argument(
        "--token-cmd",
        default='uv run scripts/cats-token.py --user charlie --server "$CATS_SERVER"',
        help="command printing a bearer token on stdout; $CATS_SERVER is exported for it")
    args = parser.parse_args()

    server = args.server or os.environ.get("CATS_SERVER")
    if not server:
        print("cats-check-fixtures: no --server and no CATS_SERVER; skipping", file=sys.stderr)
        return 0

    ref_data = Path(args.ref_data or
                    Path(os.environ.get("CATS_RESULTS_DIR", "test/results/cats")) / "cats-test-data.yml")
    if not ref_data.is_file():
        print(f"cats-check-fixtures: {ref_data} not found; skipping", file=sys.stderr)
        return 0

    try:
        data = yaml.safe_load(ref_data.read_text()) or {}
    except (OSError, yaml.YAMLError) as exc:
        print(f"cats-check-fixtures: cannot read {ref_data}: {exc}", file=sys.stderr)
        return 0

    # shell=True is for the OPERATOR's own token command (same trust model as
    # the cats plugin's hooks — it needs pipes, quoting and env expansion), but
    # nothing is interpolated into it. The server goes in via the environment,
    # which is also how the plugin passes it to every other hook, so a URL
    # containing shell metacharacters cannot reach a command line.
    proc = subprocess.run(
        args.token_cmd, shell=True, capture_output=True, text=True, check=False,
        env={**os.environ, "CATS_SERVER": server},
    )
    token = (proc.stdout or "").strip()
    if proc.returncode != 0 or not token:
        print("cats-check-fixtures: could not mint a token; skipping", file=sys.stderr)
        return 0

    globals_ = data.get("all") or {}
    casualties, survivors = [], []
    for key, template in ANCHORS.items():
        rid = globals_.get(key)
        if not rid or str(rid).startswith("00000000-"):
            continue
        path = template.format(id=rid)
        status = probe(server, path, token)
        # 404/410 means gone. 401/403 means we cannot tell, which is not a
        # casualty — reporting it as one would cry wolf on every permissions
        # quirk. 0 means the probe itself failed.
        (casualties if status in (404, 410) else survivors).append((key, path, status))

    print()
    print("Seeded fixture survival (#608):")
    for key, path, status in survivors:
        print(f"  OK       {key:20s} {status} {path}")
    for key, path, status in casualties:
        print(f"  DESTROYED {key:19s} {status} {path}")

    if casualties:
        print()
        print(f"{len(casualties)} seeded anchor(s) did not survive the campaign. Every path "
              "nested under them was fuzzed against a 404 from the point of deletion "
              "onward, so per-path conclusions for those families are unreliable. "
              "Re-seed before analysing them. See issue #608.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
