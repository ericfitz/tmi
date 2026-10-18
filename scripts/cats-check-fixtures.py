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
each fixture and prints the casualties, so a run whose nested coverage was
quietly truncated says so instead of looking clean. The fixture list comes from
the manifest dbtool writes beside the reference file (cats-fixtures.json,
#608/#1115), which names every seeded fixture with its verify URL; the built-in
ANCHORS table is only a fallback for runs seeded before the manifest existed.
It never fails the run: the campaign is already over by the time it executes,
and the run-validity gates own the pass/fail decision.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from collections.abc import Callable
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


class ManifestError(Exception):
    """The fixture manifest exists but cannot be used."""


def load_manifest(path: Path) -> list[tuple[str, str]]:
    """Return (key, verify_url) for every fixture in the manifest.

    Raises ManifestError on anything malformed: silently falling back to the
    built-in table would hide exactly the fixtures this report exists to show.
    """
    try:
        doc = json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        raise ManifestError(f"cannot read {path}: {exc}") from exc
    fixtures = doc.get("fixtures") if isinstance(doc, dict) else None
    if not isinstance(fixtures, list):
        raise ManifestError(f"{path}: missing 'fixtures' list")
    targets = []
    for i, f in enumerate(fixtures):
        key = f.get("key") if isinstance(f, dict) else None
        url = f.get("verify_url") if isinstance(f, dict) else None
        if not isinstance(key, str) or not isinstance(url, str) or not key or not url.startswith("/"):
            raise ManifestError(f"{path}: fixtures[{i}] needs string 'key' and a server-relative 'verify_url'")
        targets.append((key, url))
    return targets


def anchor_targets(globals_: dict) -> list[tuple[str, str]]:
    """Fallback: (key, path) for the ANCHORS the reference file names."""
    targets = []
    for key, template in ANCHORS.items():
        rid = globals_.get(key)
        if not rid or str(rid).startswith("00000000-"):
            continue
        targets.append((key, template.format(id=rid)))
    return targets


def classify(
    server: str, token: str, targets: list[tuple[str, str]],
    probe_fn: Callable[[str, str, str], int] | None = None,
) -> tuple[list[tuple[str, str, int]], list[tuple[str, str, int]]]:
    """Split targets into (casualties, survivors) by probing each one."""
    probe_fn = probe_fn or probe
    casualties, survivors = [], []
    for key, path in targets:
        status = probe_fn(server, path, token)
        # 404/410 means gone. 401/403 means we cannot tell, which is not a
        # casualty - reporting it as one would cry wolf on every permissions
        # quirk. 0 means the probe itself failed.
        (casualties if status in (404, 410) else survivors).append((key, path, status))
    return casualties, survivors


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
    parser.add_argument("--fixtures", help="path to cats-fixtures.json (the fixture manifest)")
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

    results_dir = Path(os.environ.get("CATS_RESULTS_DIR", "test/results/cats"))
    manifest = Path(args.fixtures or results_dir / "cats-fixtures.json")
    targets: list[tuple[str, str]]
    if manifest.is_file():
        try:
            targets = load_manifest(manifest)
        except ManifestError as exc:
            print(f"cats-check-fixtures: {exc}", file=sys.stderr)
            return 0
    else:
        ref_data = Path(args.ref_data or results_dir / "cats-test-data.yml")
        if not ref_data.is_file():
            print(f"cats-check-fixtures: neither {manifest} nor {ref_data} found; skipping",
                  file=sys.stderr)
            return 0
        try:
            data = yaml.safe_load(ref_data.read_text()) or {}
        except (OSError, yaml.YAMLError) as exc:
            print(f"cats-check-fixtures: cannot read {ref_data}: {exc}", file=sys.stderr)
            return 0
        targets = anchor_targets(data.get("all") or {})

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

    casualties, survivors = classify(server, token, targets)

    print()
    print("Seeded fixture survival (#608):")
    for key, path, status in survivors:
        print(f"  OK       {key:20s} {status} {path}")
    for key, path, status in casualties:
        print(f"  DESTROYED {key:19s} {status} {path}")

    if casualties:
        print()
        print(f"{len(casualties)} seeded fixture(s) did not survive the campaign. Every path "
              "nested under them was fuzzed against a 404 from the point of deletion "
              "onward, so per-path conclusions for those families are unreliable. "
              "Re-seed before analysing them. See issue #608.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
