#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz to every threat-model nested
sub-resource operation under /threat_models/{threat_model_id}/... (assets,
audit_trail, documents, notes, repositories, threats, plus diagram
metadata/audit_trail and TM-level metadata).

Used once during slice 3 (#366) and then deleted in Task 9 (#371).

Rules (mechanical per #366):
- GET → reader
- POST/PUT/PATCH → writer
- DELETE → writer (relaxation from legacy ThreatModelMiddleware DELETE=Owner;
  the issue body explicitly specifies "writer for write" for sub-resources)
- Special: POST .../restore → owner (preserved from legacy behavior)
- Special: POST .../audit_trail/{id}/rollback → owner (destructive)

Excludes:
- /threat_models top-level + /diagrams + /diagrams/{id} top-level + /restore +
  /collaborate + /model (already annotated by slice 2 / #365)
- /chat/* (slice 5 / #369)

Usage:
    uv run scripts/annotate-tm-sub-resources-authz.py
"""

import json
import re
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_info, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

# Paths covered by slice 2 (#365) — must NOT be re-annotated.
SLICE_2_PATHS = {
    "/threat_models",
    "/threat_models/{threat_model_id}",
    "/threat_models/{threat_model_id}/restore",
    "/threat_models/{threat_model_id}/diagrams",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/restore",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/model",
}

# Path-pattern excludes (regex on the path string) for sub-slices that own
# their own annotation work.
SLICE_OTHER_PATTERNS = [
    re.compile(r"^/threat_models/\{threat_model_id\}/chat/"),
]


def is_in_scope(path: str) -> bool:
    if not path.startswith("/threat_models/{threat_model_id}/"):
        return False
    if path in SLICE_2_PATHS:
        return False
    for pat in SLICE_OTHER_PATTERNS:
        if pat.search(path):
            return False
    return True


def rule_for(path: str, method: str) -> dict[str, Any]:
    method = method.lower()
    if method == "get":
        return {"ownership": "reader"}
    # POST .../restore — preserve legacy Owner gate
    if method == "post" and path.endswith("/restore"):
        return {"ownership": "owner"}
    # POST .../audit_trail/{entry_id}/rollback — destructive admin
    if method == "post" and path.endswith("/rollback"):
        return {"ownership": "owner"}
    # All other writes (POST/PUT/PATCH/DELETE) — writer per the mechanical rule.
    return {"ownership": "writer"}


def main() -> int:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    log_info(f"Loading {spec_path}")
    spec = json.loads(spec_path.read_text())

    annotated = 0
    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not is_in_scope(path):
            continue
        for method, op in list(path_item.items()):
            if method not in HTTP_METHODS:
                continue
            if not isinstance(op, dict):
                continue
            op["x-tmi-authz"] = rule_for(path, method)
            annotated += 1

    spec_path.write_text(
        json.dumps(spec, indent=2, ensure_ascii=False) + "\n"
    )
    log_success(f"Annotated {annotated} threat-model sub-resource operations with x-tmi-authz")
    return 0


if __name__ == "__main__":
    sys.exit(main())
