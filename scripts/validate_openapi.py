#!/usr/bin/env python3
# /// script
# requires-python = ">=3.8"
# dependencies = [
#     "jsonschema>=4.0.0",
#     "pyyaml>=6.0",
#     "requests>=2.25.0",
# ]
# ///
"""
OpenAPI Specification Validator

This script performs comprehensive validation of OpenAPI specifications including:
- JSON syntax validation
- OpenAPI structure validation
- Schema reference validation
- Best practices checking
- Endpoint coverage analysis
"""

import json
import sys
import re
import argparse
from pathlib import Path
from typing import Dict, List, Tuple, Any, Set
from collections import defaultdict

try:
    import jsonschema

    JSONSCHEMA_AVAILABLE = True
except ImportError:
    JSONSCHEMA_AVAILABLE = False


def load_openapi_spec(file_path: str) -> Dict[str, Any]:
    """Load and parse OpenAPI specification from JSON file."""
    try:
        with open(file_path, "r", encoding="utf-8") as f:
            return json.load(f)
    except FileNotFoundError:
        print(f"❌ Error: File not found: {file_path}")
        sys.exit(1)
    except json.JSONDecodeError as e:
        print(f"❌ Error: Invalid JSON in {file_path}: {e}")
        sys.exit(1)


def validate_basic_structure(spec: Dict[str, Any]) -> Tuple[List[str], List[str]]:
    """Validate basic OpenAPI structure and required fields."""
    errors = []
    warnings = []

    # Check required root fields
    required_fields = ["openapi", "info", "paths"]
    for field in required_fields:
        if field not in spec:
            errors.append(f"Missing required root field: {field}")

    # Check OpenAPI version
    openapi_version = spec.get("openapi", "")
    if not openapi_version.startswith("3."):
        warnings.append(
            f"OpenAPI version {openapi_version} may not be supported (expected 3.x)"
        )

    # Check info section
    info = spec.get("info", {})
    info_required = ["title", "version"]
    for field in info_required:
        if field not in info:
            errors.append(f"Missing required info.{field}")

    # Check paths
    paths = spec.get("paths", {})
    if not paths:
        errors.append("No paths defined")

    return errors, warnings


def validate_operations(spec: Dict[str, Any]) -> Tuple[List[str], List[str]]:
    """Validate individual operations in paths."""
    errors = []
    warnings = []

    paths = spec.get("paths", {})
    valid_methods = {
        "get",
        "post",
        "put",
        "delete",
        "patch",
        "head",
        "options",
        "trace",
    }

    for path, path_item in paths.items():
        if not isinstance(path_item, dict):
            continue

        for method, operation in path_item.items():
            if method.lower() not in valid_methods or not isinstance(operation, dict):
                continue

            operation_id = f"{method.upper()} {path}"

            # Check for required operation fields
            if "responses" not in operation:
                errors.append(f"Missing responses in {operation_id}")

            # Check for recommended fields
            if "operationId" not in operation:
                warnings.append(f"Missing operationId in {operation_id}")

            if "description" not in operation:
                warnings.append(f"Missing description in {operation_id}")

            if "tags" not in operation:
                warnings.append(f"Missing tags in {operation_id}")

            # Validate responses
            responses = operation.get("responses", {})
            if responses:
                # Check for success response (2xx)
                has_success = any(
                    str(code).startswith("2") for code in responses.keys()
                )
                has_redirect = any(
                    str(code).startswith("3") for code in responses.keys()
                )

                # Only warn about missing success response if no redirect is present
                if not has_success and not has_redirect:
                    warnings.append(f"No success response (2xx) in {operation_id}")

                # Check for error responses (except for redirect-only endpoints)
                if not has_redirect:
                    has_error = any(
                        str(code).startswith(("4", "5")) for code in responses.keys()
                    )
                    if not has_error:
                        warnings.append(
                            f"No error response (4xx/5xx) in {operation_id}"
                        )

    return errors, warnings


