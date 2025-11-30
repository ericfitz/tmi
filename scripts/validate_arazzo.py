#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",
# ]
# ///

"""
Validate Arazzo specification using Spectral.

This script runs Spectral validation on the generated Arazzo files
and reports any errors or warnings.
"""

import subprocess
import sys
from pathlib import Path


def validate_arazzo(arazzo_path: str, format: str = 'stylish') -> bool:
    """
    Run Spectral validation on Arazzo file.

    Args:
        arazzo_path: Path to Arazzo specification file
        format: Output format (stylish, json, junit, html, text, teamcity, pretty)

    Returns:
        True if validation passes, False otherwise
    """
    arazzo_file = Path(arazzo_path)

    if not arazzo_file.exists():
        print(f"❌ File not found: {arazzo_path}")
        return False

    print(f"🔍 Validating Arazzo specification: {arazzo_path}")
    print(f"   Format: {format}")
    print()

    try:
        result = subprocess.run(
            [
                'npx',
                '@stoplight/spectral-cli',
                'lint',
                str(arazzo_file),
                '--format',
                format,
            ],
            capture_output=True,
            text=True,
        )

        # Print output
        if result.stdout:
            print(result.stdout)

        if result.stderr and result.returncode != 0:
            print(result.stderr, file=sys.stderr)

        if result.returncode == 0:
            print("\n✅ Arazzo validation passed")
            return True
        else:
            print("\n❌ Arazzo validation failed")
            print(f"   Exit code: {result.returncode}")
            return False

    except FileNotFoundError:
        print("❌ Spectral CLI not found")
        print("   Install with: npm install")
        return False
    except Exception as e:
        print(f"❌ Validation error: {e}")
        return False


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description='Validate Arazzo specifications')
    parser.add_argument(
        'files',
        nargs='*',
        default=['docs/reference/apis/tmi.arazzo.yaml'],
        help='Arazzo files to validate (default: tmi.arazzo.yaml)',
    )
    parser.add_argument(
        '--format',
        '-f',
        default='stylish',
        choices=['stylish', 'json', 'junit', 'html', 'text', 'teamcity', 'pretty'],
        help='Output format (default: stylish)',
    )

    args = parser.parse_args()

    # Validate all specified files
    all_passed = True
    for arazzo_file in args.files:
        if not validate_arazzo(arazzo_file, args.format):
            all_passed = False
        print()

    # Exit with appropriate code
    sys.exit(0 if all_passed else 1)
