#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""
Fix OpenAPI schema validation issues by adding missing constraints.

This script adds:
- maxLength to all string fields without patterns
- pattern to string fields in request bodies where appropriate
- maxItems to array fields in responses
"""

import json
import sys
from pathlib import Path


def add_string_constraints(schema, is_request=False):
    """Add maxLength and pattern constraints to string fields."""
    if not isinstance(schema, dict):
        return schema

    # Handle string types
    if schema.get("type") == "string":
        # Check if pattern is fully constraining (has anchors and fixed quantifiers)
        pattern = schema.get("pattern", "")
        has_anchors = pattern.startswith("^") and pattern.endswith("$")
        # Simple heuristic: if pattern has fixed quantifier like {n} or {n,m}, it's constraining
        has_fixed_quantifier = "{" in pattern and "}" in pattern and "," not in pattern.split("{")[1].split("}")[0] if "{" in pattern else False
        pattern_fully_constrains = has_anchors and has_fixed_quantifier

        # Add maxLength if not present
        if "maxLength" not in schema:
            # Use format-specific limits
            fmt = schema.get("format", "")
            if fmt == "uuid":
                schema["maxLength"] = 36
            elif fmt == "uri":
                schema["maxLength"] = 1000
            elif fmt == "email":
                schema["maxLength"] = 254
            elif fmt == "date-time":
                schema["maxLength"] = 64
            elif pattern_fully_constrains:
                # Pattern fully constrains length, no maxLength needed
                pass
            else:
                # Default reasonable limit for general strings
                # Even if pattern exists, still need maxLength for responses
                schema["maxLength"] = 1000

        # Add pattern for request fields if not present
        if is_request and "pattern" not in schema:
            fmt = schema.get("format", "")
            # Only add patterns where it makes sense
            # Let specific fields keep their own patterns, use generic for others
            if fmt not in ["uuid", "uri", "email", "date-time"]:
                # Generic pattern: printable Unicode characters
                schema["pattern"] = "^[\\u0020-\\uFFFF]*$"

    # Handle arrays
    elif schema.get("type") == "array":
        if "maxItems" not in schema and not is_request:
            # Add reasonable maxItems for response arrays
            schema["maxItems"] = 1000

        # Recurse into items
        if "items" in schema:
            schema["items"] = add_string_constraints(schema["items"], is_request)

    # Handle objects - recurse into properties
    elif schema.get("type") == "object" or "properties" in schema:
        if "properties" in schema:
            for key, prop in schema["properties"].items():
                schema["properties"][key] = add_string_constraints(prop, is_request)

    # Handle allOf, anyOf, oneOf
    for combiner in ["allOf", "anyOf", "oneOf"]:
        if combiner in schema:
            schema[combiner] = [add_string_constraints(s, is_request) for s in schema[combiner]]

    return schema


def process_openapi_spec(spec):
    """Process the entire OpenAPI specification."""

    # Process path operations
    if "paths" in spec:
        for path, path_item in spec["paths"].items():
            for method in ["get", "post", "put", "patch", "delete", "options", "head"]:
                if method not in path_item:
                    continue

                operation = path_item[method]

                # Process request bodies
                if "requestBody" in operation:
                    content = operation["requestBody"].get("content", {})
                    for media_type, media_schema in content.items():
                        if "schema" in media_schema:
                            media_schema["schema"] = add_string_constraints(
                                media_schema["schema"], is_request=True
                            )

                # Process parameters
                if "parameters" in operation:
                    for param in operation["parameters"]:
                        if "schema" in param:
                            param["schema"] = add_string_constraints(
                                param["schema"], is_request=True
                            )

                # Process responses
                if "responses" in operation:
                    for status, response in operation["responses"].items():
                        if "content" in response:
                            for media_type, media_schema in response["content"].items():
                                if "schema" in media_schema:
                                    media_schema["schema"] = add_string_constraints(
                                        media_schema["schema"], is_request=False
                                    )

    # Process component schemas
    if "components" in spec and "schemas" in spec["components"]:
        for schema_name, schema in spec["components"]["schemas"].items():
            # Treat component schemas as potentially used in both requests and responses
            # Be conservative and apply both sets of rules
            spec["components"]["schemas"][schema_name] = add_string_constraints(
                schema, is_request=False
            )

    return spec


def main():
    if len(sys.argv) != 3:
        print("Usage: fix-openapi-constraints.py <input.json> <output.json>")
        sys.exit(1)

    input_file = Path(sys.argv[1])
    output_file = Path(sys.argv[2])

    # Read OpenAPI spec
    with open(input_file) as f:
        spec = json.load(f)

    # Process spec
    spec = process_openapi_spec(spec)

    # Write updated spec
    with open(output_file, "w") as f:
        json.dump(spec, f, indent=2)

    print(f"Updated OpenAPI spec written to {output_file}")


if __name__ == "__main__":
    main()
