# fix-openapi-teams-projects.jq
# Adds missing OWASP-required patterns to teams/projects endpoints
# to match existing threat_models/diagrams endpoint conventions.

# Rate limit headers for success responses (200, 201, 204)
def rate_limit_headers_success:
  {
    "X-RateLimit-Limit": {
      "description": "Maximum number of requests allowed in the current time window",
      "schema": { "type": "integer", "example": 1000 }
    },
    "X-RateLimit-Remaining": {
      "description": "Number of requests remaining in the current time window",
      "schema": { "type": "integer", "example": 999 }
    },
    "X-RateLimit-Reset": {
      "description": "Unix epoch seconds when the rate limit window resets",
      "schema": { "type": "integer", "example": 1735689600 }
    }
  };

# Rate limit headers for error responses
def rate_limit_headers_error:
  {
    "X-RateLimit-Limit": {
      "description": "Maximum number of requests allowed in the current time window",
      "schema": { "type": "integer", "example": 1000 }
    },
    "X-RateLimit-Remaining": {
      "description": "Number of requests remaining in the current time window",
      "schema": { "type": "integer", "example": 999 }
    },
    "X-RateLimit-Reset": {
      "description": "Unix epoch seconds when the rate limit window resets",
      "schema": { "type": "integer", "example": 1640995200 }
    }
  };

# Standard 400 response (inline, with rate-limit headers)
def bad_request_response:
  {
    "description": "Bad Request - Invalid parameters or validation failures",
    "content": {
      "application/json": {
        "schema": { "$ref": "#/components/schemas/Error" }
      }
    },
    "headers": rate_limit_headers_error
  };

# Check if a path is a teams or projects path
def is_teams_projects_path:
  startswith("/teams") or startswith("/projects");

