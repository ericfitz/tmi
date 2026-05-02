#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz with the admin role to every operation
under /admin/. Idempotent. Used once during the foundation slice and then
deleted in Task 9.

Usage:
    uv run scripts/annotate-admin-authz.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}


def main() -> int:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    spec = json.loads(spec_path.read_text())

    annotated = 0
    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not path.startswith("/admin/"):
            continue
        for method, op in path_item.items():
            if method not in HTTP_METHODS:
                continue
            op["x-tmi-authz"] = {"ownership": "none", "roles": ["admin"]}
            annotated += 1

    spec_path.write_text(json.dumps(spec, indent=2) + "\n")
    log_success(f"Annotated {annotated} /admin/ operations with x-tmi-authz")
    return 0


if __name__ == "__main__":
    sys.exit(main())
