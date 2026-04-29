# Design: Add security_reviewer query parameter to GET /threat_models with filter operator syntax

**Issue**: [#230](https://github.com/ericfitz/tmi/issues/230)
**Date**: 2026-04-04
**Status**: Approved

## Summary

Add a `security_reviewer` query parameter to `GET /threat_models` for filtering by assigned reviewer, including the ability to filter for unassigned threat models (`security_reviewer IS NULL`). Introduce a shared filter operator utility that parses operator-prefixed values (e.g., `is:null`, `is:notnull`) to support this and future filter enhancements across the API.

## Background

The tmi-ux triage page's "Reviews" tab needs to show unassigned threat models, but there is no server-side filter for `security_reviewer`. The client currently fetches all non-closed models and post-filters, which breaks pagination accuracy and doesn't scale.

## Design

### 1. Filter Operator Utility (`api/filter_operators.go`)

A shared utility that parses query parameter values for operator prefixes. This is the foundation for extensible server-side filtering across the API.

**Supported operators (initial):**
- `is:null` — field IS NULL
- `is:notnull` — field IS NOT NULL

**Behavior:**
- Values with a recognized `operator:value` prefix are parsed into an operator enum and optional operand
- Operator parsing is case-insensitive (`is:null`, `IS:NULL`, `Is:Null` are all equivalent)
- Values without an `operator:` prefix are treated as plain values (existing partial-match behavior)
- Unrecognized operators (e.g., `is:banana`, `gt:5` before `gt` is implemented) return a `400 Bad Request` with a message like `"unsupported filter operator 'gt' for parameter 'security_reviewer'; supported operators: is:null, is:notnull"`

**Types:**

```go
type FilterOperator int

const (
    FilterOpNone    FilterOperator = iota // Plain value, no operator prefix
    FilterOpIsNull                        // is:null
    FilterOpIsNotNull                     // is:notnull
)

type ParsedFilter struct {
    Operator FilterOperator
    Value    string // Empty for is:null/is:notnull, populated for plain values
}

// ParseFilterValue parses a query parameter value for operator prefixes.
// Returns a ParsedFilter and an error if the operator is unrecognized.
func ParseFilterValue(paramName, rawValue string) (ParsedFilter, error)
```

**Unit tests** (`api/filter_operators_test.go`):
- `""` → `FilterOpNone` with empty value
- `"alice@example.com"` → `FilterOpNone` with value `"alice@example.com"`
- `"is:null"` → `FilterOpIsNull`
- `"is:notnull"` → `FilterOpIsNotNull`
- `"is:NULL"` → `FilterOpIsNull` (case-insensitive operator parsing)
- `"is:banana"` → error
- `"gt:5"` → error (not yet supported)
- `"is:"` → error (incomplete operator)

### 2. OpenAPI Spec (`api-schema/tmi-openapi.json`)

**New parameter definition** in `components/parameters`:

```json
"SecurityReviewerQueryParam": {
  "name": "security_reviewer",
  "in": "query",
  "description": "Filter by security reviewer. Supports partial match on email or display name. Use 'is:null' to find unassigned threat models, 'is:notnull' to find assigned ones.",
  "required": false,
  "schema": {
    "type": "string",
    "maxLength": 256
  }
}
```

**Add to `GET /threat_models` parameters list** — insert `{ "$ref": "#/components/parameters/SecurityReviewerQueryParam" }` alongside existing filter parameters.

### 3. ThreatModelFilters struct (`api/store.go`)

Add a new field to carry the parsed filter:

```go
SecurityReviewer *ParsedFilter // Operator-aware: is:null, is:notnull, or partial match on email/name
```

### 4. Handler (`api/threat_model_handlers.go`)

In `parseThreatModelFilters()`, parse the `security_reviewer` query param:

```go
if sr := c.Query("security_reviewer"); sr != "" {
    parsed, err := ParseFilterValue("security_reviewer", sr)
    if err != nil {
        // Return 400 with error message
    }
    filters.SecurityReviewer = &parsed
}
```

### 5. Database Layer (`api/database_store_gorm.go`)

In `applyThreatModelFilters()`, add the security reviewer filter clause:

```go
if filters.SecurityReviewer != nil {
    switch filters.SecurityReviewer.Operator {
    case FilterOpIsNull:
        query = query.Where("threat_models.security_reviewer_internal_uuid IS NULL")
    case FilterOpIsNotNull:
        query = query.Where("threat_models.security_reviewer_internal_uuid IS NOT NULL")
    case FilterOpNone:
        // Partial match on reviewer email/name — same pattern as owner filter
        query = query.Joins("LEFT JOIN users AS reviewer_filter ON threat_models.security_reviewer_internal_uuid = reviewer_filter.internal_uuid").
            Where("LOWER(reviewer_filter.email) LIKE LOWER(?) OR LOWER(reviewer_filter.name) LIKE LOWER(?)",
                "%"+filters.SecurityReviewer.Value+"%", "%"+filters.SecurityReviewer.Value+"%")
    }
}
```

### 6. Tests

**Unit tests** (`api/threat_model_handlers_test.go`):
- Filter by `security_reviewer=alice` returns only models with reviewer matching "alice"
- Filter by `security_reviewer=is:null` returns only unassigned models
- Filter by `security_reviewer=is:notnull` returns only assigned models
- Filter by `security_reviewer=is:banana` returns 400
- Combined with other filters (e.g., `status=in_review&security_reviewer=is:null`)

**Integration tests**: Same scenarios against PostgreSQL.

## What stays the same

- **Response schema** — `TMListItem` already includes `security_reviewer`
- **Authorization logic** — unchanged
- **All existing filter parameters** — unchanged behavior; operator syntax is opt-in per parameter

## Future extensibility

The filter operator utility is designed to grow. Future additions could include:
- `is:blank` — null, zero-length, or all-whitespace
- `gt:`, `lt:`, `gte:`, `lte:` — comparison operators for dates/numbers
- `contains:`, `startswith:` — explicit string matching operators
- Applying operator syntax to existing parameters (owner, name, status, etc.)

Each new operator only requires adding a constant and the parsing case — no structural changes.

## Non-goals

- No changes to other endpoints (this is scoped to `GET /threat_models`)
- No retroactive application of operator syntax to existing filter parameters
- No full-text search or fuzzy matching beyond the existing LIKE pattern
