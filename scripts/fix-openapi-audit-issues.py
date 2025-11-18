#!/usr/bin/env python3
# /// script
# dependencies = ["jsonschema"]
# ///

"""
Fix OpenAPI Audit Issues

This script addresses the following API scanner issues:
1. Add global security field ✓ (already done manually)
2. Add maxItems constraints to array schemas in requests
3. Remove duplicate 'type' properties in schemas using allOf
4. Remove duplicate 'shape' properties in schemas using allOf
5. Remove duplicate timestamp properties in schemas using allOf
6. Add security fields to operations missing them
7. Remove/fix empty security arrays in public endpoints
"""

import json
import sys
from pathlib import Path
from typing import Any, Dict, List, Set


def load_openapi(file_path: str) -> Dict[str, Any]:
    """Load OpenAPI specification from JSON file."""
    with open(file_path, 'r') as f:
        return json.load(f)


def save_openapi(spec: Dict[str, Any], file_path: str) -> None:
    """Save OpenAPI specification to JSON file with formatting."""
    with open(file_path, 'w') as f:
        json.dump(spec, f, indent=2)
        f.write('\n')  # Add trailing newline


def get_parent_properties(spec: Dict[str, Any], schema: Dict[str, Any]) -> Set[str]:
    """
    Get all properties defined in parent schemas referenced by allOf.
    Recursively follows $ref to collect all properties.
    """
    properties = set()

    if 'allOf' not in schema:
        return properties

    for item in schema['allOf']:
        if '$ref' in item:
            # Extract schema name from reference
            ref_path = item['$ref'].split('/')
            if len(ref_path) >= 4 and ref_path[-3] == 'components' and ref_path[-2] == 'schemas':
                schema_name = ref_path[-1]
                parent_schema = spec.get('components', {}).get('schemas', {}).get(schema_name, {})

                # Get properties directly defined in parent
                if 'properties' in parent_schema:
                    properties.update(parent_schema['properties'].keys())

                # Recursively get properties from parent's parents
                properties.update(get_parent_properties(spec, parent_schema))

    return properties


def remove_duplicate_properties_in_schema(spec: Dict[str, Any], schema_name: str, schema: Dict[str, Any]) -> bool:
    """
    Remove properties from a schema that are already defined in its allOf parent schemas.
    ONLY removes true duplicates, NOT refinements (where child narrows parent type).
    Returns True if changes were made.
    """
    if 'allOf' not in schema:
        return False

    parent_props = get_parent_properties(spec, schema)
    if not parent_props:
        return False

    changed = False
    for item in schema['allOf']:
        if '$ref' not in item and 'properties' in item:
            # Find properties that duplicate parent properties
            duplicates = set(item['properties'].keys()) & parent_props

            # Don't remove properties that refine/narrow the parent definition
            # (e.g., adding enum constraints to a string, or adding pattern)
            to_remove = set()
            for prop in duplicates:
                child_def = item['properties'][prop]
                # If child has enum or is more specific, keep it (it's a refinement)
                if 'enum' in child_def:
                    continue  # Keep refinements with enum
                if '$ref' in child_def:
                    continue  # Keep references
                to_remove.add(prop)

            if to_remove:
                print(f"  Removing duplicate properties from {schema_name}: {', '.join(sorted(to_remove))}")
                for prop in to_remove:
                    del item['properties'][prop]
                changed = True

                # Clean up empty properties object
                if not item['properties']:
                    del item['properties']

    return changed


def fix_duplicate_properties(spec: Dict[str, Any]) -> int:
    """
    Fix duplicate property definitions in schemas using allOf.
    Only removes true duplicates, preserves refinements.
    Returns count of schemas fixed.
    """
    count = 0
    schemas = spec.get('components', {}).get('schemas', {})

    # Skip schemas that use property refinement intentionally
    skip_schemas = {'Node', 'Edge', 'DfdDiagram', 'DfdDiagramInput'}

    for schema_name, schema in schemas.items():
        if schema_name in skip_schemas:
            print(f"  Skipping {schema_name} (uses intentional property refinement)")
            continue

        if remove_duplicate_properties_in_schema(spec, schema_name, schema):
            count += 1

    return count


