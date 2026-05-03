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

# Prefix allowlist for covered families. Subsequent slices append; slice 8
# (#371) removes the allowlist entirely and enforces on every operation.
#
# Slice 1 (#341): /admin/, /.well-known/, /oauth2/, /saml/
# Slice 2 (#365): /threat_models top-level + /diagrams top-level (added as
#   exact paths to avoid pulling in slice-3 sub-resources prematurely).
# Slice 3 (#366): /threat_models/ as a prefix — covers every nested sub-
#   resource (threats, documents, notes, assets, repositories, audit_trail,
#   metadata, plus diagram-nested metadata/audit_trail). The COVERED_DENY
#   list below still excludes /chat/ which slice 5 (#369) owns.
COVERED_PREFIXES = (
    "/admin/",
    "/.well-known/",
    "/oauth2/",
    "/saml/",
    "/threat_models/",
    "/me/",
    "/addons",
    "/automation/",
    "/intake/",
    "/triage/",
    "/projects",
)

# Path-pattern denylist applied AFTER COVERED_PREFIXES. Operations under these
# prefixes are owned by other slices and not yet required to carry
# x-tmi-authz. Each entry is removed when its owning slice lands.
COVERED_DENY_PREFIXES: tuple[str, ...] = ()

# Exact-path covered operations (not prefix-matched).
COVERED_EXACT = (
    "/",
    "/config",
    "/webhook-deliveries/{delivery_id}/status",
    "/ws/ticket",
    "/threat_models",
    "/me",
)

HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

VALID_OWNERSHIP = {"none", "reader", "writer", "owner"}
VALID_ROLES = {"admin", "security_reviewer", "automation", "confidential_reviewer"}
VALID_AUDIT = {"required", "optional"}


def is_covered(path: str) -> bool:
    if path in COVERED_EXACT:
        return True
    if not any(path.startswith(p) for p in COVERED_PREFIXES):
        return False
    # A prefix matched. Apply the denylist for sub-trees still owned by other
    # slices.
    return not any(path.startswith(p) for p in COVERED_DENY_PREFIXES)


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
