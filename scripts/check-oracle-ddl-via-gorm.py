#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that no DDL statement is executed through gorm's Exec/Raw (Oracle-unsafe).

TMI opens gorm with PrepareStmt: true (auth/db/gorm.go). Under that setting a
gorm.DB.Exec(<DDL>) works the FIRST time and silently does nothing, returning
nil, on any later call with a byte-identical string on the same *gorm.DB:
gorm's PreparedStmtDB re-executes the cached statement handle, and Oracle
performs DDL during the PARSE phase, so a re-execute has nothing to do and
reports success. Session{PrepareStmt: false} does not bypass it -- the cache
is on the pool-level ConnPool. The no-op is not detectable from the error.
See issue #763 (root-caused in #762).

The rule: anything Oracle treats as DDL (CREATE / DROP / ALTER / TRUNCATE /
RENAME / GRANT / REVOKE / COMMENT / PURGE / ANALYZE / AUDIT / FLASHBACK ...)
must go through
internal/dbschema.execMigrationDDL (pinned *sql.Conn; exported as
dbschema.ExecDDL for callers outside the package). DML and PL/SQL blocks are
unaffected and are not flagged.

Detection is per file and deliberately simple: collect every Go string
literal (interpreted or raw, including fmt.Sprintf format strings) whose
leading keyword is DDL, remember which identifier it was assigned to (var x =
..., x := ..., x = ..., or an element of a []string literal assigned to x),
or a `for _, y := range x` alias of it), then flag every `.Exec(` / `.Raw(`
call whose first argument is such a literal or such an identifier. Names are
tracked per top-level function. execMigrationDDL itself (oracle_ddl.go) is
the one sanctioned site and is skipped.

A dialect-guarded statement that can never reach Oracle (PostgreSQL-only
CREATE SEQUENCE / ALTER ROLE) is suppressed with an inline marker on the Exec
line: `// ddl-via-gorm:ok <reason>`.

LIMITATION: a DDL string that reaches .Exec() through a function parameter,
struct field, package-level const, or a variable assigned in another
function is not seen. Today every
DDL literal in the tree sits in the same function as its Exec, so this is a
regression guard for the existing shape, not a proof.

Usage:
    uv run scripts/check-oracle-ddl-via-gorm.py
"""

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

SCAN_DIRS = ("api", "auth", "cmd", "internal", "test")

# The sanctioned executor. Everything else is a finding.
ALLOW_FILES = {"internal/dbschema/oracle_ddl.go"}

# Oracle's implicit-commit DDL verbs (plus ALTER SESSION, which the ALTER
# prefix also catches: session control, not DDL, but it must not land on an
# arbitrary pooled session either).
DDL_KEYWORD = re.compile(
    r"^\s*(CREATE|DROP|ALTER|TRUNCATE|RENAME|GRANT|REVOKE|COMMENT|PURGE|ANALYZE|AUDIT|NOAUDIT|FLASHBACK|ASSOCIATE|DISASSOCIATE)\b",
    re.IGNORECASE,
)

# Go string literals: raw `...` (may span lines) or interpreted "..." (single line).
STRING_LIT = re.compile(r'`[^`]*`|"(?:[^"\\\n]|\\.)*"')

# Which identifier a literal is bound to: `name := <lit>`, `name = <lit>`,
# `var name = <lit>`, `name := fmt.Sprintf(<lit>`, `name := []string{ <lit>, ...`.
ASSIGN = re.compile(r"\b(?:var\s+)?([A-Za-z_]\w*)\s*(?::=|=)\s*(?:\[\]string\s*\{|fmt\.Sprintf\(|strings\.\w+\()?\s*$")

# First argument: a literal, an inline fmt.Sprintf(<literal>, or a bare name.
EXEC_CALL = re.compile(
    r"\.(Exec|Raw)\(\s*(?:fmt\.Sprintf\(\s*)?(`[^`]*`|\"(?:[^\"\\\n]|\\.)*\"|[A-Za-z_]\w*)"
)

RANGE_ALIAS = re.compile(r"\bfor\s+(?:_|[A-Za-z_]\w*)\s*,\s*([A-Za-z_]\w*)\s*:=\s*range\s+([A-Za-z_]\w*)")

SUPPRESS = "// ddl-via-gorm:ok"

FUNC_SPLIT = re.compile(r"^(?=func\b)", re.MULTILINE)


def is_ddl(lit: str) -> bool:
    return bool(DDL_KEYWORD.match(lit.strip("`\"")))


def scan_chunk(chunk: str, rel: str, base_line: int) -> list[str]:
    findings: list[str] = []

    # 1. Names bound to DDL literals. For each DDL literal look back to the
    #    nearest preceding assignment head within the same statement, or the
    #    `name := []string{` opener a few lines up for slice elements.
    ddl_names: set[str] = set()
    for m in STRING_LIT.finditer(chunk):
        if not is_ddl(m.group(0)):
            continue
        head = chunk[: m.start()]
        lines = head.split("\n")
        candidates = [lines[-1]] + lines[-41:-1][::-1]
        for line in candidates:
            am = ASSIGN.search(line)
            if am:
                ddl_names.add(am.group(1))
                break
            stripped = line.strip()
            if stripped.endswith("}") or stripped.endswith(";"):
                break

    # 1b. Loop aliases over a DDL-bound slice: for _, sql := range statements.
    for am in RANGE_ALIAS.finditer(chunk):
        if am.group(2) in ddl_names:
            ddl_names.add(am.group(1))

    # 2. Exec/Raw calls with a DDL literal or a DDL-bound name.
    for offset, line in enumerate(chunk.splitlines()):
        if SUPPRESS in line:
            continue
        lineno = base_line + offset
        for cm in EXEC_CALL.finditer(line):
            arg = cm.group(2)
            if arg.startswith(("`", '"')):
                if is_ddl(arg):
                    findings.append(f"{rel}:{lineno}: DDL literal passed to .{cm.group(1)}()")
            elif arg in ddl_names:
                findings.append(f"{rel}:{lineno}: DDL-bound variable {arg!r} passed to .{cm.group(1)}()")
    return findings


def scan_file(path: Path, rel: str) -> list[str]:
    src = path.read_text(encoding="utf-8", errors="replace")
    findings: list[str] = []
    line = 1
    for chunk in FUNC_SPLIT.split(src):
        findings.extend(scan_chunk(chunk, rel, line))
        line += chunk.count("\n")
    return findings


def main() -> int:
    root = get_project_root()
    log_info("Checking that DDL never goes through gorm Exec/Raw (Oracle prepared-statement no-op, #763)...")
    findings: list[str] = []
    for d in SCAN_DIRS:
        for path in sorted((root / d).rglob("*.go")):
            rel = path.relative_to(root).as_posix()
            if rel in ALLOW_FILES or rel.endswith("_test.go"):
                continue
            findings.extend(scan_file(path, rel))

    if findings:
        for f in findings:
            log_error(f)
        log_error(
            f"{len(findings)} DDL statement(s) executed through gorm. Route them through "
            "dbschema.execMigrationDDL / dbschema.ExecDDL (pinned *sql.Conn); see #763."
        )
        return 1
    log_success("No DDL executed through gorm Exec/Raw")
    return 0


if __name__ == "__main__":
    sys.exit(main())
