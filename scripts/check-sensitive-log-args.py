#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that no Go code passes sensitive config/credential fields to logger calls.

TMI's log redaction (internal/slogging/redaction.go) is attribute-based: it
matches slog.Attr KEYS. A secret interpolated directly into a format string —
e.g. logger.Info("key=%s", cfg.LLMAPIKey) — bypasses redaction entirely and
lands in the log in cleartext. This check is the guardrail for that gap
(issue #540): it fails the build when a known sensitive field is passed as an
argument to a logging call.

The check scans non-test Go files under api/, auth/, cmd/, internal/, and
pkg/. For every logger-style call (.Debug/.Info/.Warn/.Error and the f/Print
variants) it extracts the full balanced-paren argument span (multi-line calls
included), strips string literals, and flags any field access whose name is in
the sensitive catalog below.

Legitimate needs to reference a sensitive field near logging (e.g. logging
len(x.Password) or x.Password != "") should be rewritten to a boolean/derived
value BEFORE the log call; if that is genuinely impossible, add the file to
EXCEPTIONS with a justification in the commit message.

Usage:
    uv run scripts/check-sensitive-log-args.py
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

# Sensitive field identifiers (matched as `.Name` field accesses inside logger
# call arguments). Sourced from the config structs (internal/config/config.go)
# and the credential models. Word-bounded, so e.g. `.PasswordSet` or
# `.SPPrivateKeyPath` (a path, not the key material) do NOT match.
SENSITIVE_FIELDS = [
    "Password",
    "LLMAPIKey",
    "TextEmbeddingAPIKey",
    "RerankAPIKey",
    "APIKey",
    "ClientSecret",
    "Secret",
    "SPPrivateKey",
    "WebhookSecret",
    "ContentTokenEncryptionKey",
    "AccessToken",
    "RefreshToken",
]

# Logger-style method names whose argument spans are inspected. The Print/Fatal
# variants are included defensively even though the standard log package is
# banned in TMI (see CLAUDE.md Logging Requirements).
LOG_METHODS = (
    "Debug",
    "Debugf",
    "Info",
    "Infof",
    "Warn",
    "Warnf",
    "Error",
    "Errorf",
    "Print",
    "Printf",
    "Println",
    "Fatal",
    "Fatalf",
)

# Project-root-relative files allowed to reference sensitive fields inside
# logger calls. Empty on purpose: new violations should be fixed by deriving a
# safe value (bool/length/fingerprint) before logging, not by exceptions.
EXCEPTIONS: set[str] = set()

SCAN_DIRS = ("api", "auth", "cmd", "internal", "pkg")

CALL_RE = re.compile(r"\.(?:" + "|".join(LOG_METHODS) + r")\(")
FIELD_RE = re.compile(r"\.(?:" + "|".join(SENSITIVE_FIELDS) + r")\b")
# len(x.Secret) is a safe derived value (length is not secret material); such
# spans are blanked before the sensitive-field search.
LEN_RE = re.compile(r"\blen\(\s*[A-Za-z_][\w.]*\.(?:" + "|".join(SENSITIVE_FIELDS) + r")\b\s*\)")


def strip_comments(src: str) -> str:
    """Blank out // and /* */ comments, preserving newlines and string contents."""
    out: list[str] = []
    i, n = 0, len(src)
    while i < n:
        ch = src[i]
        if ch == "/" and i + 1 < n and src[i + 1] == "/":
            j = src.find("\n", i)
            j = n if j == -1 else j
            out.append(" " * (j - i))
            i = j
        elif ch == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            j = n if j == -1 else j + 2
            out.append("".join(c if c == "\n" else " " for c in src[i:j]))
            i = j
        elif ch in ('"', "'", "`"):
            j = skip_string(src, i)
            out.append(src[i:j])
            i = j
        else:
            out.append(ch)
            i += 1
    return "".join(out)


def skip_string(src: str, i: int) -> int:
    """Return the index just past the Go string/rune literal starting at i."""
    quote = src[i]
    j = i + 1
    n = len(src)
    while j < n:
        if quote != "`" and src[j] == "\\":
            j += 2
            continue
        if src[j] == quote:
            return j + 1
        j += 1
    return n


def blank_strings(span: str) -> str:
    """Blank out string/rune literal contents in a span (keep the quotes)."""
    out: list[str] = []
    i, n = 0, len(span)
    while i < n:
        ch = span[i]
        if ch in ('"', "'", "`"):
            j = skip_string(span, i)
            out.append(ch + " " * max(0, j - i - 2) + (span[j - 1] if j - i >= 2 else ""))
            i = j
        else:
            out.append(ch)
            i += 1
    return "".join(out)


def arg_span(src: str, open_paren: int) -> tuple[int, int]:
    """Given the index of '(' in comment-stripped source, return (start, end)
    of the balanced argument span (exclusive of the parens)."""
    depth = 0
    i, n = open_paren, len(src)
    while i < n:
        ch = src[i]
        if ch in ('"', "'", "`"):
            i = skip_string(src, i)
            continue
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
            if depth == 0:
                return open_paren + 1, i
        i += 1
    return open_paren + 1, n


def check_file(go_file: Path, project_root: Path) -> list[str]:
    src = strip_comments(go_file.read_text(encoding="utf-8"))
    rel = go_file.relative_to(project_root).as_posix()
    violations: list[str] = []
    for m in CALL_RE.finditer(src):
        start, end = arg_span(src, m.end() - 1)
        args = blank_strings(src[start:end])
        args = LEN_RE.sub(lambda m: " " * len(m.group(0)), args)
        hit = FIELD_RE.search(args)
        if hit:
            lineno = src.count("\n", 0, start + hit.start()) + 1
            violations.append(f"{rel}:{lineno}: sensitive field `{hit.group(0)}` passed to logger call")
    return violations


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Check that no Go code passes sensitive config/credential fields "
            "as logger call arguments (attribute-based redaction cannot catch "
            "format-string interpolation; see issue #540)."
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
        log_error(f"No Go files found under {project_root} scan dirs {SCAN_DIRS}")
        return 1

    log_info("Checking for sensitive fields passed to logger calls...")

    violations: list[str] = []
    for go_file in go_files:
        rel_path = go_file.relative_to(project_root).as_posix()
        if rel_path in EXCEPTIONS or go_file.name == "api.go":
            continue
        violations.extend(check_file(go_file, project_root))

    if violations:
        log_error("Found sensitive fields passed to logger calls:")
        for v in violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            "Log redaction is attribute-based and cannot redact secrets interpolated "
            "into format strings. Log a derived value instead (bool presence, length, "
            "or fingerprint) — never the secret itself. See issue #540.",
            file=sys.stderr,
        )
        return 1

    log_success("No sensitive fields passed to logger calls")
    return 0


if __name__ == "__main__":
    sys.exit(main())
