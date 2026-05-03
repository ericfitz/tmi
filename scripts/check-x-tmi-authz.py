#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that every OpenAPI operation under an allow-listed prefix family carries
`x-tmi-authz`. The prefix list grows per slice (#365–#370). Slice 8 (#371)
removes the prefix list and enforces the rule on every operation.

Also validates the shape of each `x-tmi-authz` value against the schema documented
in `api-schema/x-tmi-authz-schema.md`.

Usage:
    uv run scripts/check-x-tmi-authz.py
"""

import argparse
import json
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_error,
    log_info,
    log_success,
)

SPEC_PATH = "api-schema/tmi-openapi.json"

# Prefix allowlist for slice 1 (foundation + admin + public). Subsequent slices
# append to this list. Slice 8 (#371) removes it entirely.
COVERED_PREFIXES = (
    "/admin/",
    "/.well-known/",
    "/oauth2/",
    "/saml/",
)

# Exact-path covered operations (not prefix-matched).
# Slice 2 (#365) adds the threat-model top-level and diagram top-level paths
# plus /ws/ticket. Sub-resources of threat models (threats, documents, notes,
# repositories, assets, audit_trail, metadata, chat, embeddings) are scoped to
# slice 3 (#366) and remain unchecked here until that slice lands.
COVERED_EXACT = (
    "/",
    "/config",
    "/webhook-deliveries/{delivery_id}/status",
    "/ws/ticket",
    "/threat_models",
    "/threat_models/{threat_model_id}",
    "/threat_models/{threat_model_id}/restore",
    "/threat_models/{threat_model_id}/diagrams",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/restore",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate",
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/model",
)

HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

VALID_OWNERSHIP = {"none", "reader", "writer", "owner"}
VALID_ROLES = {"admin", "security_reviewer", "automation", "confidential_reviewer"}
VALID_AUDIT = {"required", "optional"}


def is_covered(path: str) -> bool:
    if path in COVERED_EXACT:
        return True
    return any(path.startswith(p) for p in COVERED_PREFIXES)


# NOTE: future fields like `subject` (slice 4 / #367) and `subject_authority`
# (slice 5 / #368) are intentionally NOT validated here. The slice that adds
# them must extend VALID_* sets and add corresponding validation. Until then,
# unrecognized fields are silently ignored — adding a future field early gives
# false confidence.
def validate_authz_value(path: str, method: str, value: Any) -> list[str]:
    errors: list[str] = []
    where = f"{method.upper()} {path}"

    if not isinstance(value, dict):
        errors.append(f"{where}: x-tmi-authz must be an object")
        return errors

    ownership = value.get("ownership")
    if ownership not in VALID_OWNERSHIP:
        errors.append(
            f"{where}: x-tmi-authz.ownership must be one of {sorted(VALID_OWNERSHIP)} "
            f"(got {ownership!r})"
        )

    roles = value.get("roles", [])
    if not isinstance(roles, list):
        errors.append(f"{where}: x-tmi-authz.roles must be an array")
    else:
        for r in roles:
            if r not in VALID_ROLES:
                errors.append(
                    f"{where}: x-tmi-authz.roles entry {r!r} must be one of "
                    f"{sorted(VALID_ROLES)}"
                )

    public = value.get("public", False)
    if not isinstance(public, bool):
        errors.append(f"{where}: x-tmi-authz.public must be a boolean")

    if public:
        # Only complain about ownership/roles invariants when ownership itself
        # is a known value — otherwise the prior check has already reported it
        # and the redundant message is just noise.
        if ownership in VALID_OWNERSHIP and ownership != "none":
            errors.append(
                f"{where}: x-tmi-authz.public=true requires ownership='none' "
                f"(got {ownership!r})"
            )
        if roles:
            errors.append(
                f"{where}: x-tmi-authz.public=true requires roles=[] "
                f"(got {roles!r})"
            )

    audit = value.get("audit")
    if audit is not None and audit not in VALID_AUDIT:
        errors.append(
            f"{where}: x-tmi-authz.audit must be one of {sorted(VALID_AUDIT)} "
            f"(got {audit!r})"
        )

    return errors


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Check that every OpenAPI operation under an allow-listed prefix family "
            "carries x-tmi-authz."
        )
    )
    parser.parse_args()

    root = get_project_root()
    spec_path = root / SPEC_PATH
    log_info(f"Loading {spec_path}")
    try:
        spec = json.loads(spec_path.read_text())
    except FileNotFoundError:
        log_error(f"Spec file not found: {spec_path}")
        return 1
    except json.JSONDecodeError as exc:
        log_error(f"Spec file contains invalid JSON: {exc}")
        return 1

    missing: list[str] = []
    invalid: list[str] = []

    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not is_covered(path):
            continue
        for method, op in path_item.items():
            if method not in HTTP_METHODS:
                continue
            authz = op.get("x-tmi-authz")
            if authz is None:
                missing.append(f"{method.upper()} {path}")
                continue
            invalid.extend(validate_authz_value(path, method, authz))

    if missing:
        log_error("Operations missing x-tmi-authz:")
        for m in missing:
            log_error(f"  {m}")
    if invalid:
        log_error("Invalid x-tmi-authz values:")
        for i in invalid:
            log_error(f"  {i}")

    if missing or invalid:
        return 1

    log_success(
        f"x-tmi-authz check passed: covered prefixes {COVERED_PREFIXES} "
        f"and exact paths {COVERED_EXACT}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