# Add rate-limit headers to an existing inline response based on its code
def add_rate_limit_headers($code):
  if has("$ref") then .
  elif ($code | test("^(200|201|204)$")) then
    .headers = ((.headers // {}) + rate_limit_headers_success)
  else
    .headers = ((.headers // {}) + rate_limit_headers_error)
  end;

# Process the spec
# Step 1: Add rate-limit headers to ALL existing responses on teams/projects paths
.paths |= with_entries(
  if (.key | is_teams_projects_path) then
    .value |= with_entries(
      if .key == "parameters" then .
      else
        .value.responses |= with_entries(
          .key as $code |
          .value |= add_rate_limit_headers($code)
        )
      end
    )
  else . end
)

# Step 2: Add 429 response ref to all operations
| .paths |= with_entries(
  if (.key | is_teams_projects_path) then
    .value |= with_entries(
      if .key == "parameters" then .
      else
        .value.responses["429"] //= { "$ref": "#/components/responses/TooManyRequests" }
      end
    )
  else . end
)

# Step 3: Replace inline 500 with $ref, or add if missing
| .paths |= with_entries(
  if (.key | is_teams_projects_path) then
    .value |= with_entries(
      if .key == "parameters" then .
      else
        .value.responses["500"] = { "$ref": "#/components/responses/Error" }
      end
    )
  else . end
)

# Step 4: Add 400 response to GET and DELETE operations that don't have it
| .paths |= with_entries(
  if (.key | is_teams_projects_path) then
    .value |= with_entries(
      if .key == "parameters" then .
      elif (.key == "get" or .key == "delete") and (.value.responses["400"] | not) then
        .value.responses["400"] = bad_request_response
      else . end
    )
  else . end
)

# Step 5: Add parameter descriptions where missing
| .paths |= with_entries(
  if (.key | is_teams_projects_path) then
    .value |= with_entries(
      if .key == "parameters" then .
      else
        if .value.parameters then
          .value.parameters |= map(
            if .description then .
            elif .name == "team_id" then . + { "description": "Team identifier (UUID)" }
            elif .name == "project_id" then . + { "description": "Project identifier (UUID)" }
            elif .name == "key" then . + { "description": "Metadata key" }
            elif .name == "limit" then . + { "description": "Maximum number of results per page" }
            elif .name == "offset" then . + { "description": "Number of results to skip" }
            else . end
          )
        else . end
      end
    )
  else . end
)

# Step 6: Add requestBody descriptions where missing
| .paths["/teams"].post.requestBody.description //= "Team creation data"
| .paths["/teams/{team_id}"].put.requestBody.description //= "Complete team data for replacement"
| .paths["/teams/{team_id}"].patch.requestBody.description //= "Partial team data for update"
| .paths["/teams/{team_id}/metadata"].post.requestBody.description //= "Metadata entry to create"
| .paths["/teams/{team_id}/metadata/{key}"].put.requestBody.description //= "Metadata value to set for the specified key"
| .paths["/teams/{team_id}/metadata/bulk"].post.requestBody.description //= "Bulk metadata entries to create"
| .paths["/teams/{team_id}/metadata/bulk"].patch.requestBody.description //= "Metadata entries to update"
| .paths["/teams/{team_id}/metadata/bulk"].put.requestBody.description //= "Complete set of metadata entries for replacement"
| .paths["/projects"].post.requestBody.description //= "Project creation data"
| .paths["/projects/{project_id}"].put.requestBody.description //= "Complete project data for replacement"
| .paths["/projects/{project_id}"].patch.requestBody.description //= "Partial project data for update"
| .paths["/projects/{project_id}/metadata"].post.requestBody.description //= "Metadata entry to create"
| .paths["/projects/{project_id}/metadata/{key}"].put.requestBody.description //= "Metadata value to set for the specified key"
| .paths["/projects/{project_id}/metadata/bulk"].post.requestBody.description //= "Bulk metadata entries to create"
| .paths["/projects/{project_id}/metadata/bulk"].patch.requestBody.description //= "Metadata entries to update"
| .paths["/projects/{project_id}/metadata/bulk"].put.requestBody.description //= "Complete set of metadata entries for replacement"

# Step 7: Add examples to schema properties (directly on TeamBase/ProjectBase, not via allOf $ref)
| .components.schemas.TeamBase.properties.name.example = "Platform Engineering"
| .components.schemas.ProjectBase.properties.name.example = "API Gateway Modernization"
| .components.schemas.ListTeamsResponse.properties.teams.example = [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Platform Engineering",
      "description": "Core platform infrastructure team",
      "status": "active",
      "created_at": "2025-01-15T10:30:00Z",
      "modified_at": "2025-06-20T14:22:00Z"
    }
  ]
| .components.schemas.ListProjectsResponse.properties.projects.example = [
    {
      "id": "660e8400-e29b-41d4-a716-446655440001",
      "name": "API Gateway Modernization",
      "description": "Migrate legacy API gateway to cloud-native architecture",
      "team_id": "550e8400-e29b-41d4-a716-446655440000",
      "status": "active",
      "created_at": "2025-02-01T09:00:00Z",
      "modified_at": "2025-07-10T16:45:00Z"
    }
  ]

# Step 8: Add examples to response content for list and single-resource endpoints
| .paths["/teams"].get.responses["200"].content["application/json"].example = {
    "teams": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "name": "Platform Engineering",
        "description": "Core platform infrastructure team",
        "status": "active",
        "created_at": "2025-01-15T10:30:00Z",
        "modified_at": "2025-06-20T14:22:00Z"
      }
    ],
    "total": 1,
    "limit": 20,
    "offset": 0
  }
| .paths["/teams"].post.responses["201"].content["application/json"].example = {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Platform Engineering",
    "description": "Core platform infrastructure team",
    "status": "active",
    "created_at": "2025-01-15T10:30:00Z",
    "modified_at": "2025-01-15T10:30:00Z"
  }
| .paths["/teams"].post.requestBody.content["application/json"].example = {
    "name": "Platform Engineering",
    "description": "Core platform infrastructure team"
  }
| .paths["/teams/{team_id}"].get.responses["200"].content["application/json"].example = {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Platform Engineering",
    "description": "Core platform infrastructure team",
    "status": "active",
    "created_at": "2025-01-15T10:30:00Z",
    "modified_at": "2025-06-20T14:22:00Z"
  }
| .paths["/teams/{team_id}"].put.requestBody.content["application/json"].example = {
    "name": "Platform Engineering",
    "description": "Core platform infrastructure team - updated"
  }
| .paths["/teams/{team_id}"].put.responses["200"].content["application/json"].example = {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Platform Engineering",
    "description": "Core platform infrastructure team - updated",
    "status": "active",
    "created_at": "2025-01-15T10:30:00Z",
    "modified_at": "2025-08-01T11:00:00Z"
  }
| .paths["/projects"].get.responses["200"].content["application/json"].example = {
    "projects": [
      {
        "id": "660e8400-e29b-41d4-a716-446655440001",
        "name": "API Gateway Modernization",
        "description": "Migrate legacy API gateway to cloud-native architecture",
        "team_id": "550e8400-e29b-41d4-a716-446655440000",
        "status": "active",
        "created_at": "2025-02-01T09:00:00Z",
        "modified_at": "2025-07-10T16:45:00Z"
      }
    ],
    "total": 1,
    "limit": 20,
    "offset": 0
  }
| .paths["/projects"].post.responses["201"].content["application/json"].example = {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "name": "API Gateway Modernization",
    "description": "Migrate legacy API gateway to cloud-native architecture",
    "team_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "active",
    "created_at": "2025-02-01T09:00:00Z",
    "modified_at": "2025-02-01T09:00:00Z"
  }
| .paths["/projects"].post.requestBody.content["application/json"].example = {
    "name": "API Gateway Modernization",
    "description": "Migrate legacy API gateway to cloud-native architecture",
    "team_id": "550e8400-e29b-41d4-a716-446655440000"
  }
| .paths["/projects/{project_id}"].get.responses["200"].content["application/json"].example = {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "name": "API Gateway Modernization",
    "description": "Migrate legacy API gateway to cloud-native architecture",
    "team_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "active",
    "created_at": "2025-02-01T09:00:00Z",
    "modified_at": "2025-07-10T16:45:00Z"
  }
| .paths["/projects/{project_id}"].put.requestBody.content["application/json"].example = {
    "name": "API Gateway Modernization",
    "description": "Updated project description",
    "team_id": "550e8400-e29b-41d4-a716-446655440000"
  }
| .paths["/projects/{project_id}"].put.responses["200"].content["application/json"].example = {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "name": "API Gateway Modernization",
    "description": "Updated project description",
    "team_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "active",
    "created_at": "2025-02-01T09:00:00Z",
    "modified_at": "2025-08-05T13:30:00Z"
  }
