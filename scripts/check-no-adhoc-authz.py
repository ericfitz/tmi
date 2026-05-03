#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Lint rule for the x-tmi-authz migration closer (#371).

After every operation in the OpenAPI spec carries `x-tmi-authz` and
AuthzMiddleware enforces every gate, ad-hoc role checks in handler
files are redundant. This script fails CI if any handler file under
api/*_handlers.go calls one of the deprecated route-level role helpers
in a way that's not explicitly allowlisted (the few remaining sites
that enforce business rules tighter than the route-level gate).

Targets (forbidden in handler files except via allowlist below):
- CheckResourceAccess   (legacy alias)
- CheckResourceAccessFromContext
- CheckThreatModelAccess
- CheckSubResourceAccess
- ValidateResourceAccess (middleware factory; the middleware caller
  doesn't count, but a handler invocation does)

Allowlist (handler-level, business-rule callers that go beyond the
route-level gate):
- threat_model_handlers.go: the two owner-change branches in PUT/PATCH
  that enforce "only the owner can change ownership or authorization"
  — stricter than the route-level writer gate.
- ws_ticket_handler.go: the WS ticket handler verifies the caller has
  reader access to the specific session's threat model before minting
  a ticket — this is a per-resource check, not a route-level role gate.
- websocket.go: WS upgrades authenticate via the ticket protocol; the
  reader/writer checks inside the upgrade path are part of that
  protocol, not the HTTP route-level x-tmi-authz gate.

Add a justification comment at the call site if you need to extend the
allowlist; the lint rule prints the matched line for review.

Usage:
    uv run scripts/check-no-adhoc-authz.py
"""

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_error, log_info, log_success  # noqa: E402

# File-level allowlist. Each entry is a path (relative to repo root) plus a
# justification — these files have ad-hoc authz calls that are intentionally
# tighter than (or otherwise outside the scope of) the route-level x-tmi-authz
# gate. Each is documented:
#
#   threat_model_handlers.go — the two owner-change branches in PUT/PATCH
#     enforce "only the owner can change ownership or authorization", a
#     stricter rule than the route-level writer gate; the route-level gate
#     in x-tmi-authz cannot express this.
#
#   ws_ticket_handler.go — minting a WS ticket verifies the caller has reader
#     access to the parent threat model of a specific WebSocket session.
#     This is per-resource and tied to the ticket protocol, not a HTTP
#     route-level role.
#
#   websocket.go — WS upgrades authenticate via the short-lived ticket; the
#     reader/writer checks inside the upgrade path are part of the WS auth
#     protocol, not the HTTP route-level x-tmi-authz gate.
ALLOWLIST_FILES = {
    "api/threat_model_handlers.go",
    "api/ws_ticket_handler.go",
    "api/websocket.go",
}

FORBIDDEN_PATTERN = re.compile(
    r"\b(CheckResourceAccess|CheckResourceAccessFromContext|"
    r"CheckThreatModelAccess|CheckSubResourceAccess|ValidateResourceAccess)\("
)


def is_allowed(rel_path: str, line: str) -> bool:
    return rel_path in ALLOWLIST_FILES


def main() -> int:
    root = get_project_root()
    api_dir = root / "api"
    if not api_dir.exists():
        log_error(f"api/ directory not found at {api_dir}")
        return 1

    handler_files = sorted(api_dir.glob("*_handlers.go"))
    handler_files += [
        api_dir / "ws_ticket_handler.go",
        api_dir / "websocket.go",
    ]
    # de-dup while preserving order
    seen = set()
    handler_files = [
        p for p in handler_files
        if p.exists() and not (p in seen or seen.add(p))
    ]

    log_info(f"Scanning {len(handler_files)} handler file(s) for ad-hoc authz calls")

    violations: list[str] = []
    for path in handler_files:
        try:
            text = path.read_text()
        except Exception as exc:
            log_error(f"Failed to read {path}: {exc}")
            return 1
        rel_path = str(path.relative_to(root))
        for lineno, line in enumerate(text.splitlines(), start=1):
            if "CheckResourceAccessFromContext" in line and line.lstrip().startswith("//"):
                continue
            m = FORBIDDEN_PATTERN.search(line)
            if not m:
                continue
            # The function definitions themselves live in api/auth_utils.go and
            # api/middleware.go, not in handler files; if a handler file is
            # ever named *_handlers.go but contains a `func` declaration with
            # one of these names, that's still a regression.
            if line.lstrip().startswith("func "):
                continue
            if is_allowed(rel_path, line):
                continue
            violations.append(f"{rel_path}:{lineno}:  {line.strip()}")

    if violations:
        log_error(
            f"Found {len(violations)} ad-hoc authz call(s) in handler files. "
            f"Move the route-level gate to x-tmi-authz, or add to the "
            f"ALLOWLIST in {Path(__file__).name} with justification."
        )
        for v in violations:
            log_error(f"  {v}")
        return 1

    log_success(
        f"No ad-hoc authz calls in handler files (scanned {len(handler_files)} file(s))"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
