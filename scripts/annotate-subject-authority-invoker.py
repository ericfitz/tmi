#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator for #358 (T18): adds `subject_authority: invoker` to
every write operation on threat-model and threat-model-nested paths. After
this runs, service-account-only tokens (`sub: sa:*`) are rejected on those
routes by AuthzMiddleware. Addons performing write-backs MUST use the
scoped delegation token from the `X-TMI-Delegation-Token` header
(populated by the webhook delivery worker) instead.

Read paths are intentionally NOT annotated — addons and other automation
may legitimately read threat-model data with their own service-account
credentials, and the §8 mitigation in #358 explicitly carves out reads
("Reads can remain on the addon's broad service-account creds if needed
(with audit), but writes must use the scoped delegation token.")

This is a one-shot bootstrapper; once committed, new write endpoints
must include `subject_authority: invoker` from day one. The validator
script `check-x-tmi-authz.py` accepts the field; a missing value is
allowed (default = "any") so non-write endpoints don't need it.

Usage:
    uv run scripts/annotate-subject-authority-invoker.py
"""

import json
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_info, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"

WRITE_METHODS = {"post", "put", "patch", "delete"}

# Apply to writes under any /threat_models/* path. Excludes:
#   - GET (always)
#   - /threat_models top-level POST (creating a TM is open to any
#     authenticated user; SAs creating TMs are a legitimate workflow if
#     anyone deems them so. The confused-deputy concern is on writes to
#     EXISTING TMs, not on creating new ones).
TARGET_PATTERN = re.compile(r"^/threat_models/\{threat_model_id\}")


def main() -> int:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    log_info(f"Loading {spec_path}")
    spec = json.loads(spec_path.read_text())

    annotated = 0
    skipped_existing = 0
    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not TARGET_PATTERN.match(path):
            continue
        if not isinstance(path_item, dict):
            continue
        for method, op in path_item.items():
            if method not in WRITE_METHODS:
                continue
            if not isinstance(op, dict):
                continue
            authz = op.get("x-tmi-authz")
            if not isinstance(authz, dict):
                # Default-deny validator caught this elsewhere; skip.
                continue
            if authz.get("subject_authority"):
                skipped_existing += 1
                continue
            authz["subject_authority"] = "invoker"
            annotated += 1

    spec_path.write_text(
        json.dumps(spec, indent=2, ensure_ascii=False) + "\n"
    )
    log_success(
        f"Annotated {annotated} write operation(s) with subject_authority=invoker; "
        f"{skipped_existing} already had a value"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
