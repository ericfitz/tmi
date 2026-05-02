#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Block CI on open Dependabot alerts in security-sensitive paths.

This is the T4 mitigation: not every Dependabot alert blocks the build, only
those that touch the auth, SSRF, webhook, content-extractor, or signature/JWT
paths. Everything else stays as an alert (visible in the GitHub UI) without
gating the merge queue, so the alert-fatigue problem the maintainer reported
does not return.

The list of security-sensitive Go modules and source paths lives in
scripts/security-deps-gate.config.yml so a future operator can revise the
gate without changing this script.

Behavior:
- Calls the GitHub Dependabot Alerts API for the repo this script is run in.
- Keeps only alerts whose state is "open" AND severity is "critical" or "high".
- Drops alerts whose package name is not in sensitive_modules AND whose
  manifest_path does not start with any of sensitive_paths.
- Prints a one-line summary per blocking alert, then exits 1.
- Exits 0 if no blocking alert remains.

Usage:
    GITHUB_REPOSITORY=owner/repo GH_TOKEN=... uv run scripts/security-deps-gate.py
    # Or simply: uv run scripts/security-deps-gate.py  (auto-detects via gh CLI)

Required env (CI):
    GITHUB_REPOSITORY = owner/repo
    GH_TOKEN          = a token with security_events:read on the repo

Local use:
    Requires the gh CLI to be authenticated. Auto-detects the repo from
    `git remote get-url origin`.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from pathlib import Path

CONFIG_PATH = Path(__file__).resolve().parent / "security-deps-gate.config.yml"


def parse_config(text: str) -> dict[str, list[str]]:
    """Tiny YAML-subset parser: keys map to lists of "- " entries.

    The config file is intentionally constrained to two top-level keys
    each holding a flat list of strings; this avoids a runtime PyYAML
    dependency for what is otherwise a 30-line config.
    """
    out: dict[str, list[str]] = {}
    current_key: str | None = None
    for raw in text.splitlines():
        line = raw.split("#", 1)[0].rstrip()
        if not line.strip():
            continue
        if not raw.startswith(" ") and line.endswith(":"):
            current_key = line[:-1].strip()
            out[current_key] = []
            continue
        if current_key is not None and line.lstrip().startswith("- "):
            value = line.lstrip()[2:].strip().strip('"').strip("'")
            out[current_key].append(value)
    return out


def load_config() -> tuple[list[str], list[str]]:
    if not CONFIG_PATH.exists():
        print(f"missing config file: {CONFIG_PATH}", file=sys.stderr)
        sys.exit(2)
    cfg = parse_config(CONFIG_PATH.read_text())
    return cfg.get("sensitive_modules", []), cfg.get("sensitive_paths", [])


def detect_repo() -> str:
    repo = os.environ.get("GITHUB_REPOSITORY")
    if repo:
        return repo
    try:
        url = subprocess.check_output(
            ["git", "remote", "get-url", "origin"], text=True
        ).strip()
    except subprocess.CalledProcessError as e:
        print(f"cannot detect repo from git remote: {e}", file=sys.stderr)
        sys.exit(2)
    m = re.search(r"github\.com[:/]([^/]+/[^/]+?)(?:\.git)?$", url)
    if not m:
        print(f"unrecognized origin URL: {url}", file=sys.stderr)
        sys.exit(2)
    return m.group(1)


def fetch_alerts(repo: str) -> list[dict]:
    """Pull all open Dependabot alerts via the gh CLI."""
    cmd = [
        "gh",
        "api",
        "--paginate",
        f"/repos/{repo}/dependabot/alerts?state=open&per_page=100",
    ]
    try:
        out = subprocess.check_output(cmd, text=True)
    except FileNotFoundError:
        print("gh CLI is required to read Dependabot alerts", file=sys.stderr)
        sys.exit(2)
    except subprocess.CalledProcessError as e:
        print(f"gh api failed: {e}", file=sys.stderr)
        sys.exit(2)

    # gh api --paginate emits one JSON array per page on stdout.
    # Concatenate them into a single list so callers do not have to care.
    alerts: list[dict] = []
    decoder = json.JSONDecoder()
    idx = 0
    while idx < len(out):
        if out[idx].isspace():
            idx += 1
            continue
        page, length = decoder.raw_decode(out, idx)
        if isinstance(page, list):
            alerts.extend(page)
        idx += length
    return alerts


def alert_severity(alert: dict) -> str:
    sa = alert.get("security_advisory") or {}
    return (sa.get("severity") or "").lower()


def alert_package(alert: dict) -> str:
    return (
        ((alert.get("dependency") or {}).get("package") or {}).get("name") or ""
    )


def alert_manifest(alert: dict) -> str:
    return (alert.get("dependency") or {}).get("manifest_path") or ""


def is_blocking(alert: dict, modules: list[str], paths: list[str]) -> bool:
    if alert.get("state") != "open":
        return False
    if alert_severity(alert) not in {"critical", "high"}:
        return False
    pkg = alert_package(alert)
    if pkg in modules:
        return True
    manifest = alert_manifest(alert)
    return any(manifest.startswith(p) for p in paths)


def summarise(alert: dict) -> str:
    sa = alert.get("security_advisory") or {}
    ghsa = sa.get("ghsa_id") or "GHSA-?"
    severity = alert_severity(alert).upper() or "?"
    pkg = alert_package(alert) or "?"
    manifest = alert_manifest(alert) or "?"
    summary = (sa.get("summary") or "").strip()
    return f"[{severity}] {ghsa} {pkg} (manifest={manifest}): {summary}"


def main() -> int:
    modules, paths = load_config()
    repo = detect_repo()
    alerts = fetch_alerts(repo)
    blocking = [a for a in alerts if is_blocking(a, modules, paths)]

    if not blocking:
        print(
            f"OK: 0 blocking Dependabot alerts in security-sensitive paths "
            f"({len(alerts)} open alert(s) total in {repo})."
        )
        return 0

    print(
        f"FAIL: {len(blocking)} Dependabot alert(s) block this build "
        f"({len(alerts)} open alert(s) total in {repo})."
    )
    print("Sensitive modules: " + ", ".join(modules))
    print("Sensitive paths:   " + ", ".join(paths))
    print("Blocking alerts:")
    for a in blocking:
        print(f"  - {summarise(a)}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
