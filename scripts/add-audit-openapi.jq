# Add audit trail schemas and endpoints to the OpenAPI spec

# Add schemas
.components.schemas.AuditActor = {
  "type": "object",
  "description": "Denormalized user information stored with audit entries. Persists after user deletion.",
  "required": ["email", "provider", "provider_id", "display_name"],
  "properties": {
    "email": {
      "type": "string",
      "format": "email",
      "description": "User email at the time of the action"
    },
    "provider": {
      "type": "string",
      "description": "Identity provider (e.g., google, github, tmi)"
    },
    "provider_id": {
      "type": "string",
      "description": "Provider-specific user identifier"
    },
    "display_name": {
      "type": "string",
      "description": "User display name at the time of the action"
    }
  }
}
|
.components.schemas.AuditEntry = {
  "type": "object",
  "description": "An entry in the audit trail recording a mutation to an entity",
  "required": ["id", "threat_model_id", "object_type", "object_id", "change_type", "actor", "created_at"],
  "properties": {
    "id": {
      "type": "string",
      "format": "uuid",
      "description": "Unique identifier for the audit entry"
    },
    "threat_model_id": {
      "type": "string",
      "format": "uuid",
      "description": "ID of the threat model this audit entry belongs to"
    },
    "object_type": {
      "type": "string",
      "enum": ["threat_model", "diagram", "threat", "asset", "document", "note", "repository"],
      "description": "Type of the entity that was mutated"
    },
    "object_id": {
      "type": "string",
      "format": "uuid",
      "description": "ID of the entity that was mutated"
    },
    "version": {
      "type": "integer",
      "nullable": true,
      "description": "Version number. Null if the version snapshot has been pruned and rollback is no longer available."
    },
    "change_type": {
      "type": "string",
      "enum": ["created", "updated", "patched", "deleted", "rolled_back"],
      "description": "Type of mutation"
    },
    "actor": {
      "$ref": "#/components/schemas/AuditActor"
    },
    "change_summary": {
      "type": "string",
      "nullable": true,
      "description": "Human-readable summary of what changed"
    },
    "created_at": {
      "type": "string",
      "format": "date-time",
      "description": "When the mutation occurred"
    }
  }
}
|
.components.schemas.ListAuditTrailResponse = {
  "type": "object",
  "description": "Paginated list of audit trail entries",
  "required": ["audit_entries", "total", "limit", "offset"],
  "properties": {
    "audit_entries": {
      "type": "array",
      "items": {
        "$ref": "#/components/schemas/AuditEntry"
      }
    },
    "total": {
      "type": "integer",
      "description": "Total number of matching audit entries"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of entries returned"
    },
    "offset": {
      "type": "integer",
      "description": "Offset from the beginning of the result set"
    }
  }
}
|
.components.schemas.RollbackResponse = {
  "type": "object",
  "description": "Result of a rollback operation",
  "required": ["audit_entry"],
  "properties": {
    "restored_entity": {
      "type": "object",
      "description": "The entity restored to its previous state"
    },
    "audit_entry": {
      "$ref": "#/components/schemas/AuditEntry",
      "description": "The new audit entry recording the rollback"
    }
  }
}
|

# Add parameters
.components.parameters.AuditEntryId = {
  "name": "entry_id",
  "in": "path",
  "required": true,
  "description": "Unique identifier of the audit entry",
  "schema": {
    "type": "string",
    "format": "uuid"
  }
}
|
.components.parameters.AuditObjectType = {
  "name": "object_type",
  "in": "query",
  "required": false,
  "description": "Filter by object type",
  "schema": {
    "type": "string",
    "enum": ["threat_model", "diagram", "threat", "asset", "document", "note", "repository"]
  }
}
|
.components.parameters.AuditChangeType = {
  "name": "change_type",
  "in": "query",
  "required": false,
  "description": "Filter by change type",
  "schema": {
    "type": "string",
    "enum": ["created", "updated", "patched", "deleted", "rolled_back"]
  }
}
|
.components.parameters.AuditActorEmail = {
  "name": "actor_email",
  "in": "query",
  "required": false,
  "description": "Filter by actor email",
  "schema": {
    "type": "string",
    "format": "email"
  }
}
|
.components.parameters.AuditAfter = {
  "name": "after",
  "in": "query",
  "required": false,
  "description": "Filter entries after this timestamp (ISO 8601)",
  "schema": {
    "type": "string",
    "format": "date-time"
  }
}
|
.components.parameters.AuditBefore = {
  "name": "before",
  "in": "query",
  "required": false,
  "description": "Filter entries before this timestamp (ISO 8601)",
  "schema": {
    "type": "string",
    "format": "date-time"
  }
}
|

# Define reusable audit trail responses
def audit_trail_get_operation(tag; summary; operationId):
  {
    "tags": [tag],
    "summary": summary,
    "description": "Returns a paginated list of audit trail entries",
    "operationId": operationId,
    "security": [{"bearerAuth": []}],
    "parameters": [
      {"$ref": "#/components/parameters/ThreatModelId"},
      {"$ref": "#/components/parameters/PaginationLimit"},
      {"$ref": "#/components/parameters/PaginationOffset"},
      {"$ref": "#/components/parameters/AuditObjectType"},
      {"$ref": "#/components/parameters/AuditChangeType"},
      {"$ref": "#/components/parameters/AuditActorEmail"},
      {"$ref": "#/components/parameters/AuditAfter"},
      {"$ref": "#/components/parameters/AuditBefore"}
    ],
    "responses": {
      "200": {
        "description": "List of audit trail entries",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ListAuditTrailResponse"}
          }
        }
      },
      "401": {"$ref": "#/components/responses/Error"},
      "403": {"$ref": "#/components/responses/Error"},
      "404": {"$ref": "#/components/responses/Error"},
      "429": {"$ref": "#/components/responses/TooManyRequests"},
      "500": {"$ref": "#/components/responses/Error"}
    }
  };

