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
NOT wrapped in ColumnMap.

It ALSO flags map-keyed ``Updates(...)`` payloads, which requires
``AssignmentMap(<db>.Name(), ...)`` (issue #615). The earlier claim that these
"resolve their keys against the model schema, so the NamingStrategy applies and
they are safe" holds only by accident, on two counts:

1. The emitted SQL is correct because GORM's ``LookUpField`` has a namer-driven
   fallback that runs ``modified_at`` through the Oracle naming strategy. That
   is a GORM *version* dependency -- on a release without the fallback the raw
   key is emitted verbatim and Oracle raises ORA-00904.
2. A lowercase key does not collide with the ``autoUpdateTime`` injector only
   because ``SkipHooks`` disables that injector. Its dedup check tests
   ``value[field.Name]`` / ``value[field.DBName]`` -- ``ModifiedAt`` /
   ``MODIFIED_AT`` on Oracle -- which a lowercase key matches neither of. Drop
   ``SkipHooks`` and Oracle gets a duplicate assignment to the same column
   (ORA-00957) while PostgreSQL stays green.

``AssignmentMap`` uppercases on Oracle, which both hits ``FieldsByDBName``
directly and satisfies the dedup check, making the behaviour independent of
GORM internals rather than contingent on them.

``Assign``/``Attrs`` remain unflagged: they feed FirstOrCreate-class calls whose
keys go through the schema, and no site in this repo uses them with a raw map.

Exemption: put ``//oracle-map-key:exempt <reason>`` on the line immediately
above the call. Used sparingly -- the point is that these are hazardous by
default.

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

# GORM methods that accept inline conditions (WHERE-class). These require
# ColumnMap. Updates is handled separately below because it requires a different
# helper (AssignmentMap) and can take its payload as a variable.
CONDITION_METHODS = "Where|Or|Not|First|Last|Take|Find|Delete"

# An exemption marker on the line immediately above the offending call.
EXEMPT = re.compile(r"//\s*oracle-map-key:exempt\b")

# `.Updates(` taking either an inline map literal or a bare identifier. The
# identifier form is the common one in this repo -- the payload is built up over
# several lines and then passed by name -- and is only reported when that
# identifier is assigned a map literal somewhere in the same file, which keeps
# `Updates(someStruct)` (schema-resolved, safe) from being flagged.
# The `\s*` after the dot is load-bearing: these calls are method-chain
# continuations, so the dot terminates the PREVIOUS line and `Updates(` opens
# the next one. A same-line-only `\.Updates\(` misses every multi-line chain --
# the identical blind spot recorded for the condition-method regex in issue #503
# note 3 -- and it hides all three sites #615 actually names.
UPDATES_CALL = re.compile(r"\.\s*Updates\(\s*(map\[string\](?:any|interface\{\})\{|([A-Za-z_]\w*)\s*\))")
MAP_ASSIGN = re.compile(r"\b(\w+)\s*:?=\s*map\[string\](?:any|interface\{\})\{")

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



# Timestamp columns GORM manages via autoUpdateTime/autoCreateTime. A map key
# naming one of these is what collides with the injector.
MANAGED_TIMESTAMPS = ("modified_at", "created_at")


def _match_brace(s: str, i: int) -> int:
    """Index just past the ``}`` closing the literal whose ``{`` is at i."""
    depth = 0
    while i < len(s):
        ch = s[i]
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return i + 1
        elif ch == '"':
            i += 1
            while i < len(s) and s[i] != '"':
                i += 2 if s[i] == "\\" else 1
        i += 1
    return -1


def _payload_sets_managed_timestamp(content: str, m: re.Match, ident: str | None) -> bool:
    """Whether this Updates payload assigns an autoUpdateTime-managed column."""
    if ident:
        am = re.search(r"\b" + re.escape(ident) + r"\s*:?=\s*map\[string\](?:any|interface\{\})\{", content)
        if not am:
            return False
        body = content[am.end() - 1:_match_brace(content, am.end() - 1)]
    else:
        brace = content.find("{", m.start())
        if brace < 0:
            return False
        body = content[brace:_match_brace(content, brace)]
    return any(f'"{ts}"' in body for ts in MANAGED_TIMESTAMPS)


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
    update_violations: list[str] = []
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

        # --- Updates(...) payloads: require AssignmentMap (#615) ---
        #
        # Scoped to payloads that set an autoUpdateTime-managed timestamp. That
        # is where the hazard actually bites, and it is a real failure on
        # Oracle rather than a latent one:
        #
        #   * Model(&X{}) without SkipHooks -- GORM's injector dedups on
        #     value[field.Name] / value[field.DBName] ("ModifiedAt" /
        #     "MODIFIED_AT" on Oracle), which a lowercase "modified_at" matches
        #     neither of, so it appends a SECOND assignment to the same column:
        #     ORA-00957.
        #   * Table("x") -- stmt.Schema is nil, so the key never reaches the
        #     namer and is emitted verbatim as quoted lowercase: ORA-00904.
        #
        # Ordinary column keys carry only the pre-existing namer-fallback
        # version dependency, which predates this check and is tracked
        # separately; flagging them here would bury the live hazard in noise.
        lines = content.splitlines()
        map_vars = {m.group(1) for m in MAP_ASSIGN.finditer(content)}
        for m in UPDATES_CALL.finditer(content):
            ident = m.group(2)
            if not _payload_sets_managed_timestamp(content, m, ident):
                continue
            # Bare identifier that is not a known map variable -> struct payload,
            # which resolves through the schema and is safe.
            if ident is not None and ident not in map_vars:
                continue
            lineno = content.count("\n", 0, m.start()) + 1
            if "AssignmentMap(" in content[m.start():m.start() + 160]:
                continue
            if lineno >= 2 and EXEMPT.search(lines[lineno - 2]):
                continue
            snippet = lines[lineno - 1].strip()[:80]
            update_violations.append(f"{go_file.relative_to(project_root)}:{lineno}: {snippet}")

    if update_violations:
        log_error("Found GORM map-keyed Updates() payloads not routed through AssignmentMap:")
        for v in update_violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            "Wrap the assignment map in AssignmentMap(<db>.Name(), ...) "
            "(api/dialect_helpers.go). A lowercase key only works by way of a GORM "
            "namer fallback (a version dependency), and it silently fails the "
            "autoUpdateTime dedup check -- so removing Session{SkipHooks:true} would "
            "emit a duplicate column assignment and raise ORA-00957 on Oracle while "
            "PostgreSQL stays green. See issue #615.",
            file=sys.stderr,
        )

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

    if violations or update_violations:
        return 1

    log_success("All GORM map-keyed predicates route through ColumnMap/AssignmentMap")
    return 0


if __name__ == "__main__":
    sys.exit(main())
