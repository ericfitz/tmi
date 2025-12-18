#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""
Fix Spectral $ref sibling issues in TMI OpenAPI specification.

The issue: Rate limit headers were added alongside $ref properties, which violates
OpenAPI 3.0 spec. When using $ref, it must be the only property at that level.

Solution: Remove inline headers from responses that use $ref, and instead add
rate limit headers to the component response definitions.

Usage:
    uv run scripts/fix-openapi-spectral-issues.py
"""

import json
import shutil
from pathlib import Path
from typing import Any, Dict

OPENAPI_FILE = Path("docs/reference/apis/tmi-openapi.json")


def load_openapi() -> Dict[str, Any]:
    """Load the OpenAPI specification."""
    with open(OPENAPI_FILE, 'r') as f:
        return json.load(f)


def save_openapi(spec: Dict[str, Any]) -> None:
    """Save the OpenAPI specification with pretty formatting."""
    with open(OPENAPI_FILE, 'w') as f:
        json.dump(spec, f, indent=2)
        f.write('\n')


def create_rate_limit_headers() -> Dict[str, Any]:
    """Create rate limit header definitions."""
    return {
        "X-RateLimit-Limit": {
            "description": "The maximum number of requests allowed in the current time window",
            "schema": {
                "type": "integer",
                "example": 1000
            }
        },
        "X-RateLimit-Remaining": {
            "description": "The number of requests remaining in the current time window",
            "schema": {
                "type": "integer",
                "example": 999
            }
        },
        "X-RateLimit-Reset": {
            "description": "The time at which the current rate limit window resets (Unix timestamp)",
            "schema": {
                "type": "integer",
                "example": 1640995200
            }
        }
    }


def add_rate_limit_headers_to_components(spec: Dict[str, Any]) -> None:
    """Add rate limit headers to component response definitions."""
    print("Adding rate limit headers to component responses...")

    rate_limit_headers = create_rate_limit_headers()
    components_updated = 0

    for response_name, response_def in spec.get('components', {}).get('responses', {}).items():
        if 'headers' not in response_def:
            response_def['headers'] = {}

        # Add rate limit headers if not present
        for header_name, header_def in rate_limit_headers.items():
            if header_name not in response_def['headers']:
                response_def['headers'][header_name] = header_def
                components_updated += 1

    print(f"  Updated {components_updated} headers in component responses")


def remove_inline_headers_from_refs(spec: Dict[str, Any]) -> None:
    """Remove inline headers from responses that use $ref."""
    print("Removing inline headers from $ref responses...")

    refs_cleaned = 0

    for path, path_item in spec.get('paths', {}).items():
        for method, operation in path_item.items():
            if method in ['get', 'post', 'put', 'patch', 'delete']:
                for status_code, response in operation.get('responses', {}).items():
                    # If response has both $ref and headers, remove headers
                    if '$ref' in response and 'headers' in response:
                        del response['headers']
                        refs_cleaned += 1

                    # Also check for content alongside $ref
                    if '$ref' in response and 'content' in response:
                        del response['content']
                        refs_cleaned += 1

    print(f"  Cleaned {refs_cleaned} $ref siblings")


def add_rate_limit_headers_to_inline_responses(spec: Dict[str, Any]) -> None:
    """Add rate limit headers to inline response definitions (those without $ref)."""
    print("Adding rate limit headers to inline responses...")

    rate_limit_headers = create_rate_limit_headers()
    inline_updated = 0

    for path, path_item in spec.get('paths', {}).items():
        for method, operation in path_item.items():
            if method in ['get', 'post', 'put', 'patch', 'delete']:
                for status_code, response in operation.get('responses', {}).items():
                    # Only update inline responses (no $ref)
                    if '$ref' not in response:
                        # Check if it's a 2XX or 4XX response
                        if status_code.startswith('2') or status_code.startswith('4'):
                            if 'headers' not in response:
                                response['headers'] = {}

                            # Add rate limit headers if not present
                            for header_name, header_def in rate_limit_headers.items():
                                if header_name not in response['headers']:
                                    response['headers'][header_name] = header_def
                                    inline_updated += 1

    print(f"  Added {inline_updated} headers to inline responses")


def main():
    """Main execution function."""
    print("TMI OpenAPI Spectral Fix Script")
    print("=" * 60)

    # Create backup
    backup_file = OPENAPI_FILE.with_suffix('.json.backup-spectral')
    shutil.copy2(OPENAPI_FILE, backup_file)
    print(f"Created backup: {backup_file}")
    print()

    # Load specification
    spec = load_openapi()

    # Apply fixes
    add_rate_limit_headers_to_components(spec)
    remove_inline_headers_from_refs(spec)
    add_rate_limit_headers_to_inline_responses(spec)

    # Save updated specification
    save_openapi(spec)
    print()
    print(f"✓ Updated {OPENAPI_FILE}")
    print(f"✓ Backup saved to {backup_file}")
    print()
    print("Next steps:")
    print("  1. Run: spectral lint docs/reference/apis/tmi-openapi.json")
    print("  2. Run: make validate-openapi")
    print("  3. If issues, restore: cp {backup_file} {OPENAPI_FILE}")


if __name__ == '__main__':
    main()