def sub_resource_audit_get(tag; summary; operationId; param_ref):
  {
    "tags": [tag],
    "summary": summary,
    "description": "Returns a paginated list of audit trail entries for a specific resource",
    "operationId": operationId,
    "security": [{"bearerAuth": []}],
    "parameters": [
      {"$ref": "#/components/parameters/ThreatModelId"},
      {"$ref": param_ref},
      {"$ref": "#/components/parameters/PaginationLimit"},
      {"$ref": "#/components/parameters/PaginationOffset"}
    ],
    "responses": {
      "200": {
        "description": "List of audit trail entries",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/ListAuditTrailResponse"}
          }
        }
      },
      "401": {"$ref": "#/components/responses/Error"},
      "403": {"$ref": "#/components/responses/Error"},
      "404": {"$ref": "#/components/responses/Error"},
      "429": {"$ref": "#/components/responses/TooManyRequests"},
      "500": {"$ref": "#/components/responses/Error"}
    }
  };

# Add TM audit trail endpoint
.paths["/threat_models/{threat_model_id}/audit_trail"] = {
  "get": audit_trail_get_operation("Audit Trail"; "List audit trail for a threat model and all sub-objects"; "getThreatModelAuditTrail")
}
|

# Add single audit entry endpoint
.paths["/threat_models/{threat_model_id}/audit_trail/{entry_id}"] = {
  "get": {
    "tags": ["Audit Trail"],
    "summary": "Get a single audit trail entry",
    "description": "Returns a single audit trail entry by ID",
    "operationId": "getAuditEntry",
    "security": [{"bearerAuth": []}],
    "parameters": [
      {"$ref": "#/components/parameters/ThreatModelId"},
      {"$ref": "#/components/parameters/AuditEntryId"}
    ],
    "responses": {
      "200": {
        "description": "Audit trail entry",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/AuditEntry"}
          }
        }
      },
      "401": {"$ref": "#/components/responses/Error"},
      "403": {"$ref": "#/components/responses/Error"},
      "404": {"$ref": "#/components/responses/Error"},
      "429": {"$ref": "#/components/responses/TooManyRequests"},
      "500": {"$ref": "#/components/responses/Error"}
    }
  }
}
|

# Add rollback endpoint
.paths["/threat_models/{threat_model_id}/audit_trail/{entry_id}/rollback"] = {
  "post": {
    "tags": ["Audit Trail"],
    "summary": "Rollback an entity to a previous version",
    "description": "Restores an entity to the state captured in the specified audit entry's version snapshot. Creates a new audit entry recording the rollback. Returns 410 Gone if the version snapshot has been pruned.",
    "operationId": "rollbackToVersion",
    "security": [{"bearerAuth": []}],
    "parameters": [
      {"$ref": "#/components/parameters/ThreatModelId"},
      {"$ref": "#/components/parameters/AuditEntryId"}
    ],
    "responses": {
      "200": {
        "description": "Rollback successful",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/RollbackResponse"}
          }
        }
      },
      "401": {"$ref": "#/components/responses/Error"},
      "403": {"$ref": "#/components/responses/Error"},
      "404": {"$ref": "#/components/responses/Error"},
      "410": {
        "description": "Version snapshot has been pruned; rollback is no longer available",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/Error"}
          }
        }
      },
      "429": {"$ref": "#/components/responses/TooManyRequests"},
      "500": {"$ref": "#/components/responses/Error"}
    }
  }
}
|

# Add sub-resource audit trail endpoints
.paths["/threat_models/{threat_model_id}/diagrams/{diagram_id}/audit_trail"] = {
  "get": sub_resource_audit_get("Audit Trail"; "List audit trail for a diagram"; "getDiagramAuditTrail"; "#/components/parameters/DiagramId")
}
|
.paths["/threat_models/{threat_model_id}/threats/{threat_id}/audit_trail"] = {
  "get": sub_resource_audit_get("Audit Trail"; "List audit trail for a threat"; "getThreatAuditTrail"; "#/components/parameters/ThreatId")
}
|
.paths["/threat_models/{threat_model_id}/assets/{asset_id}/audit_trail"] = {
  "get": sub_resource_audit_get("Audit Trail"; "List audit trail for an asset"; "getAssetAuditTrail"; "#/components/parameters/AssetId")
}
|
.paths["/threat_models/{threat_model_id}/documents/{document_id}/audit_trail"] = {
  "get": sub_resource_audit_get("Audit Trail"; "List audit trail for a document"; "getDocumentAuditTrail"; "#/components/parameters/DocumentId")
}
|
.paths["/threat_models/{threat_model_id}/notes/{note_id}/audit_trail"] = {
  "get": sub_resource_audit_get("Audit Trail"; "List audit trail for a note"; "getNoteAuditTrail"; "#/components/parameters/NoteId")
}
|
.paths["/threat_models/{threat_model_id}/repositories/{repository_id}/audit_trail"] = {
  "get": sub_resource_audit_get("Audit Trail"; "List audit trail for a repository"; "getRepositoryAuditTrail"; "#/components/parameters/RepositoryId")
}

# Add "Audit Trail" to tags
| .tags += [{"name": "Audit Trail", "description": "Audit trail and version history for threat model entities"}]
