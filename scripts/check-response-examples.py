#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Fail the build when a 2xx response `example` omits a property its schema declares.

CATS validates a 2xx response against the operation's declared `example`, not its
`schema` (Endava/cats#206). The response's property names must be a recursive
*subset* of the example's, so any declared property missing from an example is a
latent "Not matching response schema" finding -- it fires as soon as a fuzzer
gets a 2xx on that operation with the field populated.

That latency is what makes this worth a build gate rather than periodic cleanup.
The 2026-07-31 campaign turned 5 such gaps into 68 findings purely because
coverage shifted; 67 more were sitting unfired in the same spec. Without this
check the next optional property added to any schema silently reintroduces them.

Usage:
    uv run scripts/check-response-examples.py [--spec PATH]
"""

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from openapi_examples import SpecWalker, iter_response_examples  # noqa: E402
from tmi_common import (  # noqa: E402
    get_project_root,
    log_error,
    log_info,
    log_success,
)

SPEC_PATH = "api-schema/tmi-openapi.json"
BASELINE_PATH = "api-schema/response-example-baseline.json"


# SEM@746aa587013953beb72c3f38c9643442ca4cca0d: validate response examples cover declared schema properties against a baseline
def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Check that every 2xx response example covers all properties its "
            "schema declares (CATS compares responses to examples, not schemas)."
        )
    )
    parser.add_argument("--spec", default=None, help="path to the OpenAPI spec")
    parser.add_argument(
        "--baseline", default=None, help="path to the accepted-gap baseline"
    )
    parser.add_argument(
        "--update-baseline",
        action="store_true",
        help="rewrite the baseline from the current spec (use after fixing gaps)",
    )
    args = parser.parse_args()

    spec_path = Path(args.spec) if args.spec else get_project_root() / SPEC_PATH
    log_info(f"Checking response examples in {spec_path}")
    try:
        spec = json.loads(spec_path.read_text())
    except FileNotFoundError:
        log_error(f"Spec file not found: {spec_path}")
        return 1
    except json.JSONDecodeError as exc:
        log_error(f"Spec file contains invalid JSON: {exc}")
        return 1

    walker = SpecWalker(spec)
    gaps: list[tuple[str, str, str, str, list[str]]] = []
    checked = 0

    for path, method, code, _media, schema, example, source in iter_response_examples(
        spec
    ):
        checked += 1
        missing = walker.missing_paths(schema, example)
        if missing:
            gaps.append((path, method, code, source, sorted(missing)))

    current = {f"{m.upper()} {p} -> {c}": miss for p, m, c, _s, miss in gaps}

    baseline_path = (
        Path(args.baseline) if args.baseline else get_project_root() / BASELINE_PATH
    )
    if args.update_baseline:
        payload = {
            "_comment": (
                "Accepted response-example gaps. CATS compares a 2xx response to "
                "the operation's example, not its schema (Endava/cats#206), so a "
                "property the schema declares but the example omits becomes a "
                "finding once the server returns it. Every entry here was checked "
                "against a 107k-response corpus and never observed, so none fires "
                "today. They are still debt: some are simply unexercised, and some "
                "mean the schema declares more than the server returns (the "
                "Project.team divergence). Tracked in #659. This file is a ratchet "
                "-- it may shrink, never grow, and a new gap fails the build."
            ),
            "gaps": {k: sorted(v) for k, v in sorted(current.items())},
        }
        baseline_path.write_text(json.dumps(payload, indent=2) + "\n")
        log_success(
            f"Baseline updated: {len(current)} operation(s), "
            f"{sum(len(v) for v in current.values())} accepted gap(s)"
        )
        return 0

    try:
        accepted = json.loads(baseline_path.read_text()).get("gaps", {})
    except FileNotFoundError:
        accepted = {}

    new_gaps = {
        key: [m for m in miss if m not in accepted.get(key, [])]
        for key, miss in current.items()
    }
    new_gaps = {k: v for k, v in new_gaps.items() if v}

    # A baseline entry that no longer reproduces must be removed, or the file
    # silently re-accumulates debt it is supposed to be retiring.
    stale = {
        key: [m for m in miss if m not in current.get(key, [])]
        for key, miss in accepted.items()
    }
    stale = {k: v for k, v in stale.items() if v}

    if new_gaps:
        total = sum(len(v) for v in new_gaps.values())
        log_error(
            f"{len(new_gaps)} response example(s) omit {total} property/properties "
            "their schema declares:"
        )
        for key, missing in sorted(new_gaps.items()):
            print(f"  {key}", file=sys.stderr)
            print(f"         missing: {', '.join(sorted(missing))}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            "CATS compares a 2xx response against the example, not the schema "
            "(Endava/cats#206),",
            file=sys.stderr,
        )
        print(
            "so each omission becomes a 'Not matching response schema' finding once "
            "that operation",
            file=sys.stderr,
        )
        print(
            "returns 2xx with the property populated. Add the property to the "
            "example (preferred),",
            file=sys.stderr,
        )
        print(
            "or, if the server genuinely never returns it, record it with "
            "--update-baseline.",
            file=sys.stderr,
        )
        return 1

    if stale:
        total = sum(len(v) for v in stale.values())
        log_error(
            f"Baseline lists {total} gap(s) that no longer reproduce. "
            "Retire them so the ratchet cannot loosen:"
        )
        for key, missing in sorted(stale.items()):
            print(f"  {key}: {', '.join(sorted(missing))}", file=sys.stderr)
        print("\n    uv run scripts/check-response-examples.py --update-baseline",
              file=sys.stderr)
        return 1

    accepted_total = sum(len(v) for v in accepted.values())
    log_success(
        f"All {checked} 2xx response examples cover their declared properties "
        f"({accepted_total} baselined gap(s) unchanged)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
