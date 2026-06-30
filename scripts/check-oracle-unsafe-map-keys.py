#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that GORM map-keyed WHERE/condition predicates route through ColumnMap.

GORM emits ``map[string]any`` predicate keys VERBATIM through the dialect quoter
-- it does NOT run them through the NamingStrategy the way it does struct-field
queries (verified empirically; see issue #503). The Oracle GORM driver creates
columns UPPERCASE (auth/db.OracleNamingStrategy), so a bare lowercase literal
key such as ``Where(map[string]any{"team_id": id})`` produces a quoted-lowercase
``"team_id"`` that fails to match the ``"TEAM_ID"`` column (ORA-00904 / silent
no-match) on Oracle, while working fine on PostgreSQL.

The sanctioned form wraps the map in ``ColumnMap(<db>.Name(), ...)`` (see
api/dialect_helpers.go), which uppercases the keys on Oracle and is a passthrough
on PostgreSQL/SQLite.

This is a line-based tripwire: it flags any line that calls a GORM
condition-bearing method (Where/Or/Not/First/Last/Take/Find/Delete) with an
inline ``map[string]any{"...`` / ``map[string]interface{}{"...`` literal that is
NOT wrapped in ColumnMap. It intentionally does NOT flag ``Updates``/``Assign``/
``Attrs`` map payloads -- those resolve their keys against the model schema (so
the NamingStrategy applies) and are safe.

Usage:
    uv run scripts/check-oracle-unsafe-map-keys.py
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

# Repo roots that use GORM. The bug surfaces anywhere a map-keyed predicate is
# built, so the guard must cover every GORM-using tree -- not just api/ (see
# issue #503 note 2: the original api/-only scope missed cmd/server/main.go).
SCAN_DIRS = ("api", "auth", "cmd", "internal")

# GORM methods that accept inline conditions (WHERE-class). Updates/Assign/Attrs
# are intentionally excluded -- their map keys resolve through the model schema
# (NamingStrategy applies) and are Oracle-safe.
CONDITION_METHODS = "Where|Or|Not|First|Last|Take|Find|Delete"

# A map literal opening: map[string]any{ or map[string]interface{}{ .
MAP_LIT = re.compile(r"map\[string\](?:any|interface\{\})\{")

# The map is the predicate of a condition method, possibly after a destination
# first arg (e.g. First(&x, map{...}) / Delete(&models.Y{}, map{...})). re.DOTALL
# is NOT used; instead we collapse the lookbehind window so a map literal whose
# opening brace is on the line AFTER the `.Where(` is still caught (issue #503
# note 3: the original same-line-only regex had a multi-line blind spot).
PRECEDER = re.compile(
    r"\.(?:" + CONDITION_METHODS + r")\(\s*(?:&?[\w.]+(?:\{\})?\s*,\s*)?$"
)


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Check that GORM map-keyed WHERE/condition predicates route through "
            "ColumnMap() so column identifiers are cased correctly for Oracle."
        )
    )
    parser.parse_args()

    project_root = get_project_root()
    go_files: list[Path] = []
    for dir_name in SCAN_DIRS:
        scan_dir = project_root / dir_name
        if scan_dir.is_dir():
            go_files.extend(sorted(p for p in scan_dir.rglob("*.go") if not p.name.endswith("_test.go")))
    if not go_files:
        log_error(f"No Go files found under {project_root} ({', '.join(SCAN_DIRS)})")
        return 1

    log_info("Checking GORM map-keyed predicates route through ColumnMap (Oracle-safe)...")

    violations: list[str] = []
    for go_file in go_files:
        content = go_file.read_text(encoding="utf-8")
        for m in MAP_LIT.finditer(content):
            # Window before the map literal, whitespace/newlines collapsed so a
            # `.Where(` on the previous line still anchors the PRECEDER match
            # (the regex's trailing \s* consumes any space before the map).
            window = content[max(0, m.start() - 120):m.start()]
            collapsed = re.sub(r"\s+", " ", window)
            # Already wrapped in ColumnMap (incl. api.ColumnMap) -> safe.
            if "ColumnMap(" in collapsed[-60:]:
                continue
            if PRECEDER.search(collapsed):
                lineno = content.count("\n", 0, m.start()) + 1
                snippet = content[m.start():m.start() + 60].splitlines()[0]
                violations.append(f"{go_file.relative_to(project_root)}:{lineno}: ...{snippet}")

    if violations:
        log_error("Found GORM map-keyed predicates not routed through ColumnMap:")
        for v in violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            "Wrap the map literal in ColumnMap(<db>.Name(), ...) (api/dialect_helpers.go) "
            "so column identifiers are uppercased on Oracle. GORM emits map predicate keys "
            "verbatim, so a bare lowercase key fails to match the uppercase Oracle column "
            "(ORA-00904 / silent no-match). See issue #503.",
            file=sys.stderr,
        )
        return 1

    log_success("All GORM map-keyed predicates route through ColumnMap")
    return 0


if __name__ == "__main__":
    sys.exit(main())
