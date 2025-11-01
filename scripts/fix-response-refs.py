#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""Replace invalid response references with Error response."""

import json
from pathlib import Path

def fix_response_refs(schema):
    """Replace BadRequest, Unauthorized, Forbidden, NotFound with Error response."""

    invalid_refs = [
        "#/components/responses/BadRequest",
        "#/components/responses/Unauthorized",
        "#/components/responses/Forbidden",
        "#/components/responses/NotFound"
    ]

    error_ref = {"$ref": "#/components/responses/Error"}

    paths = schema["paths"]
    count = 0

    for path_url, path_item in paths.items():
        for method in ["get", "post", "put", "patch", "delete"]:
            if method in path_item and "responses" in path_item[method]:
                responses = path_item[method]["responses"]
                for status_code, response_def in responses.items():
                    if isinstance(response_def, dict) and "$ref" in response_def:
                        if response_def["$ref"] in invalid_refs:
                            responses[status_code] = error_ref.copy()
                            count += 1

    print(f"Replaced {count} invalid response references with Error response")
    return schema


def main():
    schema_path = Path(__file__).parent.parent / "docs" / "reference" / "apis" / "tmi-openapi.json"

    print("Loading schema...")
    with open(schema_path, 'r') as f:
        schema = json.load(f)

    print("Fixing response references...")
    schema = fix_response_refs(schema)

    print("Saving schema...")
    with open(schema_path, 'w') as f:
        json.dump(schema, f, indent=2)
        f.write('\n')

    print("Done!")


if __name__ == "__main__":
    main()
