#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# ///
"""Verify test/cats/false-positives.yaml rule-position comments.

Rule order is load-bearing (first match wins). Each rule's leading comment
must contain exactly one line `# rule N of M:` where N is the rule's actual
1-indexed file position and M is the total rule count. Any other
`rule N of M` string in the file is a stale leftover and fails the check.
"""

import re
import sys
from pathlib import Path

RULE_RE = re.compile(r"^  - id:")
NUM_RE = re.compile(r"#\s*rule (\d+) of (\d+)")


def main() -> int:
    path = Path(__file__).resolve().parent.parent / "test/cats/false-positives.yaml"
    lines = path.read_text().splitlines()
    rule_lines = [i for i, l in enumerate(lines) if RULE_RE.match(l)]
    total = len(rule_lines)
    errors: list[str] = []

    numbered_lines: set[int] = set()
    for pos, start in enumerate(rule_lines, 1):
        # leading comment block: contiguous comment/blank lines above the rule
        j = start - 1
        block: list[int] = []
        while j >= 0 and (lines[j].strip().startswith("#") or not lines[j].strip()):
            block.append(j)
            j -= 1
        found = []
        for k in block:
            m = NUM_RE.search(lines[k])
            if m:
                found.append((k, int(m.group(1)), int(m.group(2))))
                numbered_lines.add(k)
        if len(found) != 1:
            errors.append(
                f"line {start + 1}: rule {pos} has {len(found)} 'rule N of M' "
                f"comment lines (want exactly 1)"
            )
            continue
        k, n, m_ = found[0]
        if n != pos or m_ != total:
            errors.append(
                f"line {k + 1}: says 'rule {n} of {m_}', actual position is "
                f"rule {pos} of {total}"
            )

    # stray numbering lines outside any rule's leading block (e.g. header text)
    for i, l in enumerate(lines):
        if NUM_RE.search(l) and i not in numbered_lines:
            errors.append(f"line {i + 1}: stray 'rule N of M' text: {l.strip()}")

    if errors:
        print(f"check-cats-fp-numbering: {len(errors)} error(s) in {path.name}:")
        for e in errors:
            print(f"  {e}")
        return 1
    print(f"check-cats-fp-numbering: OK ({total} rules, numbering consistent)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
