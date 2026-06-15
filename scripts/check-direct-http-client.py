#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that non-helper Go code in api/ and auth/ does not construct its own outbound HTTP clients.

All server-originated outbound HTTP MUST go through a hardened client
(api/safe_http_client.go for api/, or the shared internal/safehttp core via
auth's newProviderHTTPClient / safehttp.NewHardenedClient for auth/) so that
scheme allowlist, SSRF blocklist, DNS/dial-time IP-pinning, header-size cap,
body-size cap, and ResponseHeaderTimeout are enforced uniformly.

This check scans api/ and auth/ RECURSIVELY (so subpackages such as auth/saml/
are covered) and fails if any non-test, non-helper file either constructs
&http.Client{...} or references http.DefaultClient. New violations should be
fixed by routing through the hardened client instead of being added to an
exception list.

Usage:
    uv run scripts/check-direct-http-client.py
"""

import argparse
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_error,
    log_info,
    log_success,
)

# Files that legitimately construct their own http.Client. All previously-
# legacy outbound-HTTP callers in api/ have been migrated to SafeHTTPClient
# (issue #364). New violations should be fixed by routing through
# SafeHTTPClient instead of being added here.
LEGACY_EXCEPTIONS: set[str] = set()

# auth/ now routes all outbound HTTP through the shared internal/safehttp core
# (auth's newProviderHTTPClient and safehttp.NewHardenedClient), which pins the
# dialed IP and refuses redirects. No auth/ file constructs its own http.Client
# anymore, so this exception list is empty (issue #470/#471). New outbound HTTP
# in auth/ should reuse newProviderHTTPClient or safehttp.NewHardenedClient.
# Keys are project-root-relative paths.
HARDENED_AUTH_EXCEPTIONS: set[str] = set()

# The helper itself is allowed to construct an http.Client; that is its job.
HELPER_FILES = {
    "safe_http_client.go",
}

# Generated code never constructs outbound clients but is excluded defensively.
GENERATED_FILES = {
    "api.go",
}

PATTERN = re.compile(r"&http\.Client\{|http\.DefaultClient\b")


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Check that non-helper code in api/ and auth/ routes outbound HTTP "
            "through SafeHTTPClient (or an allowlisted hardened in-package "
            "client) instead of constructing its own http.Client."
        )
    )
    parser.parse_args()

    project_root = get_project_root()

    go_files: list[Path] = []
    for dir_name in ("api", "auth"):
        scan_dir = project_root / dir_name
        go_files.extend(sorted(p for p in scan_dir.rglob("*.go") if not p.name.endswith("_test.go")))
    if not go_files:
        log_error(f"No Go files found under {project_root}/api or {project_root}/auth")
        return 1

    log_info("Checking for direct http.Client / http.DefaultClient use in api/ and auth/...")

    violations: list[str] = []
    for go_file in go_files:
        rel_path = go_file.relative_to(project_root).as_posix()
        if go_file.name in HELPER_FILES or go_file.name in GENERATED_FILES:
            continue
        if go_file.name in LEGACY_EXCEPTIONS or rel_path in HARDENED_AUTH_EXCEPTIONS:
            continue

        lines = go_file.read_text(encoding="utf-8").splitlines()
        for lineno, line in enumerate(lines, start=1):
            stripped = line.strip()
            # Skip pure-comment lines.
            if stripped.startswith("//"):
                continue
            if PATTERN.search(line):
                violations.append(f"{go_file.relative_to(project_root)}:{lineno}: {stripped}")

    if violations:
        log_error("Found direct http.Client / http.DefaultClient use:")
        for v in violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            "Route outbound HTTP through SafeHTTPClient (api/safe_http_client.go) so that "
            "DNS-pinning, SSRF blocklist, header timeout, and body cap are enforced.",
            file=sys.stderr,
        )
        print(
            "If migration is genuinely impossible, add the file to LEGACY_EXCEPTIONS in "
            "scripts/check-direct-http-client.py and explain why in the commit message.",
            file=sys.stderr,
        )
        return 1

    log_success("No direct http.Client / http.DefaultClient use found")
    return 0


if __name__ == "__main__":
    sys.exit(main())
