#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""
OpenAPI Schema Normalization Script for TMI v1.0.0

This script transforms the TMI OpenAPI specification to implement the schema
normalization plan, including:
- Creating Base/Input/Complete patterns for all resources
- Adding timestamps to all resources
- Adding description field to Diagrams
- Removing batch endpoints
- Adding bulk PATCH/DELETE and PATCH endpoints
- Creating ListItem schemas
- Adding documentation

Usage:
    uv run scripts/normalize-openapi-schema.py
"""

import json
import sys
from pathlib import Path
from typing import Any, Dict, List


def load_openapi_schema(file_path: str) -> Dict[str, Any]:
    """Load OpenAPI schema from JSON file."""
    with open(file_path, 'r') as f:
        return json.load(f)


def save_openapi_schema(schema: Dict[str, Any], file_path: str) -> None:
    """Save OpenAPI schema to JSON file with pretty formatting."""
    with open(file_path, 'w') as f:
        json.dump(schema, f, indent=2)
        f.write('\n')  # Add trailing newline


def create_base_schema(resource_name: str, original_schema: Dict[str, Any]) -> Dict[str, Any]:
    """
    Create a Base schema from an original resource schema.
    Removes server-generated fields: id, metadata, created_at, modified_at
    """
    base_schema = {
        "type": "object",
        "description": f"Base fields for {resource_name} (user-writable only)",
        "required": original_schema.get("required", []).copy() if "required" in original_schema else [],
        "properties": {}
    }

    # Remove id from required fields if present
    if "id" in base_schema["required"]:
        base_schema["required"].remove("id")

    # Copy properties except server-generated ones
    server_fields = {"id", "metadata", "created_at", "modified_at"}
    for prop_name, prop_def in original_schema.get("properties", {}).items():
        if prop_name not in server_fields:
            base_schema["properties"][prop_name] = prop_def.copy()

    return base_schema


def create_input_schema(resource_name: str, base_schema_ref: str) -> Dict[str, Any]:
    """
    Create an Input schema that references the Base schema via allOf.
    """
    return {
        "description": f"Input schema for creating or updating {resource_name}",
        "allOf": [{"$ref": base_schema_ref}]
    }


def create_complete_schema(resource_name: str, base_schema_ref: str) -> Dict[str, Any]:
    """
    Create a complete schema that extends Base with server-generated fields.
    """
    return {
        "description": f"Complete {resource_name} schema with server-generated fields",
        "allOf": [
            {"$ref": base_schema_ref},
            {
                "type": "object",
                "required": ["id"],
                "properties": {
                    "id": {
                        "type": "string",
                        "format": "uuid",
                        "description": f"Unique identifier for the {resource_name.lower()}",
                        "readOnly": True
                    },
                    "metadata": {
                        "type": "array",
                        "items": {
                            "$ref": "#/components/schemas/Metadata"
                        },
                        "description": "Optional metadata key-value pairs",
                        "readOnly": True
                    },
                    "created_at": {
                        "type": "string",
                        "format": "date-time",
                        "maxLength": 24,
                        "description": "Creation timestamp (RFC3339)",
                        "readOnly": True
                    },
                    "modified_at": {
                        "type": "string",
                        "format": "date-time",
                        "maxLength": 24,
                        "description": "Last modification timestamp (RFC3339)",
                        "readOnly": True
                    }
                }
            }
        ]
    }


def create_list_item_schema(resource_name: str, include_fields: List[str], original_schema: Dict[str, Any]) -> Dict[str, Any]:
    """
    Create a ListItem schema with only specified fields.
    """
    list_schema = {
        "type": "object",
        "description": f"Summary information for {resource_name} in list responses",
        "required": [],
        "properties": {}
    }

    # Include specified fields from original schema
    for field_name in include_fields:
        if field_name in original_schema.get("properties", {}):
            list_schema["properties"][field_name] = original_schema["properties"][field_name].copy()
            if field_name in original_schema.get("required", []):
                list_schema["required"].append(field_name)

    # Always include standard fields
    standard_fields = {
        "id": {
            "type": "string",
            "format": "uuid",
            "description": f"Unique identifier for the {resource_name.lower()}",
            "readOnly": True
        },
        "created_at": {
            "type": "string",
            "format": "date-time",
            "maxLength": 24,
            "description": "Creation timestamp (RFC3339)",
            "readOnly": True
        },
        "modified_at": {
            "type": "string",
            "format": "date-time",
            "maxLength": 24,
            "description": "Last modification timestamp (RFC3339)",
            "readOnly": True
        }
    }

    for field_name, field_def in standard_fields.items():
        if field_name not in list_schema["properties"]:
            list_schema["properties"][field_name] = field_def
            if field_name == "id" and field_name not in list_schema["required"]:
                list_schema["required"].append(field_name)

    return list_schema


def normalize_openapi_schema(schema: Dict[str, Any]) -> Dict[str, Any]:
    """
    Apply all normalization transformations to the OpenAPI schema.
    """
    schemas = schema["components"]["schemas"]
    paths = schema["paths"]

    print("Step 1: Creating Base/Input schemas for simple resources...")

    # Process Asset
    if "Asset" in schemas:
        print("  - Processing Asset schemas...")
        asset_original = schemas["Asset"].copy()
        schemas["AssetBase"] = create_base_schema("Asset", asset_original)
        schemas["AssetInput"] = create_input_schema("Asset", "#/components/schemas/AssetBase")
        schemas["Asset"] = create_complete_schema("Asset", "#/components/schemas/AssetBase")

    # Process Document
    if "Document" in schemas:
        print("  - Processing Document schemas...")
        doc_original = schemas["Document"].copy()
        schemas["DocumentBase"] = create_base_schema("Document", doc_original)
        schemas["DocumentInput"] = create_input_schema("Document", "#/components/schemas/DocumentBase")
        schemas["Document"] = create_complete_schema("Document", "#/components/schemas/DocumentBase")

    # Process Note
    if "Note" in schemas:
        print("  - Processing Note schemas...")
        note_original = schemas["Note"].copy()
        schemas["NoteBase"] = create_base_schema("Note", note_original)
        schemas["NoteInput"] = create_input_schema("Note", "#/components/schemas/NoteBase")
        schemas["Note"] = create_complete_schema("Note", "#/components/schemas/NoteBase")

        # Create NoteListItem
        note_list_fields = ["name", "description", "metadata"]
        schemas["NoteListItem"] = create_list_item_schema("Note", note_list_fields, note_original)

    # Process Repository
    if "Repository" in schemas:
        print("  - Processing Repository schemas...")
        repo_original = schemas["Repository"].copy()
        schemas["RepositoryBase"] = create_base_schema("Repository", repo_original)
        schemas["RepositoryInput"] = create_input_schema("Repository", "#/components/schemas/RepositoryBase")
        schemas["Repository"] = create_complete_schema("Repository", "#/components/schemas/RepositoryBase")

    print("Step 2: Processing Diagram schemas (add description, create Base/Input)...")
    # Note: Diagram schema is complex due to polymorphism - will need manual handling
    # For now, just add description field to BaseDiagram
    if "BaseDiagram" in schemas and "properties" in schemas["BaseDiagram"]:
        schemas["BaseDiagram"]["properties"]["description"] = {
            "type": "string",
            "maxLength": 1024,
            "nullable": True,
            "description": "Optional description of the diagram"
        }

    print("Step 3: Removing batch endpoints...")
    # Remove batch endpoints
    batch_paths_to_remove = [
        "/threat_models/{threat_model_id}/threats/batch",
        "/threat_models/{threat_model_id}/threats/batch/patch"
    ]

    for path in batch_paths_to_remove:
        if path in paths:
            del paths[path]
            print(f"  - Removed {path}")

    print("Step 4: Adding bulk PATCH/DELETE operations...")
    # Add PATCH and DELETE methods to /threats/bulk
    bulk_threats_path = "/threat_models/{threat_model_id}/threats/bulk"
    if bulk_threats_path in paths:
        # Add PATCH method
        paths[bulk_threats_path]["patch"] = {
            "summary": "Bulk PATCH threats",
            "description": "Apply JSON Patch operations to multiple threats in a single request",
            "operationId": "bulkPatchThreatModelThreats",
            "tags": ["Threats"],
            "parameters": [
                {
                    "$ref": "#/components/parameters/ThreatModelIdParam"
                }
            ],
            "requestBody": {
                "required": True,
                "content": {
                    "application/json-patch+json": {
                        "schema": {
                            "type": "object",
                            "required": ["patches"],
                            "properties": {
                                "patches": {
                                    "type": "array",
                                    "items": {
                                        "type": "object",
                                        "required": ["id", "operations"],
                                        "properties": {
                                            "id": {
                                                "type": "string",
                                                "format": "uuid",
                                                "description": "Threat ID to patch"
                                            },
                                            "operations": {
                                                "type": "array",
                                                "items": {
                                                    "$ref": "#/components/schemas/PatchOperation"
                                                },
                                                "description": "JSON Patch operations to apply"
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            },
            "responses": {
                "200": {
                    "description": "Successfully patched threats",
                    "content": {
                        "application/json": {
                            "schema": {
                                "type": "array",
                                "items": {
                                    "$ref": "#/components/schemas/Threat"
                                }
                            }
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

        # Add DELETE method
        paths[bulk_threats_path]["delete"] = {
            "summary": "Bulk DELETE threats",
            "description": "Delete multiple threats in a single request",
            "operationId": "bulkDeleteThreatModelThreats",
            "tags": ["Threats"],
            "parameters": [
                {
                    "$ref": "#/components/parameters/ThreatModelIdParam"
                }
            ],
            "requestBody": {
                "required": True,
                "content": {
                    "application/json": {
                        "schema": {
                            "type": "object",
                            "required": ["threat_ids"],
                            "properties": {
                                "threat_ids": {
                                    "type": "array",
                                    "items": {
                                        "type": "string",
                                        "format": "uuid"
                                    },
                                    "minItems": 1,
                                    "maxItems": 20,
                                    "description": "List of threat IDs to delete"
                                }
                            }
                        }
                    }
                }
            },
            "responses": {
                "200": {
                    "description": "Successfully deleted threats",
                    "content": {
                        "application/json": {
                            "schema": {
                                "type": "object",
                                "properties": {
                                    "deleted_count": {
                                        "type": "integer",
                                        "description": "Number of threats deleted"
                                    },
                                    "deleted_ids": {
                                        "type": "array",
                                        "items": {
                                            "type": "string",
                                            "format": "uuid"
                                        },
                                        "description": "IDs of deleted threats"
                                    }
                                }
                            }
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
        print(f"  - Added PATCH and DELETE methods to {bulk_threats_path}")

    print("Step 5: Adding PATCH endpoints for simple resources...")
    # Add PATCH endpoints
    resource_paths = {
        "assets": "Asset",
        "documents": "Document",
        "notes": "Note",
        "repositories": "Repository"
    }

    for resource_plural, resource_singular in resource_paths.items():
        patch_path = f"/threat_models/{{threat_model_id}}/{resource_plural}/{{id}}"
        if patch_path in paths:
            paths[patch_path]["patch"] = {
                "summary": f"Partially update {resource_singular}",
                "description": f"Apply JSON Patch operations to partially update a {resource_singular.lower()}",
                "operationId": f"patchThreatModel{resource_singular}",
                "tags": [resource_singular + "s"],
                "parameters": [
                    {"$ref": "#/components/parameters/ThreatModelIdParam"},
                    {
                        "name": "id",
                        "in": "path",
                        "required": True,
                        "schema": {
                            "type": "string",
                            "format": "uuid"
                        },
                        "description": f"{resource_singular} ID"
                    }
                ],
                "requestBody": {
                    "required": True,
                    "content": {
                        "application/json-patch+json": {
                            "schema": {
                                "type": "array",
                                "items": {
                                    "$ref": "#/components/schemas/PatchOperation"
                                }
                            }
                        }
                    }
                },
                "responses": {
                    "200": {
                        "description": f"Successfully patched {resource_singular.lower()}",
                        "content": {
                            "application/json": {
                                "schema": {
                                    "$ref": f"#/components/schemas/{resource_singular}"
                                }
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
            print(f"  - Added PATCH method to {patch_path}")

    print("Step 6: Updating request/response schemas...")
    # Update POST/PUT endpoints to use Input schemas
    for path_url, path_item in paths.items():
        for method in ["post", "put"]:
            if method in path_item:
                operation = path_item[method]

                # Update request bodies to use Input schemas
                if "requestBody" in operation and "content" in operation["requestBody"]:
                    content = operation["requestBody"]["content"]
                    if "application/json" in content and "schema" in content["application/json"]:
                        schema_ref = content["application/json"]["schema"]

                        # Replace schema references with Input variants
                        if "$ref" in schema_ref:
                            ref = schema_ref["$ref"]
                            for resource in ["Asset", "Document", "Note", "Repository"]:
                                if ref.endswith(f"/{resource}"):
                                    schema_ref["$ref"] = f"#/components/schemas/{resource}Input"
                                    print(f"  - Updated {method.upper()} {path_url} to use {resource}Input")
                                    break

    # Update Notes list endpoint to return NoteListItem
    notes_list_path = "/threat_models/{threat_model_id}/notes"
    if notes_list_path in paths and "get" in paths[notes_list_path]:
        get_op = paths[notes_list_path]["get"]
        if "responses" in get_op and "200" in get_op["responses"]:
            response_content = get_op["responses"]["200"].get("content", {}).get("application/json", {})
            if "schema" in response_content and "items" in response_content["schema"]:
                response_content["schema"]["items"]["$ref"] = "#/components/schemas/NoteListItem"
                print(f"  - Updated GET {notes_list_path} to return NoteListItem")

    print("Step 7: Adding documentation...")
    # Update API description with design rationale
    schema["info"]["description"] = schema["info"].get("description", "") + """

