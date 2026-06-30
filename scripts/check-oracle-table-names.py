#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that GORM .Table("...") calls use dialect-correct table identifiers.

GORM quotes a bare single-identifier .Table("users") as "users" (quoted-lowercase)
through the dialect quoter. The Oracle GORM driver creates tables UPPERCASE
(OracleNamingStrategy / UseUppercaseTableNames), so "users" does not match the
USERS table on Oracle (ORA-00942 / no-match). The same happens for the table
identifier GORM emits when qualifying columns or running a map-based Create over
.Table(...). See issue #504.

The sanctioned form derives the dialect-correct name from the model:
.Table((&models.User{}).TableName()) -- or just .Model(&models.User{}).

This check flags only the dangerous shape: a single bare lowercase identifier
literal, e.g. .Table("users") (including chain-split forms where Table( begins a
continuation line). It intentionally does NOT flag:
  - Aliased/space forms like .Table("group_members gm") -- GORM emits these
    UNQUOTED (verified), so Oracle folds the lowercase to uppercase and they
    match. Quoting them would break them.
  - .Table((&models.X{}).TableName()) / .Table(expr) -- already dialect-correct.

LIMITATION: this is a string-literal check; it does NOT catch a bare lowercase
table name that reaches .Table() through a variable, const, or struct field
(e.g. tables := []struct{name string}{{"documents", ...}}; db.Table(t.name)).
There are none today, but a future dynamic table name would need a manual audit.

Usage:
    uv run scripts/check-oracle-table-names.py
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

# GORM-using repo roots (mirrors check-oracle-unsafe-map-keys.py).
SCAN_DIRS = ("api", "auth", "cmd", "internal")

# A bare single lowercase identifier table literal: .Table("users"),
# .Table("group_members"). \bTable (not \.Table) also matches chain-split calls
# where Table( starts a continuation line (the leading "." is on the prior line).
# The closing quote immediately after the identifier (no space) excludes aliased
# forms like .Table("group_members gm"). Uppercase literals (.Table("THREATS"))
# and expressions (.Table(x.TableName())) do not match.
PATTERN = re.compile(r'\bTable\("[a-z][a-z0-9_]*"\)')


def main() -> int:
    argparse.ArgumentParser(
        description=(
            "Check that GORM .Table(\"...\") calls use dialect-correct table "
            "identifiers (model TableName), not bare lowercase literals."
        )
    ).parse_args()

    project_root = get_project_root()
    go_files: list[Path] = []
    for dir_name in SCAN_DIRS:
        scan_dir = project_root / dir_name
        if scan_dir.is_dir():
            go_files.extend(sorted(p for p in scan_dir.rglob("*.go") if not p.name.endswith("_test.go")))
    if not go_files:
        log_error(f"No Go files found under {project_root} ({', '.join(SCAN_DIRS)})")
        return 1

    log_info("Checking GORM .Table() calls use dialect-correct table names (Oracle-safe)...")

    violations: list[str] = []
    for go_file in go_files:
        for lineno, line in enumerate(go_file.read_text(encoding="utf-8").splitlines(), start=1):
            stripped = line.strip()
            if stripped.startswith("//"):
                continue
            if PATTERN.search(line):
                violations.append(f"{go_file.relative_to(project_root)}:{lineno}: {stripped}")

    if violations:
        log_error("Found GORM .Table() calls with bare lowercase table literals:")
        for v in violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            'Use a dialect-correct table name: .Table((&models.X{}).TableName()) or '
            ".Model(&models.X{}). A bare .Table(\"users\") is quoted-lowercase and "
            "fails to match the uppercase Oracle table (ORA-00942). See issue #504.",
            file=sys.stderr,
        )
        return 1

    log_success("All GORM .Table() calls use dialect-correct table names")
    return 0


if __name__ == "__main__":
    sys.exit(main())
