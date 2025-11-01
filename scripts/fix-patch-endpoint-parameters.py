#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""Fix parameter names in PATCH endpoints to match path parameters."""

import json
from pathlib import Path

def fix_patch_parameters(schema):
    """Fix parameter names in PATCH endpoints."""

    # Map of paths to correct parameter names
    path_params = {
        "/threat_models/{threat_model_id}/assets/{asset_id}": "asset_id",
        "/threat_models/{threat_model_id}/documents/{document_id}": "document_id",
        "/threat_models/{threat_model_id}/notes/{note_id}": "note_id",
        "/threat_models/{threat_model_id}/repositories/{repository_id}": "repository_id"
    }

    for path, id_param_name in path_params.items():
        if path in schema["paths"] and "patch" in schema["paths"][path]:
            patch_op = schema["paths"][path]["patch"]
            if "parameters" in patch_op and len(patch_op["parameters"]) >= 2:
                # Fix the second parameter (ID parameter)
                patch_op["parameters"][1]["name"] = id_param_name
                print(f"Fixed parameter name for {path}: {id_param_name}")

    return schema


def main():
    schema_path = Path(__file__).parent.parent / "docs" / "reference" / "apis" / "tmi-openapi.json"

    print("Loading schema...")
    with open(schema_path, 'r') as f:
        schema = json.load(f)

    print("Fixing PATCH endpoint parameters...")
    schema = fix_patch_parameters(schema)

    print("Saving schema...")
    with open(schema_path, 'w') as f:
        json.dump(schema, f, indent=2)
        f.write('\n')

    print("Done!")


if __name__ == "__main__":
    main()
