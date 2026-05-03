#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz to every operation under
/threat_models, /threat_models/{threat_model_id}, /threat_models/{threat_model_id}/restore,
/threat_models/{threat_model_id}/diagrams, /threat_models/{threat_model_id}/diagrams/{diagram_id},
and /ws/ticket. Idempotent. Used once during slice 2 (#365) and then deleted
in Task 9 (#371).

Usage:
    uv run scripts/annotate-threat-models-authz.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"

# Per-path, per-method annotations. Mirrors the pre-existing role decisions
# in api/middleware.go::ThreatModelMiddleware (no-regression migration).
ANNOTATIONS = {
    "/threat_models": {
        "get":  {"ownership": "none"},
        "post": {"ownership": "none"},
    },
    "/threat_models/{threat_model_id}": {
        "get":    {"ownership": "reader"},
        "put":    {"ownership": "writer"},
        "patch":  {"ownership": "writer"},
        "delete": {"ownership": "owner"},
    },
    "/threat_models/{threat_model_id}/restore": {
        "post": {"ownership": "owner"},
    },
    "/threat_models/{threat_model_id}/diagrams": {
        "get":  {"ownership": "reader"},
        "post": {"ownership": "writer"},
    },
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}": {
        "get":    {"ownership": "reader"},
        "put":    {"ownership": "writer"},
        "patch":  {"ownership": "writer"},
        "delete": {"ownership": "owner"},
    },
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/restore": {
        "post": {"ownership": "owner"},
    },
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate": {
        # Matches the existing handler logic in
        # threat_model_diagram_handlers.go which checks RoleReader for all
        # three methods. Per-session host/participant rules remain in-handler.
        "get":    {"ownership": "reader"},
        "post":   {"ownership": "reader"},
        "delete": {"ownership": "reader"},
    },
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/model": {
        "get": {"ownership": "reader"},
    },
    "/ws/ticket": {
        # Authenticated, but the ticket itself is per-user with no
        # parent-resource ACL until it is redeemed against a diagram.
        "get": {"ownership": "none"},
    },
}


def main() -> int:
    root = get_project_root()
    spec_path = root / SPEC_PATH
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
            op["x-tmi-authz"] = rule
            annotated += 1

    spec_path.write_text(
        json.dumps(spec, indent=2, ensure_ascii=False) + "\n"
    )
    log_success(
        f"Annotated {annotated} operations under /threat_models and /ws/ticket "
        f"with x-tmi-authz"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
