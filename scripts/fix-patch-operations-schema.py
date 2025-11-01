#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""
Fix PatchOperation schema references in OpenAPI spec.
Replaces $ref with inline schema definition.
"""

import json
from pathlib import Path

def fix_patch_operations(schema):
    """Replace PatchOperation $ref with inline definition."""

    patch_operation_schema = {
        "type": "object",
        "required": ["op", "path"],
        "properties": {
            "op": {
                "type": "string",
                "enum": ["add", "remove", "replace", "move", "copy", "test"]
            },
            "path": {
                "type": "string"
            },
            "value": {},
            "from": {
                "type": "string"
            }
        }
    }

    # Fix bulk PATCH threats endpoint
    bulk_path = "/threat_models/{threat_model_id}/threats/bulk"
    if bulk_path in schema["paths"] and "patch" in schema["paths"][bulk_path]:
        patch_op = schema["paths"][bulk_path]["patch"]
        if "requestBody" in patch_op:
            content = patch_op["requestBody"]["content"]["application/json-patch+json"]
            # Fix the nested operations schema
            if "schema" in content and "properties" in content["schema"]:
                patches_items = content["schema"]["properties"]["patches"]["items"]
                if "properties" in patches_items and "operations" in patches_items["properties"]:
                    patches_items["properties"]["operations"]["items"] = patch_operation_schema
                    print("Fixed bulk PATCH threats operations schema")

    # Add PATCH endpoints for simple resources (these were missing)
    resource_paths = {
        "/threat_models/{threat_model_id}/assets/{asset_id}": ("Asset", "Assets"),
        "/threat_models/{threat_model_id}/documents/{document_id}": ("Document", "Documents"),
        "/threat_models/{threat_model_id}/notes/{note_id}": ("Note", "Notes"),
        "/threat_models/{threat_model_id}/repositories/{repository_id}": ("Repository", "Repositories")
    }

    for path, (resource_singular, resource_plural) in resource_paths.items():
        if path in schema["paths"]:
            schema["paths"][path]["patch"] = {
                "summary": f"Partially update {resource_singular.lower()}",
                "description": f"Apply JSON Patch operations to partially update a {resource_singular.lower()}",
                "operationId": f"patchThreatModel{resource_singular}",
                "tags": [resource_plural],
                "parameters": [
                    {"$ref": "#/components/parameters/ThreatModelIdParam"},
                    {
                        "name": f"{resource_singular.lower()}_id" if resource_singular != "Repository" else "repository_id",
                        "in": "path",
                        "required": True,
                        "schema": {"type": "string", "format": "uuid"},
                        "description": f"{resource_singular} ID"
                    }
                ],
                "requestBody": {
                    "required": True,
                    "content": {
                        "application/json-patch+json": {
                            "schema": {
                                "type": "array",
                                "items": patch_operation_schema
                            }
                        }
                    }
                },
                "responses": {
                    "200": {
                        "description": f"Successfully patched {resource_singular.lower()}",
                        "content": {
                            "application/json": {
                                "schema": {"$ref": f"#/components/schemas/{resource_singular}"}
                            }
                        }
                    },
                    "400": {"$ref": "#/components/responses/BadRequest"},
                    "401": {"$ref": "#/components/responses/Unauthorized"},
                    "403": {"$ref": "#/components/responses/Forbidden"},
                    "404": {"$ref": "#/components/responses/NotFound"}
                },
                "security": [{"bearerAuth": []}]
            }
            print(f"Added PATCH endpoint for {path}")

    # Add PUT methods to bulk endpoints
    bulk_resource_paths = {
        "/threat_models/{threat_model_id}/assets/bulk": ("Asset", "Assets"),
        "/threat_models/{threat_model_id}/documents/bulk": ("Document", "Documents"),
        "/threat_models/{threat_model_id}/repositories/bulk": ("Repository", "Repositories")
    }

    for path, (resource_singular, resource_plural) in bulk_resource_paths.items():
        if path in schema["paths"] and "post" in schema["paths"][path]:
            # Copy POST and modify for PUT (upsert)
            put_op = json.loads(json.dumps(schema["paths"][path]["post"]))  # Deep copy
            put_op["summary"] = f"Bulk upsert {resource_plural.lower()}"
            put_op["description"] = f"Create or update multiple {resource_plural.lower()} in a single request"
            put_op["operationId"] = put_op["operationId"].replace("bulkCreate", "bulkUpsert")
            schema["paths"][path]["put"] = put_op
            print(f"Added PUT method to {path}")

    return schema


def main():
    schema_path = Path(__file__).parent.parent / "docs" / "reference" / "apis" / "tmi-openapi.json"

    print("Loading schema...")
    with open(schema_path, 'r') as f:
        schema = json.load(f)

    print("Fixing PatchOperation schemas and adding missing endpoints...")
    schema = fix_patch_operations(schema)

    print("Saving schema...")
    with open(schema_path, 'w') as f:
        json.dump(schema, f, indent=2)
        f.write('\n')

    print("Done!")


if __name__ == "__main__":
    main()