def validate_schema_references(spec: Dict[str, Any]) -> Tuple[List[str], List[str]]:
    """Validate schema references throughout the specification."""
    errors = []
    warnings = []

    def find_refs(obj, path=""):
        """Recursively find all $ref occurrences."""
        refs = []
        if isinstance(obj, dict):
            if "$ref" in obj:
                refs.append((path, obj["$ref"]))
            for key, value in obj.items():
                new_path = f"{path}.{key}" if path else key
                refs.extend(find_refs(value, new_path))
        elif isinstance(obj, list):
            for i, item in enumerate(obj):
                new_path = f"{path}[{i}]"
                refs.extend(find_refs(item, new_path))
        return refs

    # Get all references in the spec
    all_refs = find_refs(spec)

    # Get available schemas
    components = spec.get("components", {})
    schemas = components.get("schemas", {})
    security_schemes = components.get("securitySchemes", {})

    # Validate schema references
    schema_refs = []
    for ref_path, ref in all_refs:
        if ref.startswith("#/components/schemas/"):
            schema_name = ref.split("/")[-1]
            schema_refs.append(schema_name)
            if schema_name not in schemas:
                errors.append(f"Broken schema reference: {ref} used in {ref_path}")
        elif ref.startswith("#/components/securitySchemes/"):
            scheme_name = ref.split("/")[-1]
            if scheme_name not in security_schemes:
                errors.append(
                    f"Broken security scheme reference: {ref} used in {ref_path}"
                )

    # Check for unused schemas
    used_schemas = set(schema_refs)
    unused_schemas = set(schemas.keys()) - used_schemas

    for unused in unused_schemas:
        warnings.append(f"Unused schema: {unused}")

    return errors, warnings


def analyze_endpoint_coverage(spec: Dict[str, Any]) -> Dict[str, List[str]]:
    """Analyze endpoint coverage by functional category."""
    paths = spec.get("paths", {})

    categories = {
        "Root": [],
        "Authentication": [],
        "Threat Models": [],
        "Diagrams": [],
        "Threats": [],
        "Documents": [],
        "Sources": [],
        "Metadata": [],
        "Collaboration": [],
        "Other": [],
    }

    for path in paths.keys():
        if path == "/":
            categories["Root"].append(path)
        elif path.startswith("/oauth2"):
            categories["Authentication"].append(path)
        elif path.startswith("/collaboration"):
            categories["Collaboration"].append(path)
        elif "/threats" in path:
            categories["Threats"].append(path)
        elif "/diagrams" in path:
            categories["Diagrams"].append(path)
        elif "/documents" in path:
            categories["Documents"].append(path)
        elif "/sources" in path:
            categories["Sources"].append(path)
        elif "/metadata" in path:
            categories["Metadata"].append(path)
        elif path.startswith("/threat_models"):
            categories["Threat Models"].append(path)
        else:
            categories["Other"].append(path)

    # Remove empty categories
    return {k: v for k, v in categories.items() if v}


def check_required_endpoints(spec: Dict[str, Any]) -> List[str]:
    """Check for presence of core required endpoints."""
    paths = spec.get("paths", {})

    required_endpoints = [
        "/",
        "/oauth2/providers",
        "/oauth2/authorize",
        "/oauth2/callback",
        "/threat_models",
        "/threat_models/{threat_model_id}",
        "/threat_models/{threat_model_id}/diagrams",
        "/threat_models/{threat_model_id}/diagrams/{diagram_id}",
    ]

    missing = []
    for endpoint in required_endpoints:
        if endpoint not in paths:
            missing.append(endpoint)

    return missing


