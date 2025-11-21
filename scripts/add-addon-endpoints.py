#!/usr/bin/env python3
"""
Add addon endpoints to the TMI OpenAPI specification.

This script adds all addon-related endpoints, schemas, and security requirements
to the OpenAPI 3.0 specification following the design document.
"""
# /// script
# dependencies = []
# ///

import json
import sys
from pathlib import Path

def add_addon_schemas(spec):
    """Add addon-related schemas to the components section."""

    schemas = spec.setdefault("components", {}).setdefault("schemas", {})

    # CreateAddonRequest schema
    schemas["CreateAddonRequest"] = {
        "type": "object",
        "required": ["name", "webhook_id"],
        "properties": {
            "name": {
                "type": "string",
                "maxLength": 255,
                "description": "Display name for the add-on"
            },
            "webhook_id": {
                "type": "string",
                "format": "uuid",
                "description": "UUID of the associated webhook subscription"
            },
            "description": {
                "type": "string",
                "description": "Description of what the add-on does"
            },
            "icon": {
                "type": "string",
                "maxLength": 60,
                "pattern": "^(material-symbols:[a-z]([a-z0-9_]*[a-z0-9])?|fa-[a-z]([a-z]*[a-z])?(\\-[a-z]+)? fa-([a-z]+)(-[a-z]+)*)$",
                "description": "Icon identifier (Material Symbols or FontAwesome format)"
            },
            "objects": {
                "type": "array",
                "items": {
                    "type": "string",
                    "enum": ["threat_model", "diagram", "asset", "threat", "document", "note", "repository", "metadata"]
                },
                "description": "TMI object types this add-on can operate on"
            },
            "threat_model_id": {
                "type": "string",
                "format": "uuid",
                "description": "Optional: Scope add-on to specific threat model"
            }
        }
    }

    # AddonResponse schema
    schemas["AddonResponse"] = {
        "type": "object",
        "required": ["id", "created_at", "name", "webhook_id"],
        "properties": {
            "id": {
                "type": "string",
                "format": "uuid",
                "description": "Add-on identifier"
            },
            "created_at": {
                "type": "string",
                "format": "date-time",
                "description": "Creation timestamp"
            },
            "name": {
                "type": "string",
                "description": "Display name"
            },
            "webhook_id": {
                "type": "string",
                "format": "uuid",
                "description": "Associated webhook subscription ID"
            },
            "description": {
                "type": "string",
                "description": "Add-on description"
            },
            "icon": {
                "type": "string",
                "description": "Icon identifier"
            },
            "objects": {
                "type": "array",
                "items": {
                    "type": "string"
                },
                "description": "Supported TMI object types"
            },
            "threat_model_id": {
                "type": "string",
                "format": "uuid",
                "description": "Threat model scope (if scoped)"
            }
        }
    }

    # ListAddonsResponse schema
    schemas["ListAddonsResponse"] = {
        "type": "object",
        "required": ["addons", "total", "limit", "offset"],
        "properties": {
            "addons": {
                "type": "array",
                "items": {
                    "$ref": "#/components/schemas/AddonResponse"
                }
            },
            "total": {
                "type": "integer",
                "description": "Total number of add-ons matching criteria"
            },
            "limit": {
                "type": "integer",
                "description": "Pagination limit"
            },
            "offset": {
                "type": "integer",
                "description": "Pagination offset"
            }
        }
    }

    # InvokeAddonRequest schema
    schemas["InvokeAddonRequest"] = {
        "type": "object",
        "required": ["threat_model_id"],
        "properties": {
            "threat_model_id": {
                "type": "string",
                "format": "uuid",
                "description": "Threat model context for invocation"
            },
            "object_type": {
                "type": "string",
                "enum": ["threat_model", "diagram", "asset", "threat", "document", "note", "repository", "metadata"],
                "description": "Optional: Specific object type to operate on"
            },
            "object_id": {
                "type": "string",
                "format": "uuid",
                "description": "Optional: Specific object ID to operate on"
            },
            "payload": {
                "type": "object",
                "description": "User-provided data for the add-on (max 1KB JSON-serialized)",
                "additionalProperties": True
            }
        }
    }

    # InvokeAddonResponse schema
    schemas["InvokeAddonResponse"] = {
        "type": "object",
        "required": ["invocation_id", "status", "created_at"],
        "properties": {
            "invocation_id": {
                "type": "string",
                "format": "uuid",
                "description": "Invocation identifier for tracking"
            },
            "status": {
                "type": "string",
                "enum": ["pending", "in_progress", "completed", "failed"],
                "description": "Current invocation status"
            },
            "created_at": {
                "type": "string",
                "format": "date-time",
                "description": "Invocation creation timestamp"
            }
        }
    }

    # InvocationResponse schema
    schemas["InvocationResponse"] = {
        "type": "object",
        "required": ["id", "addon_id", "threat_model_id", "invoked_by", "status", "status_percent", "created_at", "status_updated_at"],
        "properties": {
            "id": {
                "type": "string",
                "format": "uuid",
                "description": "Invocation identifier"
            },
            "addon_id": {
                "type": "string",
                "format": "uuid",
                "description": "Add-on that was invoked"
            },
            "threat_model_id": {
                "type": "string",
                "format": "uuid",
                "description": "Threat model context"
            },
            "object_type": {
                "type": "string",
                "description": "Object type (if specified)"
            },
            "object_id": {
                "type": "string",
                "format": "uuid",
                "description": "Object ID (if specified)"
            },
            "invoked_by": {
                "type": "string",
                "format": "uuid",
                "description": "User who invoked the add-on"
            },
            "payload": {
                "type": "string",
                "description": "JSON-encoded payload"
            },
            "status": {
                "type": "string",
                "enum": ["pending", "in_progress", "completed", "failed"],
                "description": "Current status"
            },
            "status_percent": {
                "type": "integer",
                "minimum": 0,
                "maximum": 100,
                "description": "Progress percentage (0-100)"
            },
            "status_message": {
                "type": "string",
                "description": "Optional status description"
            },
            "created_at": {
                "type": "string",
                "format": "date-time",
                "description": "Creation timestamp"
            },
            "status_updated_at": {
                "type": "string",
                "format": "date-time",
                "description": "Last status update timestamp"
            }
        }
    }

    # ListInvocationsResponse schema
    schemas["ListInvocationsResponse"] = {
        "type": "object",
        "required": ["invocations", "total", "limit", "offset"],
        "properties": {
            "invocations": {
                "type": "array",
                "items": {
                    "$ref": "#/components/schemas/InvocationResponse"
                }
            },
            "total": {
                "type": "integer",
                "description": "Total number of invocations"
            },
            "limit": {
                "type": "integer",
                "description": "Pagination limit"
            },
            "offset": {
                "type": "integer",
                "description": "Pagination offset"
            }
        }
    }

    # UpdateInvocationStatusRequest schema
    schemas["UpdateInvocationStatusRequest"] = {
        "type": "object",
        "required": ["status"],
        "properties": {
            "status": {
                "type": "string",
                "enum": ["in_progress", "completed", "failed"],
                "description": "New status (cannot transition back to pending)"
            },
            "status_percent": {
                "type": "integer",
                "minimum": 0,
                "maximum": 100,
                "description": "Progress percentage"
            },
            "status_message": {
                "type": "string",
                "description": "Optional status description"
            }
        }
    }

    # UpdateInvocationStatusResponse schema
    schemas["UpdateInvocationStatusResponse"] = {
        "type": "object",
        "required": ["id", "status", "status_percent", "status_updated_at"],
        "properties": {
            "id": {
                "type": "string",
                "format": "uuid",
                "description": "Invocation identifier"
            },
            "status": {
                "type": "string",
                "enum": ["pending", "in_progress", "completed", "failed"],
                "description": "Current status"
            },
            "status_percent": {
                "type": "integer",
                "minimum": 0,
                "maximum": 100,
                "description": "Progress percentage"
            },
            "status_updated_at": {
                "type": "string",
                "format": "date-time",
                "description": "Status update timestamp"
            }
        }
    }


