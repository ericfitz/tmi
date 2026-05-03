#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that EVERY OpenAPI operation in api-schema/tmi-openapi.json carries
`x-tmi-authz`. Default-deny: any operation lacking the extension causes the
build to fail. Slice 8 (#371) flipped this from the per-prefix allowlist that
slices 1-7 grew during the migration; the allowlist is intentionally gone so
new endpoints cannot be added without declaring authz.

Also validates the shape of each `x-tmi-authz` value against the schema
documented in `api-schema/x-tmi-authz-schema.md`.

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

HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

VALID_OWNERSHIP = {"none", "reader", "writer", "owner"}
VALID_ROLES = {"admin", "security_reviewer", "automation", "confidential_reviewer"}
VALID_AUDIT = {"required", "optional"}


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
            "Default-deny check: every OpenAPI operation must carry x-tmi-authz."
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
    total = 0

    paths = spec.get("paths", {})
    for path, path_item in paths.items():
        if not isinstance(path_item, dict):
            continue
        for method, op in path_item.items():
            if method not in HTTP_METHODS:
                continue
            if not isinstance(op, dict):
                continue
            total += 1
            authz = op.get("x-tmi-authz")
            if authz is None:
                missing.append(f"{method.upper()} {path}")
                continue
            invalid.extend(validate_authz_value(path, method, authz))

    if missing:
        log_error(
            f"Default-deny: {len(missing)} operation(s) missing x-tmi-authz "
            f"(out of {total} total). Add x-tmi-authz to each before merging."
        )
        for m in missing:
            log_error(f"  {m}")
    if invalid:
        log_error("Invalid x-tmi-authz values:")
        for i in invalid:
            log_error(f"  {i}")

    if missing or invalid:
        return 1

    log_success(f"x-tmi-authz check passed: all {total} operations annotated")
    return 0


if __name__ == "__main__":
    sys.exit(main())
