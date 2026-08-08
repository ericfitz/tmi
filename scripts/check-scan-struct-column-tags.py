#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that GORM structs do not hardcode lowercase `gorm:"column:..."` tags.

Result-set column labels come back UPPERCASE from Oracle and lowercase from
PostgreSQL, and GORM matches labels to DBNames case-sensitively. An untagged
exported field's DBName is derived from the dialect's NamingStrategy
(auth/db.OracleNamingStrategy on Oracle, the default lowercase-snake_case
strategy on Postgres/SQLite), so the mapping holds on both engines. A
hardcoded lowercase `column:` tag pins the DBName to the Postgres label and
silently zeroes that field on every Oracle scan (issue #699) -- no error, no
panic, just empty results.

The sanctioned form drops the `column:` tag entirely and lets the field name
derive the DBName (see the #699 comment in api/group_member_repository.go's
GetGroupsForUser, or api/project_store_gorm.go's List). Where a SELECT emits a
join alias that has no field of its own on the destination struct, emit the
alias itself through ColumnName(<db>.Name(), "alias") (api/dialect_helpers.go)
so the label matches the struct field's derived DBName on both engines.

This is a substring tripwire, matching any `gorm:"...column:<lowercase>` tag
on a non-comment line. It does not distinguish ad-hoc scan structs from GORM
models with legitimate PostgreSQL-only reasons to pin a lowercase column name
(none exist in this repo today) -- use the allowlist below for a reviewed,
documented exception, or an inline `//oracle-column-tag:exempt <reason>` on
the line above the struct field.

Scope note: internal/dbschema/ is excluded (see EXCLUDED_PATH_PREFIXES below)
-- schema introspection helpers that read catalog metadata, not application
row data. api/models/ was excluded for the same reason api/group_member_repository.go
needed fixing under #699: 41 hardcoded lowercase column: tags on registered
GORM models (AliasCounter's proven to cause ORA-00001 duplicate-alias
collisions on Oracle). All 41 were fixed under #710. Two of them --
UsabilityFeedback.CreatedByUUID and ContentFeedback.CreatedByUUID -- could not
simply drop the tag, because the field name's NamingStrategy-derived DBName
("created_by_uuid", UUID being a common-initialism word boundary) diverged
from the actual, already-migrated column ("created_by"); those two fields
were renamed to CreatedBy instead, so the derived DBName now equals the real
column with no tag and no exemption needed. No allowlisted or
`//oracle-column-tag:exempt`-marked sites exist in this repo as of #710.

Usage:
    uv run scripts/check-scan-struct-column-tags.py
"""

import argparse
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_error,
    log_info,
    log_success,
)

# GORM-using repo roots (mirrors check-oracle-unsafe-map-keys.py /
# check-oracle-table-names.py).
SCAN_DIRS = ("api", "auth", "cmd", "internal")

# Directory prefixes excluded even though they fall under SCAN_DIRS:
#   - internal/dbschema: schema introspection helpers that read catalog
#     metadata, not application row data -- not part of the #699 bug class.
# api/models/ is intentionally NOT excluded (#710 removed that exclusion once
# the 41 pre-existing violations there were fixed) -- the guard now covers the
# canonical GORM model definitions too, with zero exceptions: the two fields
# that couldn't simply drop their tag (UsabilityFeedback/ContentFeedback
# CreatedByUUID) were renamed to CreatedBy instead, so no allowlist or inline
# `//oracle-column-tag:exempt` marker is needed for them.
# Matched as path prefixes (not bare directory-name segments) so a
# same-named directory elsewhere in the tree (e.g. a future auth/models/)
# is not silently swept in.
EXCLUDED_PATH_PREFIXES = ("internal/dbschema/",)

# A `gorm:"..."` tag whose `column:` sub-tag names a lowercase-leading
# identifier. Matches regardless of where column: falls in the tag string
# (e.g. `gorm:"column:foo;index"` or `gorm:"index;column:foo"`).
PATTERN = re.compile(r'gorm:"[^"]*column:[a-z]')

# An exemption marker on the line immediately above the struct field.
EXEMPT = re.compile(r"//\s*oracle-column-tag:exempt\b")

# Reviewed-and-safe sites: (relative POSIX path, reason). Empty today -- every
# site found during the #699 sweep was either fixed (untagged field) or, for
# api/notifications/polling.go's NotificationQueueEntry, confirmed
# Oracle-reachable (factory.go selects PollingNotifier for Oracle) and fixed
# rather than allowlisted.
ALLOWLIST: dict[str, str] = {}


def main() -> int:
    argparse.ArgumentParser(
        description=(
            "Check that GORM structs do not hardcode lowercase "
            'gorm:"column:..." tags, which silently zero fields on Oracle.'
        )
    ).parse_args()

    project_root = get_project_root()
    go_files: list[Path] = []
    for dir_name in SCAN_DIRS:
        scan_dir = project_root / dir_name
        if not scan_dir.is_dir():
            continue
        for p in sorted(scan_dir.rglob("*.go")):
            if p.name.endswith("_test.go"):
                continue
            rel_posix = p.relative_to(project_root).as_posix()
            if any(rel_posix.startswith(prefix) for prefix in EXCLUDED_PATH_PREFIXES):
                continue
            go_files.append(p)

    if not go_files:
        log_error(f"No Go files found under {project_root} ({', '.join(SCAN_DIRS)})")
        return 1

    log_info('Checking for hardcoded lowercase gorm:"column:..." tags (Oracle-safe)...')

    violations: list[str] = []
    for go_file in go_files:
        rel = go_file.relative_to(project_root).as_posix()
        if rel in ALLOWLIST:
            continue
        lines = go_file.read_text(encoding="utf-8").splitlines()
        for lineno, line in enumerate(lines, start=1):
            stripped = line.strip()
            if stripped.startswith("//"):
                continue
            if not PATTERN.search(line):
                continue
            if lineno >= 2 and EXEMPT.search(lines[lineno - 2]):
                continue
            violations.append(f"{rel}:{lineno}: {stripped}")

    if violations:
        log_error('Found hardcoded lowercase gorm:"column:..." tags:')
        for v in violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            'Drop the column: tag and let the field name derive the DBName via the '
            "dialect NamingStrategy. Where a SELECT emits a join alias with no field "
            "of its own, emit the alias through ColumnName(<db>.Name(), \"alias\") "
            "(api/dialect_helpers.go) instead of a hardcoded tag. Hardcoded lowercase "
            "column: tags silently zero every field on Oracle -- no error, just empty "
            "results. See issue #699.",
            file=sys.stderr,
        )
        return 1

    log_success('No hardcoded lowercase gorm:"column:..." tags found')
    return 0


if __name__ == "__main__":
    sys.exit(main())
