#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""Replace parameter references with inline definitions."""

import json
from pathlib import Path

def replace_param_refs(schema):
    """Replace all ThreatModelIdParam references with inline definitions."""

    threat_model_id_param = {
        "name": "threat_model_id",
        "in": "path",
        "required": True,
        "schema": {
            "type": "string",
            "format": "uuid",
            "maxLength": 36
        },
        "description": "Unique identifier of the threat model (UUID)"
    }

    paths = schema["paths"]
    count = 0

    for path_url, path_item in paths.items():
        for method in ["get", "post", "put", "patch", "delete"]:
            if method in path_item and "parameters" in path_item[method]:
                parameters = path_item[method]["parameters"]
                for i, param in enumerate(parameters):
                    if isinstance(param, dict) and "$ref" in param:
                        if param["$ref"] == "#/components/parameters/ThreatModelIdParam":
                            parameters[i] = threat_model_id_param.copy()
                            count += 1
                            print(f"Replaced ThreatModelIdParam in {method.upper()} {path_url}")

    print(f"\nTotal replacements: {count}")
    return schema


def main():
    schema_path = Path(__file__).parent.parent / "docs" / "reference" / "apis" / "tmi-openapi.json"

    print("Loading schema...")
    with open(schema_path, 'r') as f:
        schema = json.load(f)

    print("Replacing parameter references...")
    schema = replace_param_refs(schema)

    print("Saving schema...")
    with open(schema_path, 'w') as f:
        json.dump(schema, f, indent=2)
        f.write('\n')

    print("Done!")


if __name__ == "__main__":
    main()
