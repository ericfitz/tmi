#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""
Remove redundant headers from responses that use $ref.

When a response uses $ref to a component (e.g., #/components/responses/TooManyRequests),
any headers defined locally are redundant if they're already in the referenced component.
This script removes such redundancies.
"""

import json
import sys
from pathlib import Path
from datetime import datetime
from typing import TypedDict


class ProcessStats(TypedDict):
    """Type definition for process statistics."""
    total_ref_responses: int
    responses_with_local_headers: int
    responses_cleaned: int
    headers_removed: int
    endpoints_modified: list[str]

OPENAPI_PATH = Path(__file__).parent.parent / "docs/reference/apis/tmi-openapi.json"


def get_component_headers(spec: dict, ref: str) -> set[str]:
    """
    Extract header names from a component reference.

    Args:
        spec: The OpenAPI specification
        ref: Reference string like "#/components/responses/TooManyRequests"

    Returns:
        Set of header names defined in the component
    """
    if not ref.startswith("#/components/responses/"):
        return set()

    component_name = ref.split("/")[-1]

    if "components" not in spec:
        return set()

    if "responses" not in spec["components"]:
        return set()

    component = spec["components"]["responses"].get(component_name)
    if not component:
        return set()

    headers = component.get("headers", {})
    return set(headers.keys())


def clean_redundant_headers(response: dict, component_headers: set[str]) -> tuple[bool, list[str]]:
    """
    Remove headers from response that are already in the referenced component.

    Returns:
        (modified, removed_headers) - True if modified, plus list of removed header names
    """
    if "headers" not in response:
        return False, []

    local_headers = response["headers"]
    removed_headers = []

    # Find headers that are redundant (exist in component)
    for header_name in list(local_headers.keys()):
        if header_name in component_headers:
            del local_headers[header_name]
            removed_headers.append(header_name)

    # If headers dict is now empty, remove it entirely
    if not local_headers:
        del response["headers"]

    return len(removed_headers) > 0, removed_headers


def process_openapi_spec(spec: dict) -> ProcessStats:
    """
    Process the OpenAPI spec and remove redundant headers from $ref responses.

    Returns:
        Dictionary with statistics about the operation
    """
    stats: ProcessStats = {
        "total_ref_responses": 0,
        "responses_with_local_headers": 0,
        "responses_cleaned": 0,
        "headers_removed": 0,
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

            # Check all response codes, not just 429
            for status_code, response in responses.items():
                if not isinstance(response, dict):
                    continue

                # Only process responses with $ref
                if "$ref" not in response:
                    continue

                stats["total_ref_responses"] += 1

                # Check if response has local headers
                if "headers" not in response:
                    continue

                stats["responses_with_local_headers"] += 1

                # Get headers from referenced component
                component_headers = get_component_headers(spec, response["$ref"])

                # Clean redundant headers
                modified, removed = clean_redundant_headers(response, component_headers)

                if modified:
                    stats["responses_cleaned"] += 1
                    stats["headers_removed"] += len(removed)
                    endpoint_info = f"{method.upper()} {path} [{status_code}] - removed: {', '.join(removed)}"
                    stats["endpoints_modified"].append(endpoint_info)

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
    print("\nRemoving redundant headers from $ref responses...")
    stats = process_openapi_spec(spec)

    # Save modified spec
    with open(OPENAPI_PATH, 'w') as f:
        json.dump(spec, f, indent=2)
        f.write('\n')  # Add trailing newline

    # Print results
    print("\n" + "="*70)
    print("RESULTS")
    print("="*70)
    print(f"Total responses with $ref:              {stats['total_ref_responses']}")
    print(f"Responses with local headers:            {stats['responses_with_local_headers']}")
    print(f"Responses cleaned:                       {stats['responses_cleaned']}")
    print(f"Total headers removed:                   {stats['headers_removed']}")

    if stats['endpoints_modified']:
        print(f"\nEndpoints modified: {len(stats['endpoints_modified'])}")
        print("\nModified endpoints:")
        for endpoint in sorted(stats['endpoints_modified'])[:20]:  # Show first 20
            print(f"  - {endpoint}")

        if len(stats['endpoints_modified']) > 20:
            print(f"  ... and {len(stats['endpoints_modified']) - 20} more")

    print(f"\n✓ Updated OpenAPI spec: {OPENAPI_PATH}")
    print(f"✓ Backup saved to: {backup_path}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
