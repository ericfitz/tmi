#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""
Add Retry-After headers to all 429 responses in the OpenAPI specification.

This script ensures RFC 6585 compliance by adding the Retry-After header to
all 429 (Too Many Requests) responses that don't already have it.
"""

import json
import sys
from pathlib import Path
from datetime import datetime

OPENAPI_PATH = Path(__file__).parent.parent / "docs/reference/apis/tmi-openapi.json"

RETRY_AFTER_HEADER = {
    "Retry-After": {
        "description": "Number of seconds until the rate limit resets",
        "schema": {
            "type": "integer",
            "example": 60
        }
    }
}


def add_retry_after_to_response(response: dict) -> tuple[bool, str]:
    """
    Add Retry-After header to a 429 response if missing.

    Returns:
        (modified, reason) - True if modified, plus description of action
    """
    if "headers" not in response:
        response["headers"] = {}

    headers = response["headers"]

    if "Retry-After" in headers:
        return False, "already has Retry-After"

    # Add Retry-After header
    headers.update(RETRY_AFTER_HEADER)
    return True, "added Retry-After"


def process_openapi_spec(spec: dict) -> dict:
    """
    Process the OpenAPI spec and add Retry-After headers to all 429 responses.

    Returns:
        Dictionary with statistics about the operation
    """
    stats = {
        "total_429_responses": 0,
        "already_had_retry_after": 0,
        "added_retry_after": 0,
        "endpoints_modified": []
    }

    if "paths" not in spec:
        print("ERROR: No 'paths' found in OpenAPI spec", file=sys.stderr)
        return stats

    # Iterate through all paths and operations
    for path, path_item in spec["paths"].items():
        for method, operation in path_item.items():
            if method.startswith("x-"):  # Skip extension properties
                continue

            if not isinstance(operation, dict):
                continue

            if "responses" not in operation:
                continue

            responses = operation["responses"]

            if "429" not in responses:
                continue

            stats["total_429_responses"] += 1

            response_429 = responses["429"]
            modified, reason = add_retry_after_to_response(response_429)

            if modified:
                stats["added_retry_after"] += 1
                stats["endpoints_modified"].append(f"{method.upper()} {path}")
            else:
                stats["already_had_retry_after"] += 1

    return stats


def main():
    print(f"Processing: {OPENAPI_PATH}")

    # Create backup
    backup_path = OPENAPI_PATH.with_suffix(f".json.{datetime.now().strftime('%Y%m%d_%H%M%S')}.backup")
    print(f"Creating backup: {backup_path}")

    # Load OpenAPI spec
    with open(OPENAPI_PATH, 'r') as f:
        spec = json.load(f)

    # Backup original
    with open(backup_path, 'w') as f:
        json.dump(spec, f, indent=2)

    # Process spec
    print("\nProcessing 429 responses...")
    stats = process_openapi_spec(spec)

    # Save modified spec
    with open(OPENAPI_PATH, 'w') as f:
        json.dump(spec, f, indent=2)
        f.write('\n')  # Add trailing newline

    # Print results
    print("\n" + "="*70)
    print("RESULTS")
    print("="*70)
    print(f"Total 429 responses found:           {stats['total_429_responses']}")
    print(f"Already had Retry-After header:      {stats['already_had_retry_after']}")
    print(f"Added Retry-After header:            {stats['added_retry_after']}")
    print(f"\nEndpoints modified: {len(stats['endpoints_modified'])}")

    if stats['endpoints_modified']:
        print("\nModified endpoints:")
        for endpoint in sorted(stats['endpoints_modified'])[:10]:  # Show first 10
            print(f"  - {endpoint}")

        if len(stats['endpoints_modified']) > 10:
            print(f"  ... and {len(stats['endpoints_modified']) - 10} more")

    print(f"\n✓ Updated OpenAPI spec: {OPENAPI_PATH}")
    print(f"✓ Backup saved to: {backup_path}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
