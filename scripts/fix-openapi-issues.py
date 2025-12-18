#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""
Fix RateMyOpenAPI issues in TMI OpenAPI specification.

This script addresses:
1. Add Apache 2.0 license with URL
2. Add HTTPS production server
3. Add enum values to server variables
4. Add global security requirement (default to bearerAuth)
5. Add rate limit headers to all 2XX and 4XX responses
6. Add 401 responses to protected endpoints
7. Extract inline schemas to components/schemas (optional)

Usage:
    uv run scripts/fix-openapi-issues.py
"""

import json
import sys
from pathlib import Path
from typing import Any, Dict, List, Set
from copy import deepcopy

OPENAPI_FILE = Path("docs/reference/apis/tmi-openapi.json")


def load_openapi() -> Dict[str, Any]:
    """Load the OpenAPI specification."""
    with open(OPENAPI_FILE, 'r') as f:
        return json.load(f)


def save_openapi(spec: Dict[str, Any]) -> None:
    """Save the OpenAPI specification with pretty formatting."""
    with open(OPENAPI_FILE, 'w') as f:
        json.dump(spec, f, indent=2)
        f.write('\n')  # Add trailing newline


def add_license(spec: Dict[str, Any]) -> None:
    """Add Apache 2.0 license to info section."""
    print("Adding Apache 2.0 license...")
    spec['info']['license'] = {
        "name": "Apache 2.0",
        "url": "https://www.apache.org/licenses/LICENSE-2.0.html"
    }


def add_production_server(spec: Dict[str, Any]) -> None:
    """Add HTTPS production server."""
    print("Adding production server...")

    # Add enum to existing server variables
    for server in spec.get('servers', []):
        if 'variables' in server:
            for var_name, var_config in server['variables'].items():
                if var_name == 'port' and 'enum' not in var_config:
                    var_config['enum'] = ["8080", "8443", "3000"]

    # Add production server
    production_server = {
        "description": "Production server",
        "url": "https://api.tmi.dev"
    }

    # Check if production server already exists
    existing_urls = [s.get('url') for s in spec.get('servers', [])]
    if production_server['url'] not in existing_urls:
        spec['servers'].append(production_server)


def add_global_security(spec: Dict[str, Any]) -> None:
    """Add global security requirement (secure by default)."""
    print("Adding global security requirement...")
    spec['security'] = [{"bearerAuth": []}]


def create_rate_limit_headers() -> Dict[str, Any]:
    """Create reusable rate limit header definitions."""
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


def add_rate_limit_headers(spec: Dict[str, Any]) -> None:
    """Add rate limit headers to all 2XX and 4XX responses."""
    print("Adding rate limit headers to responses...")

    rate_limit_headers = create_rate_limit_headers()
    count = 0

    for path, path_item in spec.get('paths', {}).items():
        for method, operation in path_item.items():
            if method in ['get', 'post', 'put', 'patch', 'delete']:
                for status_code, response in operation.get('responses', {}).items():
                    # Add to 2XX and 4XX responses
                    if status_code.startswith('2') or status_code.startswith('4'):
                        if 'headers' not in response:
                            response['headers'] = {}

                        # Add rate limit headers if not already present
                        for header_name, header_def in rate_limit_headers.items():
                            if header_name not in response['headers']:
                                response['headers'][header_name] = header_def
                                count += 1

    print(f"  Added {count} rate limit header definitions")


def create_401_response() -> Dict[str, Any]:
    """Create reusable 401 Unauthorized response."""
    return {
        "description": "Unauthorized - Invalid or missing authentication token",
        "headers": {
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
        },
        "content": {
            "application/json": {
                "schema": {
                    "$ref": "#/components/schemas/Error"
                }
            }
        }
    }


def add_401_responses(spec: Dict[str, Any]) -> None:
    """Add 401 responses to protected endpoints (those without security: [])."""
    print("Adding 401 responses to protected endpoints...")

    response_401 = create_401_response()
    count = 0

    for path, path_item in spec.get('paths', {}).items():
        for method, operation in path_item.items():
            if method in ['get', 'post', 'put', 'patch', 'delete']:
                # Check if this is a protected endpoint
                # Protected = does NOT have security: [] override
                is_public = operation.get('security') == []

                if not is_public:
                    # This is a protected endpoint
                    if '401' not in operation.get('responses', {}):
                        if 'responses' not in operation:
                            operation['responses'] = {}
                        operation['responses']['401'] = deepcopy(response_401)
                        count += 1
                    elif 'content' not in operation['responses']['401']:
                        # 401 exists but missing content
                        operation['responses']['401']['content'] = response_401['content']
                        count += 1

    print(f"  Added {count} 401 responses")


def get_public_endpoints(spec: Dict[str, Any]) -> List[str]:
    """Get list of public endpoints for reporting."""
    public = []
    for path, path_item in spec.get('paths', {}).items():
        for method, operation in path_item.items():
            if method in ['get', 'post', 'put', 'patch', 'delete']:
                if operation.get('security') == []:
                    public.append(f"{method.upper()} {path}")
    return public


def main():
    """Main execution function."""
    print("TMI OpenAPI Remediation Script")
    print("=" * 60)

    # Create backup
    backup_file = OPENAPI_FILE.with_suffix('.json.backup')
    import shutil
    shutil.copy2(OPENAPI_FILE, backup_file)
    print(f"Created backup: {backup_file}")
    print()

    # Load specification
    spec = load_openapi()

    # Apply fixes
    add_license(spec)
    add_production_server(spec)
    add_global_security(spec)
    add_rate_limit_headers(spec)
    add_401_responses(spec)

    # Report public endpoints
    public_endpoints = get_public_endpoints(spec)
    print()
    print(f"Public endpoints (security: []): {len(public_endpoints)}")
    for endpoint in sorted(public_endpoints):
        print(f"  - {endpoint}")

    # Save updated specification
    save_openapi(spec)
    print()
    print(f"✓ Updated {OPENAPI_FILE}")
    print(f"✓ Backup saved to {backup_file}")
    print()
    print("Next steps:")
    print("  1. Run: make validate-openapi")
    print("  2. Review changes: git diff docs/reference/apis/tmi-openapi.json")
    print("  3. If issues, restore: cp {backup_file} {OPENAPI_FILE}")


if __name__ == '__main__':
    main()
