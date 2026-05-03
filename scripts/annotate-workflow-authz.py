#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""One-shot annotator: adds x-tmi-authz to the workflow-stage and
cross-cutting endpoints covered by slice 7 (#370). Used once and then
deleted in Task 9 (#371).

Paths in scope:
- /intake/surveys, /intake/surveys/{id}
- /intake/survey_responses, /intake/survey_responses/{id} + nested
  metadata/triage_notes
- /triage/survey_responses, /triage/survey_responses/{id} + nested
  metadata/triage_notes/create_threat_model
- /projects, /projects/{id} + nested metadata/notes

All operations get `ownership: none`. The handler layer continues to
enforce:
- subject-self scoping for /intake/survey_responses (handler reads JWT
  subject and filters queries to that user, plus security-reviewer
  HasAccess for cross-user reads)
- team-membership checks for /projects (IsProjectTeamMemberOrAdmin)
- triage-role checks for /triage/* (currently TODO in survey_handlers.go;
  residual scope tracked for the workflow-stage role taxonomy in #371
  cleanup)

Why ownership=none and not a new "team_member" or "intake_submitter"
role: the underlying authorization model for projects (team membership)
and surveys (subject-self + HasAccess) is not the parent-threat-model
ACL pattern AuthzMiddleware enforces today. Introducing new ownership
levels or roles for them would require teaching the middleware to
resolve team membership / response ACLs at request time, which is a
larger change than this slice's scope. The handler-layer enforcement
is correct and was already in place; this slice just gets the workflow
paths under x-tmi-authz validator coverage.

Usage:
    uv run scripts/annotate-workflow-authz.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import get_project_root, log_info, log_success  # noqa: E402

SPEC_PATH = "api-schema/tmi-openapi.json"
HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

WORKFLOW_PREFIXES = ("/intake/", "/triage/", "/projects")


def main() -> int:
    root = get_project_root()
    spec_path = root / SPEC_PATH
    log_info(f"Loading {spec_path}")
    spec = json.loads(spec_path.read_text())

    annotated = 0
    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not any(path.startswith(p) for p in WORKFLOW_PREFIXES):
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
    log_success(f"Annotated {annotated} workflow operations with x-tmi-authz")
    return 0


if __name__ == "__main__":
    sys.exit(main())