def validate_security_definitions(spec: Dict[str, Any]) -> Tuple[List[str], List[str]]:
    """Validate security scheme definitions."""
    errors = []
    warnings = []

    components = spec.get("components", {})
    security_schemes = components.get("securitySchemes", {})

    if not security_schemes:
        warnings.append("No security schemes defined")
        return errors, warnings

    # Check each security scheme
    for scheme_name, scheme in security_schemes.items():
        if "type" not in scheme:
            errors.append(f"Security scheme {scheme_name} missing type")
            continue

        scheme_type = scheme["type"]

        if scheme_type == "http":
            if "scheme" not in scheme:
                errors.append(f"HTTP security scheme {scheme_name} missing scheme")
        elif scheme_type == "apiKey":
            if "in" not in scheme or "name" not in scheme:
                errors.append(f"API key security scheme {scheme_name} missing in/name")
        elif scheme_type == "oauth2":
            if "flows" not in scheme:
                errors.append(f"OAuth2 security scheme {scheme_name} missing flows")

    return errors, warnings


def main():
    parser = argparse.ArgumentParser(
        description="Validate OpenAPI specification",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("file", help="OpenAPI specification file (JSON format)")
    parser.add_argument(
        "--quiet",
        "-q",
        action="store_true",
        help="Only show errors and warnings, not detailed analysis",
    )

    args = parser.parse_args()

    # Load specification
    spec = load_openapi_spec(args.file)

    # Print header
    if not args.quiet:
        print("🔍 OpenAPI Specification Validation")
        print("=" * 50)

        info = spec.get("info", {})
        print(f"Title: {info.get('title', 'Not specified')}")
        print(f"Version: {info.get('version', 'Not specified')}")
        print(f"OpenAPI Version: {spec.get('openapi', 'Not specified')}")
        print()

    # Collect all errors and warnings
    all_errors = []
    all_warnings = []

    # Validate basic structure
    errors, warnings = validate_basic_structure(spec)
    all_errors.extend(errors)
    all_warnings.extend(warnings)

    # Validate operations
    errors, warnings = validate_operations(spec)
    all_errors.extend(errors)
    all_warnings.extend(warnings)

    # Validate schema references
    errors, warnings = validate_schema_references(spec)
    all_errors.extend(errors)
    all_warnings.extend(warnings)

    # Validate security definitions
    errors, warnings = validate_security_definitions(spec)
    all_errors.extend(errors)
    all_warnings.extend(warnings)

    # Show results
    if all_errors:
        print("❌ Errors:")
        for error in all_errors:
            print(f"  • {error}")
        print()

    if all_warnings:
        print("⚠️  Warnings:")
        # Limit warnings to avoid spam
        shown_warnings = all_warnings[:15]
        for warning in shown_warnings:
            print(f"  • {warning}")
        if len(all_warnings) > 15:
            print(f"  ... and {len(all_warnings) - 15} more warnings")
        print()

    if not args.quiet:
        # Show endpoint coverage
        coverage = analyze_endpoint_coverage(spec)
        print("📊 Endpoint Coverage:")
        total_endpoints = 0
        for category, endpoints in coverage.items():
            print(f"  {category}: {len(endpoints)} endpoints")
            total_endpoints += len(endpoints)
        print(f"  Total: {total_endpoints} endpoints")
        print()

        # Check required endpoints
        missing_required = check_required_endpoints(spec)
        print("🔍 Required Endpoint Check:")
        if missing_required:
            print("  Missing required endpoints:")
            for endpoint in missing_required:
                print(f"    ❌ {endpoint}")
        else:
            print("  ✅ All required endpoints present")
        print()

        # Show component summary
        components = spec.get("components", {})
        schemas = components.get("schemas", {})
        security_schemes = components.get("securitySchemes", {})
        print("📋 Component Summary:")
        print(f"  Schemas: {len(schemas)}")
        print(f"  Security Schemes: {len(security_schemes)}")
        if security_schemes:
            print(f"  Available: {', '.join(security_schemes.keys())}")
        print()

    # Final status
    if all_errors:
        print(
            f"❌ Validation failed with {len(all_errors)} errors and {len(all_warnings)} warnings"
        )
        sys.exit(1)
    elif all_warnings:
        print(f"⚠️  Validation completed with {len(all_warnings)} warnings")
    else:
        print("✅ Validation successful - no issues found!")


if __name__ == "__main__":
    main()
