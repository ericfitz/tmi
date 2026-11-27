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

Manifest probes cannot see one more casualty: decoys. A decoy is the id refData
binds to a path-exact section (e.g. `/threat_models/{threat_model_id}`) so a
fuzzed DELETE there consumes it instead of the anchor. refData is per path, not
per method, so every later GET/PUT/PATCH on that path uses the same decoy, and
once a fuzzer deletes it they all get 404 (run 20261122: 70 decoy DELETEs, ~810
404s, while every manifest fixture survived, #1235). Decoys are sacrificial, so
they are not in the manifest; instead, when the plugin passes the results
database ($CATS_DB), this hook reads it and reports each resource a fuzzer
deleted while later tests still targeted it.
"""

from __future__ import annotations

import argparse
import json
import os
import sqlite3
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


# CheckDeletedResourcesNotAvailable GETs a resource it just deleted and expects
# the 404, so its 404s are the intended outcome, not lost coverage.
_EXPECTS_404_AFTER_DELETE = frozenset({"CheckDeletedResourcesNotAvailable"})

_CONSUMED_SQL = """
SELECT t.test_number, f.name, m.method, rq.url, rs.response_code
FROM tests t
JOIN requests rq ON rq.test_id = t.id
JOIN responses rs ON rs.test_id = t.id
JOIN http_methods m ON m.id = rq.http_method_id
JOIN fuzzers f ON f.id = t.fuzzer_id
ORDER BY t.test_number
"""


def _resource_path(url: str) -> str:
    """Server-relative path of a request URL, without query or fragment."""
    rest = url.split("://", 1)[-1]
    path = "/" + rest.split("/", 1)[1] if "/" in rest else "/"
    return path.split("?", 1)[0].split("#", 1)[0]


def consumed_mid_run(rows) -> list[tuple[str, int, str, int]]:
    """Resources deleted by a fuzzer while later tests still targeted them.

    `rows` are (test_number, fuzzer, method, url, response_code) in test
    order. Returns (path, test_number, fuzzer, later_404s) for each resource
    whose first successful DELETE was followed by at least one 404 on that
    resource or a path nested under it, most-damaging first.
    """
    deleted: dict[str, tuple[int, str]] = {}
    lost: dict[str, int] = {}
    for number, fuzzer, method, url, code in rows:
        path = _resource_path(url)
        if code == 404 and fuzzer not in _EXPECTS_404_AFTER_DELETE:
            for gone in deleted:
                if path == gone or path.startswith(gone + "/"):
                    lost[gone] = lost.get(gone, 0) + 1
        if method == "DELETE" and 200 <= code < 300 and path not in deleted:
            deleted[path] = (number, fuzzer)
    found = [(p, *deleted[p], n) for p, n in lost.items()]
    return sorted(found, key=lambda r: (-r[3], r[1]))


def report_consumed(db_path: Path) -> list[tuple[str, int, str, int]]:
    """Print consumed_mid_run() for the results database; [] if unreadable."""
    try:
        con = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
        try:
            rows = con.execute(_CONSUMED_SQL).fetchall()
        finally:
            con.close()
    except sqlite3.Error as exc:
        print(f"cats-check-fixtures: cannot read {db_path}: {exc}", file=sys.stderr)
        return []
    found = consumed_mid_run(rows)
    if found:
        print()
        print("Resources deleted mid-run while later tests still used them (#1235):")
        for path, number, fuzzer, n in found:
            print(f"  {n:5d} later 404s  deleted at test {number} by {fuzzer}: {path}")
        print(f"{sum(r[3] for r in found)} later test(s) ran against a resource the campaign "
              "had already deleted (usually a decoy shared by every method on its path). "
              "Withhold the deleting fuzzer from that DELETE (skip_fuzzers_for_extension) "
              "or treat those 404s as lost coverage.")
    return found


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref-data", help="path to cats-test-data.yml")
    parser.add_argument("--fixtures", help="path to cats-fixtures.json (the fixture manifest)")
    parser.add_argument("--server", help="base URL (defaults to $CATS_SERVER)")
    parser.add_argument(
        "--token-cmd",
        default='uv run scripts/cats-token.py --user charlie --server "$CATS_SERVER"',
        help="command printing a bearer token on stdout; $CATS_SERVER is exported for it")
    parser.add_argument("--db", help="CATS results database (defaults to $CATS_DB)")
    args = parser.parse_args()

    db = args.db or os.environ.get("CATS_DB")
    if db:
        report_consumed(Path(db))

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
    # the test-api-fuzzing plugin's hooks — it needs pipes, quoting and env expansion), but
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