## API Design v1.0.0

### Authorization Model
TMI uses hierarchical authorization: access control is defined at the ThreatModel level via the authorization field (readers, writers, owners). All child resources (Assets, Diagrams, Documents, Notes, Repositories, Threats) inherit permissions from their parent ThreatModel. This simplifies permission management and ensures consistent access control.

### Bulk Operations
Notes and Diagrams do not support bulk operations due to their unique creation workflows and lack of valid bulk use cases. All other resources (Threats, Assets, Documents, Repositories) support full bulk operations: POST (create), PUT (upsert), PATCH (partial update), DELETE (batch delete).

All resources support bulk metadata operations regardless of resource-level bulk support.

### List Response Strategy
- ThreatModels return summary information (TMListItem) because they contain many child objects that can be large.
- Diagrams return summary information (DiagramListItem) because diagram data (cells, images) can be large.
- Notes return summary information (NoteListItem) because the content field can be large.
- Threats, Assets, Documents, Repositories return full schemas as they are relatively small and static.

### PATCH Support
All resources support PATCH for partial updates using JSON Patch (RFC 6902). This is particularly useful for:
- Assets: Array field updates (affected_assets, trust_boundaries) ensuring no duplicates
- Notes: Updating name/description without changing content field
- All resources: Efficient updates without full object replacement
"""

    return schema


def main():
    """Main execution function."""
    print("TMI OpenAPI Schema Normalization v1.0.0\n")

    # File paths
    schema_path = Path(__file__).parent.parent / "docs" / "reference" / "apis" / "tmi-openapi.json"

    if not schema_path.exists():
        print(f"Error: OpenAPI schema not found at {schema_path}")
        sys.exit(1)

    print(f"Loading schema from {schema_path}...")
    schema = load_openapi_schema(str(schema_path))

    print("Normalizing schema...")
    normalized_schema = normalize_openapi_schema(schema)

    print(f"Saving normalized schema to {schema_path}...")
    save_openapi_schema(normalized_schema, str(schema_path))

    print("\nSchema normalization complete!")
    print("Next steps:")
    print("  1. Validate schema: make validate-openapi")
    print("  2. Regenerate API code: make generate-api")


if __name__ == "__main__":
    main()
