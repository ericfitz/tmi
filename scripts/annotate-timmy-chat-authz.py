#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz to the Timmy chat-session operations
under /threat_models/{tm_id}/chat/* covered by slice 6 (#369). Used once
and then deleted in Task 9 (#371).

Role mapping (per the issue body's "writer to start, reader to view"):
- GET sessions / GET session / GET messages: reader
- POST sessions (start), POST messages (chat), POST refresh_sources,
  DELETE session: writer

The other Timmy and content-OAuth paths the issue lists are already
annotated by earlier slices:
  /admin/timmy/*               — slice 1 (#341 admin)
  /admin/users/{id}/content_*  — slice 1 (#341 admin)
  /me/content_tokens/*         — slice 4 (#367 /me)
  /oauth2/content_callback     — slice 1 (#341 public/oauth2)

Usage:
    uv run scripts/annotate-timmy-chat-authz.py
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
    "/threat_models/{threat_model_id}/chat/sessions": {
        "get":  {"ownership": "reader"},
        "post": {"ownership": "writer"},
    },
    "/threat_models/{threat_model_id}/chat/sessions/{session_id}": {
        "get":    {"ownership": "reader"},
        "delete": {"ownership": "writer"},
    },
    "/threat_models/{threat_model_id}/chat/sessions/{session_id}/messages": {
        "get":  {"ownership": "reader"},
        "post": {"ownership": "writer"},
    },
    "/threat_models/{threat_model_id}/chat/sessions/{session_id}/refresh_sources": {
        "post": {"ownership": "writer"},
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
    log_success(f"Annotated {annotated} Timmy chat-session operations with x-tmi-authz")
    return 0


if __name__ == "__main__":
    sys.exit(main())