def add_addon_paths(spec):
    """Add addon-related paths to the OpenAPI spec."""

    paths = spec.setdefault("paths", {})

    # Standard rate limit for authenticated operations
    user_rate_limit = {
        "scope": "user",
        "tier": "resource-operations",
        "limits": [
            {
                "type": "requests_per_minute",
                "default": 100,
                "configurable": True,
                "quota_source": "user_api_quotas"
            }
        ]
    }

    # POST /addons - Create add-on (admin only)
    paths["/addons"] = {
        "post": {
            "operationId": "createAddon",
            "summary": "Create add-on",
            "description": "Create a new add-on (administrators only)",
            "tags": ["Addons"],
            "security": [{"BearerAuth": []}],
            "x-rate-limit": user_rate_limit,
            "requestBody": {
                "required": True,
                "content": {
                    "application/json": {
                        "schema": {
                            "$ref": "#/components/schemas/CreateAddonRequest"
                        }
                    }
                }
            },
            "responses": {
                "201": {
                    "description": "Add-on created successfully",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/AddonResponse"
                            }
                        }
                    }
                },
                "400": {
                    "description": "Bad request - validation failed",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "401": {
                    "description": "Unauthorized - authentication required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "403": {
                    "description": "Forbidden - administrator access required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "404": {
                    "description": "Webhook not found",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        },
        "get": {
            "operationId": "listAddons",
            "summary": "List add-ons",
            "description": "List all add-ons (authenticated users)",
            "tags": ["Addons"],
            "security": [{"BearerAuth": []}],
            "x-rate-limit": user_rate_limit,
            "parameters": [
                {
                    "name": "limit",
                    "in": "query",
                    "schema": {
                        "type": "integer",
                        "default": 50,
                        "minimum": 1,
                        "maximum": 500
                    },
                    "description": "Number of results per page"
                },
                {
                    "name": "offset",
                    "in": "query",
                    "schema": {
                        "type": "integer",
                        "default": 0,
                        "minimum": 0
                    },
                    "description": "Pagination offset"
                },
                {
                    "name": "threat_model_id",
                    "in": "query",
                    "schema": {
                        "type": "string",
                        "format": "uuid"
                    },
                    "description": "Filter by threat model"
                }
            ],
            "responses": {
                "200": {
                    "description": "List of add-ons",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/ListAddonsResponse"
                            }
                        }
                    }
                },
                "401": {
                    "description": "Unauthorized - authentication required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        }
    }

    # GET /addons/{id} - Get single add-on
    paths["/addons/{id}"] = {
        "get": {
            "operationId": "getAddon",
            "summary": "Get add-on",
            "description": "Get a single add-on by ID",
            "tags": ["Addons"],
            "security": [{"BearerAuth": []}],
            "x-rate-limit": user_rate_limit,
            "parameters": [
                {
                    "name": "id",
                    "in": "path",
                    "required": True,
                    "schema": {
                        "type": "string",
                        "format": "uuid"
                    },
                    "description": "Add-on identifier"
                }
            ],
            "responses": {
                "200": {
                    "description": "Add-on details",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/AddonResponse"
                            }
                        }
                    }
                },
                "401": {
                    "description": "Unauthorized - authentication required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "404": {
                    "description": "Add-on not found",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        },
        "delete": {
            "operationId": "deleteAddon",
            "summary": "Delete add-on",
            "description": "Delete an add-on (administrators only)",
            "tags": ["Addons"],
            "security": [{"BearerAuth": []}],
            "x-rate-limit": user_rate_limit,
            "parameters": [
                {
                    "name": "id",
                    "in": "path",
                    "required": True,
                    "schema": {
                        "type": "string",
                        "format": "uuid"
                    },
                    "description": "Add-on identifier"
                }
            ],
            "responses": {
                "204": {
                    "description": "Add-on deleted successfully"
                },
                "401": {
                    "description": "Unauthorized - authentication required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "403": {
                    "description": "Forbidden - administrator access required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "404": {
                    "description": "Add-on not found",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "409": {
                    "description": "Conflict - cannot delete add-on with active invocations",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        }
    }

    # POST /addons/{id}/invoke - Invoke add-on
    paths["/addons/{id}/invoke"] = {
        "post": {
            "operationId": "invokeAddon",
            "summary": "Invoke add-on",
            "description": "Trigger an add-on invocation (authenticated users)",
            "tags": ["Addons"],
            "security": [{"BearerAuth": []}],
            "x-rate-limit": user_rate_limit,
            "parameters": [
                {
                    "name": "id",
                    "in": "path",
                    "required": True,
                    "schema": {
                        "type": "string",
                        "format": "uuid"
                    },
                    "description": "Add-on identifier"
                }
            ],
            "requestBody": {
                "required": True,
                "content": {
                    "application/json": {
                        "schema": {
                            "$ref": "#/components/schemas/InvokeAddonRequest"
                        }
                    }
                }
            },
            "responses": {
                "202": {
                    "description": "Invocation accepted and queued",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/InvokeAddonResponse"
                            }
                        }
                    }
                },
                "400": {
                    "description": "Bad request - validation failed or payload too large",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "401": {
                    "description": "Unauthorized - authentication required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "404": {
                    "description": "Add-on not found",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "429": {
                    "description": "Rate limit exceeded (quota: 1 active invocation or 10 per hour)",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        }
    }

    # GET /invocations - List invocations
    paths["/invocations"] = {
        "get": {
            "operationId": "listInvocations",
            "summary": "List invocations",
            "description": "List add-on invocations (users see own, admins see all)",
            "tags": ["Addons"],
            "security": [{"BearerAuth": []}],
            "x-rate-limit": user_rate_limit,
            "parameters": [
                {
                    "name": "limit",
                    "in": "query",
                    "schema": {
                        "type": "integer",
                        "default": 50,
                        "minimum": 1,
                        "maximum": 500
                    },
                    "description": "Number of results per page"
                },
                {
                    "name": "offset",
                    "in": "query",
                    "schema": {
                        "type": "integer",
                        "default": 0,
                        "minimum": 0
                    },
                    "description": "Pagination offset"
                },
                {
                    "name": "status",
                    "in": "query",
                    "schema": {
                        "type": "string",
                        "enum": ["pending", "in_progress", "completed", "failed"]
                    },
                    "description": "Filter by status"
                },
                {
                    "name": "addon_id",
                    "in": "query",
                    "schema": {
                        "type": "string",
                        "format": "uuid"
                    },
                    "description": "Filter by add-on"
                }
            ],
            "responses": {
                "200": {
                    "description": "List of invocations",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/ListInvocationsResponse"
                            }
                        }
                    }
                },
                "401": {
                    "description": "Unauthorized - authentication required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        }
    }

    # GET /invocations/{id} - Get single invocation
    paths["/invocations/{id}"] = {
        "get": {
            "operationId": "getInvocation",
            "summary": "Get invocation",
            "description": "Get a single invocation by ID (own invocations or admin)",
            "tags": ["Addons"],
            "security": [{"BearerAuth": []}],
            "x-rate-limit": user_rate_limit,
            "parameters": [
                {
                    "name": "id",
                    "in": "path",
                    "required": True,
                    "schema": {
                        "type": "string",
                        "format": "uuid"
                    },
                    "description": "Invocation identifier"
                }
            ],
            "responses": {
                "200": {
                    "description": "Invocation details",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/InvocationResponse"
                            }
                        }
                    }
                },
                "401": {
                    "description": "Unauthorized - authentication required",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "403": {
                    "description": "Forbidden - not your invocation and not admin",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "404": {
                    "description": "Invocation not found or expired",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        }
    }

    # POST /invocations/{id}/status - Update invocation status (webhook callback, HMAC auth)
    paths["/invocations/{id}/status"] = {
        "post": {
            "operationId": "updateInvocationStatus",
            "summary": "Update invocation status",
            "description": "Update invocation status (webhook callback with HMAC authentication)",
            "tags": ["Addons"],
            "security": [],  # HMAC authentication handled in middleware
            "parameters": [
                {
                    "name": "id",
                    "in": "path",
                    "required": True,
                    "schema": {
                        "type": "string",
                        "format": "uuid"
                    },
                    "description": "Invocation identifier"
                },
                {
                    "name": "X-Webhook-Signature",
                    "in": "header",
                    "required": True,
                    "schema": {
                        "type": "string"
                    },
                    "description": "HMAC-SHA256 signature (format: sha256={hex_signature})"
                }
            ],
            "requestBody": {
                "required": True,
                "content": {
                    "application/json": {
                        "schema": {
                            "$ref": "#/components/schemas/UpdateInvocationStatusRequest"
                        }
                    }
                }
            },
            "responses": {
                "200": {
                    "description": "Status updated successfully",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/UpdateInvocationStatusResponse"
                            }
                        }
                    }
                },
                "400": {
                    "description": "Bad request - invalid status transition or validation failed",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "401": {
                    "description": "Unauthorized - invalid HMAC signature",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "404": {
                    "description": "Invocation not found or expired",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                },
                "409": {
                    "description": "Conflict - invalid status transition",
                    "content": {
                        "application/json": {
                            "schema": {
                                "$ref": "#/components/schemas/Error"
                            }
                        }
                    }
                }
            }
        }
    }


def main():
    """Main entry point."""
    openapi_path = Path(__file__).parent.parent / "docs" / "reference" / "apis" / "tmi-openapi.json"

    if not openapi_path.exists():
        print(f"Error: OpenAPI spec not found at {openapi_path}", file=sys.stderr)
        return 1

    # Load the OpenAPI spec
    print(f"Loading OpenAPI spec from {openapi_path}")
    with open(openapi_path, 'r') as f:
        spec = json.load(f)

    # Add addon schemas and paths
    print("Adding addon schemas...")
    add_addon_schemas(spec)

    print("Adding addon paths...")
    add_addon_paths(spec)

    # Write back with pretty formatting
    print(f"Writing updated spec to {openapi_path}")
    with open(openapi_path, 'w') as f:
        json.dump(spec, f, indent=2)
        f.write('\n')  # Add trailing newline

    print("✓ Successfully added addon endpoints to OpenAPI specification")
    return 0


if __name__ == "__main__":
    sys.exit(main())
