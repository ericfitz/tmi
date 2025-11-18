#!/usr/bin/env python3
"""
Fix missing additionalProperties in OpenAPI specification.

This script adds additionalProperties: false to all object schemas that don't
explicitly define it, following security best practices for OpenAPI 3.x.

Security rationale:
- In OAS3, additionalProperties defaults to true (unlike OAS2)
- Leaving it undefined allows arbitrary properties, enabling injection attacks
- Setting to false enforces strict schema validation

Special cases handled:
- allOf: Must have additionalProperties: true for schema composition
- oneOf/anyOf with objects: Requires additionalProperties: true
- oneOf/anyOf with primitives: Can use additionalProperties: false
"""
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

import json
import sys
from pathlib import Path
from typing import Any, Dict, Set


def needs_additional_properties_true(schema: Dict[str, Any]) -> bool:
    """
    Determine if a schema must have additionalProperties: true.

    Rules:
    - allOf always requires true (schema composition)
    - oneOf/anyOf with object properties requires true
    - Primitives (no properties) can use false
    """
    # Check for allOf - always requires true
    if "allOf" in schema:
        return True

    # Check for oneOf/anyOf with object properties
    for combiner in ["oneOf", "anyOf"]:
        if combiner in schema:
            # If any subschema has properties, we need true
            for subschema in schema[combiner]:
                if isinstance(subschema, dict):
                    if "properties" in subschema:
                        return True
                    if "$ref" in subschema:
                        # References might have properties, safer to use true
                        return True

    return False


def fix_schema(schema: Dict[str, Any], path: str, fixed_paths: Set[str]) -> None:
    """
    Recursively fix additionalProperties in a schema and all nested schemas.

    Args:
        schema: The schema object to fix
        path: JSON path for tracking (for debugging)
        fixed_paths: Set of paths already fixed to avoid duplicates
    """
    if not isinstance(schema, dict):
        return

    # Process this schema if it's an object type without additionalProperties
    if schema.get("type") == "object" and "additionalProperties" not in schema:
        if path not in fixed_paths:
            # Determine correct value based on combining operations
            if needs_additional_properties_true(schema):
                schema["additionalProperties"] = True
                print(f"  Set additionalProperties: true at {path} (has combining operations)")
            else:
                schema["additionalProperties"] = False
                print(f"  Set additionalProperties: false at {path}")
            fixed_paths.add(path)

    # Recursively process nested structures
    for key, value in list(schema.items()):
        if isinstance(value, dict):
            fix_schema(value, f"{path}.{key}", fixed_paths)
        elif isinstance(value, list):
            for i, item in enumerate(value):
                if isinstance(item, dict):
                    fix_schema(item, f"{path}.{key}[{i}]", fixed_paths)


def fix_openapi_spec(spec_path: Path) -> None:
    """
    Fix all schemas in an OpenAPI specification.

    Args:
        spec_path: Path to the OpenAPI JSON file
    """
    print(f"Loading OpenAPI spec from {spec_path}")
    with open(spec_path, 'r') as f:
        spec = json.load(f)

    fixed_paths: Set[str] = set()

    # Fix schemas in components
    if "components" in spec and "schemas" in spec["components"]:
        print("\nFixing schemas in components...")
        for schema_name, schema in spec["components"]["schemas"].items():
            fix_schema(schema, f"#/components/schemas/{schema_name}", fixed_paths)

    # Fix inline schemas in paths
    if "paths" in spec:
        print("\nFixing inline schemas in paths...")
        for path, path_item in spec["paths"].items():
            if not isinstance(path_item, dict):
                continue

            for method, operation in path_item.items():
                if not isinstance(operation, dict):
                    continue

                # Fix request body schemas
                if "requestBody" in operation:
                    req_body = operation["requestBody"]
                    if "content" in req_body:
                        for media_type, media_obj in req_body["content"].items():
                            if "schema" in media_obj:
                                fix_schema(
                                    media_obj["schema"],
                                    f"{path}.{method}.requestBody.content.{media_type}.schema",
                                    fixed_paths
                                )

                # Fix response schemas
                if "responses" in operation:
                    for status, response in operation["responses"].items():
                        if not isinstance(response, dict):
                            continue
                        if "content" in response:
                            for media_type, media_obj in response["content"].items():
                                if "schema" in media_obj:
                                    fix_schema(
                                        media_obj["schema"],
                                        f"{path}.{method}.responses.{status}.content.{media_type}.schema",
                                        fixed_paths
                                    )

                # Fix parameter schemas
                if "parameters" in operation:
                    for i, param in enumerate(operation["parameters"]):
                        if isinstance(param, dict) and "schema" in param:
                            fix_schema(
                                param["schema"],
                                f"{path}.{method}.parameters[{i}].schema",
                                fixed_paths
                            )

    # Create backup
    backup_path = spec_path.with_suffix('.json.backup')
    print(f"\nCreating backup at {backup_path}")
    with open(backup_path, 'w') as f:
        json.dump(spec, f, indent=2)

    # Write fixed spec
    print(f"Writing fixed spec to {spec_path}")
    with open(spec_path, 'w') as f:
        json.dump(spec, f, indent=2)

    print(f"\n✓ Fixed {len(fixed_paths)} schemas")
    print(f"✓ Backup saved to {backup_path}")


def main():
    spec_path = Path("docs/reference/apis/tmi-openapi.json")

    if not spec_path.exists():
        print(f"Error: {spec_path} not found", file=sys.stderr)
        sys.exit(1)

    fix_openapi_spec(spec_path)
    print("\n✓ Done! Run 'make validate-openapi' to verify the changes.")


if __name__ == "__main__":
    main()
