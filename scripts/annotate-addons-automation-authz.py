#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz to the addon and automation-embedding
operations covered by slice 5 (#368). Used once and then deleted in
Task 9 (#371).

Role mapping:
- /addons GET (listAddons), /addons/{id} GET (getAddon), /addons/{id}/invoke
  POST (invokeAddon): authenticated users only — ownership=none, no role gate.
  Lists, views, and invocations are open to any authenticated caller; the
  invocation handler enforces rate-limits and per-user invocation quotas.
- /addons POST (createAddon), /addons/{id} DELETE (deleteAddon): admin-only,
  matches the existing handler-level RequireAdministrator gate.
- /automation/embeddings/{tm_id} POST/DELETE, /automation/embeddings/
  {tm_id}/config GET: ownership=none + roles=[automation]. The middleware
  layer accepts membership in either tmi-automation or embedding-automation;
  the EmbeddingAutomationMiddleware narrows the inner gate to embedding-
  automation specifically. /admin/users/automation is already covered by
  slice 1 (#341).

This slice intentionally does not yet add a `subject_authority` field to
the schema. #358 (T18 scoped delegation tokens) introduces the runtime
check; until then `subject_authority` would be descriptive only and risks
schema drift if #358 reshapes the value space.

Usage:
    uv run scripts/annotate-addons-automation-authz.py
"""

import json
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_info, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

ANNOTATIONS: dict[str, dict[str, dict[str, Any]]] = {
    "/addons": {
        "get":  {"ownership": "none"},
        "post": {"ownership": "none", "roles": ["admin"]},
    },
    "/addons/{id}": {
        "get":    {"ownership": "none"},
        "delete": {"ownership": "none", "roles": ["admin"]},
    },
    "/addons/{id}/invoke": {
        "post": {"ownership": "none"},
    },
    "/automation/embeddings/{threat_model_id}": {
        "post":   {"ownership": "none", "roles": ["automation"]},
        "delete": {"ownership": "none", "roles": ["automation"]},
    },
    "/automation/embeddings/{threat_model_id}/config": {
        "get": {"ownership": "none", "roles": ["automation"]},
    },
}


def main() -> int:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    log_info(f"Loading {spec_path}")
    spec = json.loads(spec_path.read_text())

    annotated = 0
    paths = spec.get("paths", {})
    for path, methods in ANNOTATIONS.items():
        path_item = paths.get(path)
        if path_item is None:
            print(f"ERROR: path not found in spec: {path}", file=sys.stderr)
            return 1
        for method, rule in methods.items():
            op = path_item.get(method)
            if op is None:
                print(f"ERROR: {method.upper()} {path} not found in spec", file=sys.stderr)
                return 1
            if method not in HTTP_METHODS:
                continue
            op["x-tmi-authz"] = rule
            annotated += 1

    spec_path.write_text(
        json.dumps(spec, indent=2, ensure_ascii=False) + "\n"
    )
    log_success(f"Annotated {annotated} addon/automation operations with x-tmi-authz")
    return 0


if __name__ == "__main__":
    sys.exit(main())
