# Update OpenAPI spec for correct metadata REST semantics
# - Bump version to 1.2.0
# - Rename PUT bulk operationIds from bulkUpsert*/bulkUpdate* to bulkReplace*
# - Update PUT bulk descriptions for replace semantics
# - Add PATCH bulk endpoints with upsert semantics (bulkUpsert* operationIds)
# - Add 409 responses to POST single and POST bulk endpoints
# - Update POST descriptions to clarify create-only semantics

# Define reusable 409 response with rate-limit headers
def conflict_response(desc):
  {
    "description": desc,
    "content": {
      "application/json": {
        "schema": { "$ref": "#/components/schemas/Error" }
      }
    },
    "headers": {
      "X-RateLimit-Limit": {
        "description": "Maximum number of requests allowed in the current time window",
        "schema": {
          "type": "integer",
          "example": 1000
        }
      },
      "X-RateLimit-Remaining": {
        "description": "Number of requests remaining in the current time window",
        "schema": {
          "type": "integer",
          "example": 999
        }
      },
      "X-RateLimit-Reset": {
        "description": "Unix epoch seconds when the rate limit window resets",
        "schema": {
          "type": "integer",
          "example": 1735689600
        }
      }
    }
  };

# Step 1: Bump version
.info.version = "1.2.0"

# Step 2: Process all metadata/bulk paths
| .paths |= with_entries(
  if (.key | test("metadata/bulk$")) then
    .value |= (
      # Capture the existing PUT for cloning to PATCH
      . as $orig |

      # --- Transform PUT to replace semantics ---
      .put.summary = (.put.summary | gsub("(?i)upsert"; "replace") | gsub("(?i)update"; "replace")) |
      .put.description = "Replaces all metadata for the entity. All existing metadata is deleted and replaced with the provided set. To clear all metadata, send an empty array." |
      .put.operationId = (.put.operationId | gsub("bulkUpsert"; "bulkReplace") | gsub("bulkUpdate"; "bulkReplace")) |
      .put.requestBody.description = "Complete set of metadata key-value pairs to replace existing metadata" |
      .put.responses."200".description = "Metadata replaced successfully" |

      # --- Add PATCH with upsert semantics (clone from original PUT) ---
      .patch = ($orig.put |
        .summary = (.summary | gsub("(?i)upsert"; "upsert") | gsub("(?i)update"; "upsert")) |
        .description = "Creates or updates only the provided metadata keys. Keys not included in the request are left unchanged. This is a merge/upsert operation." |
        .operationId = (.operationId | gsub("bulkUpdate"; "bulkUpsert")) |
        .requestBody.description = "Metadata key-value pairs to create or update (merge)" |
        .responses."200".description = "Metadata upserted successfully"
      ) |

      # --- Add 409 to POST bulk ---
      .post.description = (.post.description + ". Returns 409 Conflict if any of the specified keys already exist.") |
      .post.responses."409" = conflict_response("Conflict - One or more metadata keys already exist for this entity")
    )
  else
    .
  end
)

# Step 3: Add 409 to POST single metadata endpoints
| .paths |= with_entries(
  if (.key | test("metadata$")) and (.value | has("post")) and (.key | test("saml|oauth") | not) then
    .value.post.description = (.value.post.description + ". Returns 409 Conflict if the key already exists.") |
    .value.post.responses."409" = conflict_response("Conflict - Metadata key already exists for this entity")
  else
    .
  end
)
