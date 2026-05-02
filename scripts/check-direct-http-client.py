#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that non-helper Go code in api/ does not construct its own outbound HTTP clients.

All server-originated outbound HTTP MUST go through SafeHTTPClient
(api/safe_http_client.go) so that scheme allowlist, SSRF blocklist, DNS-pinning,
header-size cap, body-size cap, and ResponseHeaderTimeout are enforced uniformly.

This check fails if any non-test, non-helper file under api/ either constructs
&http.Client{...} or references http.DefaultClient. The exception list below
documents pre-existing legacy callers that have not yet been migrated; they are
tracked by a follow-up issue. New violations should be fixed by routing through
SafeHTTPClient instead of being added to the exception list.

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

# Files that legitimately construct their own http.Client because they predate
# SafeHTTPClient and call fixed operator-configured endpoints (OAuth providers,
# Microsoft Graph, LLM APIs). These should be migrated to SafeHTTPClient over
# time; see the follow-up issue tracking that work.
LEGACY_EXCEPTIONS = {
    "content_oauth_provider.go",
    "content_oauth_provider_confluence.go",
    "content_source_confluence.go",
    "content_source_microsoft_graph.go",
    "microsoft_picker_grant_handler.go",
    "timmy_llm_service.go",
    "timmy_reranker.go",
}

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
            "Check that non-helper code in api/ routes outbound HTTP through "
            "SafeHTTPClient instead of constructing its own http.Client."
        )
    )
    parser.parse_args()

    project_root = get_project_root()
    api_dir = project_root / "api"

    go_files = sorted(p for p in api_dir.glob("*.go") if not p.name.endswith("_test.go"))
    if not go_files:
        log_error(f"No Go files found in {api_dir}")
        return 1

    log_info("Checking for direct http.Client / http.DefaultClient use in api/...")

    violations: list[str] = []
    for go_file in go_files:
        if go_file.name in HELPER_FILES or go_file.name in GENERATED_FILES:
            continue
        if go_file.name in LEGACY_EXCEPTIONS:
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
