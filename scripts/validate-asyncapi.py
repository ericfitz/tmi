#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",
# ]
# ///

"""
Validate AsyncAPI specification using Spectral.

This script runs Spectral validation on AsyncAPI files
and reports any errors or warnings, following the same
pattern as Arazzo validation.
"""

import json
import subprocess
import sys
from pathlib import Path


def validate_asyncapi(
    asyncapi_path: str, format: str = "stylish", output_file: str | None = None
) -> bool:
    """
    Run Spectral validation on AsyncAPI file.

    Args:
        asyncapi_path: Path to AsyncAPI specification file
        format: Output format (stylish, json, junit, html, text, teamcity, pretty)
        output_file: Optional path to write JSON report

    Returns:
        True if validation passes, False otherwise
    """
    asyncapi_file = Path(asyncapi_path)

    if not asyncapi_file.exists():
        print(f"❌ File not found: {asyncapi_path}")
        return False

    print(f"🔍 Validating AsyncAPI specification: {asyncapi_path}")
    print(f"   Format: {format}")
    print()

    try:
        # Build command using asyncapi-specific ruleset
        cmd = [
            "npx",
            "@stoplight/spectral-cli",
            "lint",
            str(asyncapi_file),
            "--ruleset",
            "asyncapi-spectral.yaml",
            "--format",
            format,
        ]

        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
        )

        # Print output
        if result.stdout:
            print(result.stdout)

        if result.stderr and result.returncode != 0:
            print(result.stderr, file=sys.stderr)

        # Write JSON report if requested
        if output_file and format == "json":
            output_path = Path(output_file)
            output_path.parent.mkdir(parents=True, exist_ok=True)
            with open(output_path, "w", encoding="utf-8") as f:
                if result.stdout:
                    # Parse and re-write for consistent formatting
                    try:
                        data = json.loads(result.stdout)
                        json.dump(data, f, indent=2)
                    except json.JSONDecodeError:
                        f.write(result.stdout)
                else:
                    json.dump([], f, indent=2)
            print(f"\n📄 Report written to: {output_file}")

        if result.returncode == 0:
            print("\n✅ AsyncAPI validation passed")
            return True
        else:
            print("\n❌ AsyncAPI validation failed")
            print(f"   Exit code: {result.returncode}")
            return False

    except FileNotFoundError:
        print("❌ Spectral CLI not found")
        print("   Install with: pnpm install")
        return False
    except Exception as e:
        print(f"❌ Validation error: {e}")
        return False


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Validate AsyncAPI specifications")
    parser.add_argument(
        "files",
        nargs="*",
        default=["api-schema/tmi-asyncapi.yml"],
        help="AsyncAPI files to validate (default: tmi-asyncapi.yml)",
    )
    parser.add_argument(
        "--format",
        "-f",
        default="stylish",
        choices=["stylish", "json", "junit", "html", "text", "teamcity", "pretty"],
        help="Output format (default: stylish)",
    )
    parser.add_argument(
        "--output",
        "-o",
        default=None,
        help="Output file for JSON report (only used with --format=json)",
    )

    args = parser.parse_args()

    # Validate all specified files
    all_passed = True
    for asyncapi_file in args.files:
        if not validate_asyncapi(asyncapi_file, args.format, args.output):
            all_passed = False
        print()

    # Exit with appropriate code
    sys.exit(0 if all_passed else 1)
