# add-tombstone-openapi.jq
# Adds tombstoning (soft delete) support to the OpenAPI spec:
# 1. deleted_at property to response schemas
# 2. include_deleted query parameter
# 3. "restored" to change_type enum

# Define the deleted_at property
def deleted_at_prop:
  {
    "type": "string",
    "format": "date-time",
    "description": "Deletion timestamp (RFC3339). Present only on soft-deleted entities within the tombstone retention period.",
    "maxLength": 32,
    "readOnly": true,
    "nullable": true,
    "pattern": "^[0-9]*-[0-9]*-[0-9]*T[0-9]*:[0-9]*:[0-9]*(\\.[0-9]*)?(Z|[+-][0-9]*:[0-9]*)$"
  };

# Define the include_deleted query parameter
def include_deleted_param:
  {
    "name": "include_deleted",
    "in": "query",
    "description": "Include soft-deleted (tombstoned) entities in the response. Requires owner or admin role.",
    "required": false,
    "schema": {
      "type": "boolean",
      "default": false
    }
  };

# Add deleted_at to ThreatModel response schema (in allOf[1].properties)
.components.schemas.ThreatModel.allOf[1].properties.deleted_at = deleted_at_prop

# Add deleted_at to TMListItem properties
| .components.schemas.TMListItem.properties.deleted_at = deleted_at_prop

# Add deleted_at to Asset response schema (in allOf[1].properties)
| .components.schemas.Asset.allOf[1].properties.deleted_at = deleted_at_prop

# Add deleted_at to Threat response schema (in allOf[1].properties)
| .components.schemas.Threat.allOf[1].properties.deleted_at = deleted_at_prop

# Add deleted_at to Document response schema (in allOf[1].properties)
| .components.schemas.Document.allOf[1].properties.deleted_at = deleted_at_prop

# Add deleted_at to Note response schema (in allOf[1].properties)
| .components.schemas.Note.allOf[1].properties.deleted_at = deleted_at_prop

# Add deleted_at to NoteListItem properties
| .components.schemas.NoteListItem.properties.deleted_at = deleted_at_prop

# Add deleted_at to Repository response schema (in allOf[1].properties)
| .components.schemas.Repository.allOf[1].properties.deleted_at = deleted_at_prop

# Add deleted_at to BaseDiagram (diagrams use BaseDiagram -> DfdDiagram -> Diagram)
| .components.schemas.BaseDiagram.properties.deleted_at = deleted_at_prop

# Add deleted_at to DiagramListItem
| .components.schemas.DiagramListItem.properties.deleted_at = deleted_at_prop

# Add include_deleted parameter to components.parameters
| .components.parameters.IncludeDeletedQueryParam = include_deleted_param

# Add include_deleted parameter reference to threat_models list endpoint
| .paths["/threat_models"].get.parameters += [{"$ref": "#/components/parameters/IncludeDeletedQueryParam"}]

# Add include_deleted to sub-resource list endpoints
| .paths["/threat_models/{threat_model_id}/diagrams"].get.parameters += [{"$ref": "#/components/parameters/IncludeDeletedQueryParam"}]
| .paths["/threat_models/{threat_model_id}/threats"].get.parameters += [{"$ref": "#/components/parameters/IncludeDeletedQueryParam"}]
| .paths["/threat_models/{threat_model_id}/assets"].get.parameters += [{"$ref": "#/components/parameters/IncludeDeletedQueryParam"}]
| .paths["/threat_models/{threat_model_id}/documents"].get.parameters += [{"$ref": "#/components/parameters/IncludeDeletedQueryParam"}]
| .paths["/threat_models/{threat_model_id}/notes"].get.parameters += [{"$ref": "#/components/parameters/IncludeDeletedQueryParam"}]
| .paths["/threat_models/{threat_model_id}/repositories"].get.parameters += [{"$ref": "#/components/parameters/IncludeDeletedQueryParam"}]

# Add "restored" to AuditEntry change_type enum
| .components.schemas.AuditEntry.properties.change_type.enum += ["restored"]

# Add "restored" to AuditChangeType parameter enum
| .components.parameters.AuditChangeType.schema.enum += ["restored"]
