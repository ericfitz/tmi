#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that c.JSON(non-2xx, ...) calls in production Go code are followed by
c.Abort() or return.

Background: HandleRequestError in api/request_utils.go was missing c.Abort()
after writing error responses. This let downstream handlers continue executing
and overwrite the HTTP status code, causing 4xx/5xx responses to be logged as
200 by the request tracing middleware. See issue #264.

This check enforces the rule that c.JSON(<error_status>, ...) MUST be followed
by either:
  1. c.Abort() within the next ~5 lines (preferred for shared helpers and
     middleware — stops the chain unconditionally), OR
  2. A bare `return` within the next ~5 lines (acceptable in terminal route
     handlers where no further code in the same function runs)

Usage:
    uv run scripts/check-missing-abort.py
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

JSON_STATUS = re.compile(r"\bc\.JSON\(\s*([^,]+),")

SUCCESS_STATUS = {
    "http.StatusOK",
    "http.StatusCreated",
    "http.StatusAccepted",
    "http.StatusNoContent",
    "http.StatusPartialContent",
    "http.StatusResetContent",
    "http.StatusNonAuthoritativeInfo",
    "http.StatusMultiStatus",
    "200",
    "201",
    "202",
    "204",
    "206",
}

# Skip generated and test files
SKIP_FILE_SUFFIXES = ("_test.go",)
SKIP_FILES = {"api.go"}

ROOTS = ("api", "auth")

LOOKAHEAD_LINES = 6


def find_balanced_close(lines: list[str], start_line_idx: int, start_col: int) -> int:
    """Find the line containing the matching ')' for an opening '(' at start position."""
    depth = 0
    i = start_line_idx
    j = start_col
    started = False
    while i < len(lines):
        line = lines[i]
        while j < len(line):
            ch = line[j]
            if ch == "(":
                depth += 1
                started = True
            elif ch == ")":
                depth -= 1
                if started and depth == 0:
                    return i
            j += 1
        i += 1
        j = 0
    return start_line_idx


def is_terminated(lines: list[str], call_line: int, after_line: int) -> bool:
    """Return True if c.JSON at `call_line` (with closing paren on `after_line - 1`)
    is properly terminated by c.Abort() or return.

    Handles common Go control-flow patterns:
      - Direct c.Abort() / return on next code line.
      - End-of-block: next code line at outer indent is `}`, `case`, `default`,
        another `return`, or another `c.Abort()` — safe (Go switches don't fall
        through; if/else branches commonly share a single trailing return).
    """
    call_indent = len(lines[call_line]) - len(lines[call_line].lstrip())

    # Walk forward from after_line looking at substantive code lines.
    for idx in range(after_line, min(after_line + 12, len(lines))):
        line = lines[idx]
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        line_indent = len(line) - len(line.lstrip())

        # Lines at deeper indent than the call are part of a separate inner
        # construct that runs only conditionally — keep scanning.
        if line_indent > call_indent:
            # but: c.Abort()/return at any indent within window is fine
            if "c.Abort()" in stripped or "AbortWith" in stripped:
                return True
            if re.match(r"^return(\s|$)", stripped):
                return True
            continue

        # At call_indent or shallower
        if "c.Abort()" in stripped or "AbortWith" in stripped:
            return True
        if re.match(r"^return(\s|$)", stripped):
            return True
        # Block-end markers at outer indent imply control transfers out
        if stripped == "}" or stripped.startswith("}"):
            return True
        # Switch-case boundaries (no fall-through in Go)
        if stripped.startswith("case ") or stripped.startswith("default"):
            return True
        # Substantive code follows at same indent → not terminated
        return False

    # Reached end of window or file
    return True


def main() -> int:
    project_root = get_project_root()
    log_info("Checking for c.JSON(error) calls missing c.Abort()/return...")

    violations: list[str] = []

    for root in ROOTS:
        root_dir = project_root / root
        if not root_dir.is_dir():
            continue
        for go_file in sorted(root_dir.rglob("*.go")):
            name = go_file.name
            if name in SKIP_FILES:
                continue
            if any(name.endswith(s) for s in SKIP_FILE_SUFFIXES):
                continue

            text = go_file.read_text(encoding="utf-8")
            lines = text.split("\n")
            for i, line in enumerate(lines):
                m = JSON_STATUS.search(line)
                if not m:
                    continue
                status = m.group(1).strip()
                if status in SUCCESS_STATUS:
                    continue
                # Skip variable status — cannot statically classify; helpers
                # should still abort but that's a separate review task.
                if not status.startswith("http.Status") and not status.isdigit():
                    continue
                # Locate closing ')' to know where the call ends
                col = m.start()
                op = line.find("(", col)
                cl = find_balanced_close(lines, i, op)
                if not is_terminated(lines, i, cl + 1):
                    rel = go_file.relative_to(project_root)
                    violations.append(
                        f"{rel}:{i + 1}: c.JSON({status}, ...) not followed by c.Abort() or return"
                    )

    if violations:
        log_error("Found c.JSON(error) calls missing c.Abort()/return:")
        for v in violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            "Add c.Abort() (preferred for shared helpers/middleware) or `return` "
            "(acceptable in terminal handlers) immediately after the c.JSON call.",
            file=sys.stderr,
        )
        print(
            "See issue #264 for background on why missing aborts cause 4xx/5xx "
            "responses to be logged as 200.",
            file=sys.stderr,
        )
        return 1

    log_success("No missing c.Abort()/return after error responses")
    return 0


if __name__ == "__main__":
    sys.exit(main())