def add_max_items_to_arrays(spec: Dict[str, Any], max_items: int = 10000) -> int:
    """
    Add maxItems constraint to array schemas that don't have one.
    Focus on request bodies and parameters.
    Returns count of arrays fixed.
    """
    count = 0

    def add_max_items_recursive(obj: Any, path: str = "") -> None:
        nonlocal count

        if isinstance(obj, dict):
            # If this is an array schema without maxItems, add it
            if obj.get('type') == 'array' and 'maxItems' not in obj:
                # Use different limits based on context
                if 'cells' in path or 'diagram' in path.lower():
                    obj['maxItems'] = 10000  # Diagrams can have many cells
                elif 'metadata' in path.lower():
                    obj['maxItems'] = 100  # Metadata arrays
                elif 'participants' in path.lower():
                    obj['maxItems'] = 1000  # Collaboration participants
                else:
                    obj['maxItems'] = 1000  # Default reasonable limit

                print(f"  Added maxItems={obj['maxItems']} to array at {path or 'root'}")
                count += 1

            # Recurse into nested objects
            for key, value in obj.items():
                new_path = f"{path}.{key}" if path else key
                add_max_items_recursive(value, new_path)

        elif isinstance(obj, list):
            for i, item in enumerate(obj):
                add_max_items_recursive(item, f"{path}[{i}]")

    # Process request bodies in paths
    for path, path_item in spec.get('paths', {}).items():
        for method, operation in path_item.items():
            if method.lower() in ['post', 'put', 'patch'] and isinstance(operation, dict):
                if 'requestBody' in operation:
                    add_max_items_recursive(
                        operation['requestBody'],
                        f"paths.{path}.{method}.requestBody"
                    )

    # Also check schemas that might be used in requests
    for schema_name, schema in spec.get('components', {}).get('schemas', {}).items():
        add_max_items_recursive(schema, f"schemas.{schema_name}")

    return count


def add_security_to_operations(spec: Dict[str, Any]) -> int:
    """
    Add security field to operations that don't have one.
    Public endpoints get empty security: []
    Protected endpoints get security: [{"bearerAuth": []}]
    Returns count of operations fixed.
    """
    count = 0
    # Public endpoints that don't require authentication
    public_paths = {'/health', '/'}
    public_prefixes = ['/oauth2/', '/saml/']

    for path, path_item in spec.get('paths', {}).items():
        for method, operation in path_item.items():
            if method.lower() in ['get', 'post', 'put', 'patch', 'delete'] and isinstance(operation, dict):
                # Determine if this is a public endpoint
                is_public = (path in public_paths or
                            any(path.startswith(prefix) for prefix in public_prefixes))

                if 'security' not in operation:
                    if is_public:
                        operation['security'] = []
                        print(f"  Added security: [] to {method.upper()} {path} (public endpoint)")
                    else:
                        operation['security'] = [{"bearerAuth": []}]
                        print(f"  Added security: bearerAuth to {method.upper()} {path}")
                    count += 1

                # Fix operations with malformed empty security
                elif operation.get('security') == [{}]:
                    operation['security'] = []
                    print(f"  Fixed malformed security on {method.upper()} {path}")
                    count += 1

    return count


def main() -> int:
    """Main execution function."""
    if len(sys.argv) < 2:
        print("Usage: fix-openapi-audit-issues.py <openapi-file.json>")
        return 1

    file_path = sys.argv[1]
    if not Path(file_path).exists():
        print(f"Error: File not found: {file_path}")
        return 1

    print(f"Loading OpenAPI spec from {file_path}...")
    spec = load_openapi(file_path)

    # Remove global security if it exists - we'll use operation-level security instead
    if 'security' in spec:
        print("\n=== Removing global security (using operation-level security instead) ===")
        del spec['security']
        print("Removed global security field")

    print("\n=== Fixing duplicate properties in schemas ===")
    dup_count = fix_duplicate_properties(spec)
    print(f"Fixed {dup_count} schemas with duplicate properties")

    print("\n=== Adding maxItems to array schemas ===")
    array_count = add_max_items_to_arrays(spec)
    print(f"Added maxItems to {array_count} arrays")

    print("\n=== Adding security to operations ===")
    security_count = add_security_to_operations(spec)
    print(f"Fixed {security_count} operations")

    print(f"\n=== Saving updated OpenAPI spec to {file_path} ===")
    save_openapi(spec, file_path)

    total_changes = dup_count + array_count + security_count
    print(f"\nTotal changes: {total_changes}")
    print("Done!")

    return 0


if __name__ == '__main__':
    sys.exit(main())
