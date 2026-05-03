#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz to every operation under /me/* and to
the /me top-level operation. Used once during slice 4 (#367) and then deleted
in Task 9 (#371).

All /me/* endpoints share the same model: authenticated, scoped to the JWT
subject. The middleware uses `ownership: none` (no resource-level ACL check)
because the URL has no parameter identifying a different user — handlers
read the JWT subject from context and scope queries to that subject. The
defense against user-A-accessing-user-B's-data lives in the handlers, not
in the middleware (the middleware would have to issue a DB lookup to
correlate, e.g., a credential_id with its owner).

Usage:
    uv run scripts/annotate-me-authz.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_info, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}


def main() -> int:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    log_info(f"Loading {spec_path}")
    spec = json.loads(spec_path.read_text())

    annotated = 0
    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if path != "/me" and not path.startswith("/me/"):
            continue
        if not isinstance(path_item, dict):
            continue
        for method, op in path_item.items():
            if method not in HTTP_METHODS:
                continue
            if not isinstance(op, dict):
                continue
            op["x-tmi-authz"] = {"ownership": "none"}
            annotated += 1

    spec_path.write_text(
        json.dumps(spec, indent=2, ensure_ascii=False) + "\n"
    )
    log_success(f"Annotated {annotated} /me/* operations with x-tmi-authz")
    return 0


if __name__ == "__main__":
    sys.exit(main())
