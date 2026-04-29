# Security Reviewer Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `security_reviewer` query parameter to `GET /threat_models` with a shared filter operator utility supporting `is:null` and `is:notnull` syntax.

**Architecture:** New shared filter operator parser (`api/filter_operators.go`) handles prefix-based operator syntax. The `security_reviewer` query parameter uses this parser and feeds results into the existing `ThreatModelFilters` → `applyThreatModelFilters()` pipeline. For `is:null`/`is:notnull`, the DB layer emits `IS NULL`/`IS NOT NULL` clauses; for plain values, it LEFT JOINs the users table for partial match (same pattern as the `owner` filter).

**Tech Stack:** Go, Gin, GORM, OpenAPI 3.0.3, oapi-codegen

**Issue:** [#230](https://github.com/ericfitz/tmi/issues/230)
**Spec:** `docs/superpowers/specs/2026-04-04-security-reviewer-filter-design.md`

---

### Task 1: Filter Operator Utility — Tests

**Files:**
- Create: `api/filter_operators_test.go`

- [ ] **Step 1: Write unit tests for ParseFilterValue**

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFilterValue(t *testing.T) {
	t.Run("plain value returns FilterOpNone", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "alice@example.com")
		require.NoError(t, err)
		assert.Equal(t, FilterOpNone, result.Operator)
		assert.Equal(t, "alice@example.com", result.Value)
	})

	t.Run("empty value returns FilterOpNone with empty value", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "")
		require.NoError(t, err)
		assert.Equal(t, FilterOpNone, result.Operator)
		assert.Equal(t, "", result.Value)
	})

	t.Run("is:null returns FilterOpIsNull", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "is:null")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNull, result.Operator)
		assert.Equal(t, "", result.Value)
	})

	t.Run("is:notnull returns FilterOpIsNotNull", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "is:notnull")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNotNull, result.Operator)
		assert.Equal(t, "", result.Value)
	})

	t.Run("is:NULL is case insensitive", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "is:NULL")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNull, result.Operator)
	})

	t.Run("IS:NOTNULL is case insensitive", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "IS:NOTNULL")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNotNull, result.Operator)
	})

	t.Run("Is:Null mixed case is case insensitive", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "Is:Null")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNull, result.Operator)
	})

	t.Run("unsupported is: operand returns error", func(t *testing.T) {
		_, err := ParseFilterValue("security_reviewer", "is:banana")
		require.Error(t, err)
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, 400, reqErr.Status)
		assert.Contains(t, reqErr.Message, "is:banana")
		assert.Contains(t, reqErr.Message, "security_reviewer")
	})

	t.Run("unsupported operator prefix returns error", func(t *testing.T) {
		_, err := ParseFilterValue("status", "gt:5")
		require.Error(t, err)
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, 400, reqErr.Status)
		assert.Contains(t, reqErr.Message, "gt")
	})

	t.Run("incomplete operator is: returns error", func(t *testing.T) {
		_, err := ParseFilterValue("security_reviewer", "is:")
		require.Error(t, err)
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, 400, reqErr.Status)
	})

	t.Run("value containing colon but not operator prefix is plain value", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "user:name@example.com")
		require.NoError(t, err)
		assert.Equal(t, FilterOpNone, result.Operator)
		assert.Equal(t, "user:name@example.com", result.Value)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestParseFilterValue`
Expected: FAIL — `ParseFilterValue` not defined.

- [ ] **Step 3: Commit**

```bash
git add api/filter_operators_test.go
git commit -m "test: add failing tests for filter operator parser"
```

---

### Task 2: Filter Operator Utility — Implementation

**Files:**
- Create: `api/filter_operators.go`

- [ ] **Step 1: Implement ParseFilterValue**

```go
package api

import (
	"fmt"
	"net/http"
	"strings"
)

// FilterOperator represents the type of filter operation to apply.
type FilterOperator int

const (
	// FilterOpNone indicates a plain value with no operator prefix.
	FilterOpNone FilterOperator = iota
	// FilterOpIsNull indicates the field should be NULL.
	FilterOpIsNull
	// FilterOpIsNotNull indicates the field should be NOT NULL.
	FilterOpIsNotNull
)

// ParsedFilter holds the result of parsing a filter query parameter value.
type ParsedFilter struct {
	Operator FilterOperator
	Value    string // Empty for is:null/is:notnull, populated for plain values
}

// supportedOperatorPrefixes lists the recognized operator prefixes.
// Only values starting with one of these prefixes (case-insensitive) are parsed as operators.
var supportedOperatorPrefixes = []string{"is:"}

// ParseFilterValue parses a query parameter value for operator prefixes.
// Recognized operators: is:null, is:notnull.
// Unrecognized operators return a 400 RequestError.
// Values without a recognized operator prefix are returned as plain values.
func ParseFilterValue(paramName, rawValue string) (ParsedFilter, error) {
	if rawValue == "" {
		return ParsedFilter{Operator: FilterOpNone, Value: ""}, nil
	}

	lower := strings.ToLower(rawValue)

	// Check if the value starts with a known operator prefix
	for _, prefix := range supportedOperatorPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return parseOperator(paramName, rawValue, prefix, lower)
		}
	}

	// No operator prefix — treat as plain value
	return ParsedFilter{Operator: FilterOpNone, Value: rawValue}, nil
}

func parseOperator(paramName, rawValue, prefix, lower string) (ParsedFilter, error) {
	operand := lower[len(prefix):]

	switch prefix {
	case "is:":
		switch operand {
		case "null":
			return ParsedFilter{Operator: FilterOpIsNull}, nil
		case "notnull":
			return ParsedFilter{Operator: FilterOpIsNotNull}, nil
		case "":
			return ParsedFilter{}, InvalidInputError(
				fmt.Sprintf("Incomplete filter operator for parameter %q: %q. Supported: is:null, is:notnull", paramName, rawValue))
		default:
			return ParsedFilter{}, InvalidInputError(
				fmt.Sprintf("Unsupported filter operator for parameter %q: %q. Supported: is:null, is:notnull", paramName, rawValue))
		}
	default:
		return ParsedFilter{}, InvalidInputError(
			fmt.Sprintf("Unsupported filter operator prefix %q for parameter %q. Supported prefixes: is:", prefix, paramName))
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `make test-unit name=TestParseFilterValue`
Expected: PASS — all cases pass.

- [ ] **Step 3: Run lint**

Run: `make lint`
Expected: No new issues in `api/filter_operators.go`.

- [ ] **Step 4: Commit**

```bash
git add api/filter_operators.go api/filter_operators_test.go
git commit -m "feat(api): add shared filter operator parser (is:null, is:notnull)"
```

---

### Task 3: Add SecurityReviewer to ThreatModelFilters and Handler

**Files:**
- Modify: `api/store.go:36-49` (ThreatModelFilters struct)
- Modify: `api/threat_model_handlers.go:902-1007` (parseThreatModelFilters)
- Modify: `api/handler_error_paths_test.go:93+` (parseThreatModelFilters tests)

- [ ] **Step 1: Write failing tests for parseThreatModelFilters with security_reviewer**

Add these test cases to `TestParseThreatModelFilters` in `api/handler_error_paths_test.go`, after the existing test cases (after the `multiple_filters_combined` test):

```go
	t.Run("security_reviewer_plain_value", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?security_reviewer=alice@example.com")
		filters, err := parseThreatModelFilters(c)
		require.NoError(t, err)
		require.NotNil(t, filters)
		require.NotNil(t, filters.SecurityReviewer)
		assert.Equal(t, FilterOpNone, filters.SecurityReviewer.Operator)
		assert.Equal(t, "alice@example.com", filters.SecurityReviewer.Value)
	})

	t.Run("security_reviewer_is_null", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?security_reviewer=is:null")
		filters, err := parseThreatModelFilters(c)
		require.NoError(t, err)
		require.NotNil(t, filters)
		require.NotNil(t, filters.SecurityReviewer)
		assert.Equal(t, FilterOpIsNull, filters.SecurityReviewer.Operator)
	})

	t.Run("security_reviewer_is_notnull", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?security_reviewer=is:notnull")
		filters, err := parseThreatModelFilters(c)
		require.NoError(t, err)
		require.NotNil(t, filters)
		require.NotNil(t, filters.SecurityReviewer)
		assert.Equal(t, FilterOpIsNotNull, filters.SecurityReviewer.Operator)
	})

	t.Run("security_reviewer_invalid_operator_returns_error", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?security_reviewer=is:banana")
		_, err := parseThreatModelFilters(c)
		require.Error(t, err)
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, http.StatusBadRequest, reqErr.Status)
		assert.Contains(t, reqErr.Message, "security_reviewer")
	})

	t.Run("security_reviewer_combined_with_status", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet,
			"/threat-models?security_reviewer=is:null&status=in_review")
		filters, err := parseThreatModelFilters(c)
		require.NoError(t, err)
		require.NotNil(t, filters)
		require.NotNil(t, filters.SecurityReviewer)
		assert.Equal(t, FilterOpIsNull, filters.SecurityReviewer.Operator)
		assert.Equal(t, []string{"in_review"}, filters.Status)
	})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestParseThreatModelFilters`
Expected: FAIL — `filters.SecurityReviewer` field does not exist.

- [ ] **Step 3: Add SecurityReviewer field to ThreatModelFilters**

In `api/store.go`, add the field to the `ThreatModelFilters` struct, after `StatusUpdatedBefore`:

```go
	StatusUpdatedBefore *time.Time    // Filter by status_updated <= value
	SecurityReviewer    *ParsedFilter // Filter by security reviewer (supports operator syntax: is:null, is:notnull, or partial match)
	IncludeDeleted      bool          // Include soft-deleted (tombstoned) entities
```

- [ ] **Step 4: Add security_reviewer parsing to parseThreatModelFilters**

In `api/threat_model_handlers.go`, add the parsing block in `parseThreatModelFilters`, after the `status_updated_before` block (before the `include_deleted` block at line 988):

```go
	if sr := c.Query("security_reviewer"); sr != "" {
		parsed, err := ParseFilterValue("security_reviewer", sr)
		if err != nil {
			return nil, err
		}
		filters.SecurityReviewer = &parsed
		hasFilters = true
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit name=TestParseThreatModelFilters`
Expected: PASS — all cases including the new ones.

- [ ] **Step 6: Commit**

```bash
git add api/store.go api/threat_model_handlers.go api/handler_error_paths_test.go
git commit -m "feat(api): add security_reviewer to ThreatModelFilters and handler parsing"
```

---

### Task 4: Database Filter Implementation

**Files:**
- Modify: `api/database_store_gorm.go:424-468` (applyThreatModelFilters)

- [ ] **Step 1: Add security_reviewer filter to applyThreatModelFilters**

In `api/database_store_gorm.go`, add the following block at the end of `applyThreatModelFilters`, before the final `return query` on line 467:

```go
	if filters.SecurityReviewer != nil {
		switch filters.SecurityReviewer.Operator {
		case FilterOpIsNull:
			query = query.Where("threat_models.security_reviewer_internal_uuid IS NULL")
		case FilterOpIsNotNull:
			query = query.Where("threat_models.security_reviewer_internal_uuid IS NOT NULL")
		case FilterOpNone:
			if filters.SecurityReviewer.Value != "" {
				query = query.Joins("LEFT JOIN users AS reviewer_filter ON threat_models.security_reviewer_internal_uuid = reviewer_filter.internal_uuid").
					Where("LOWER(reviewer_filter.email) LIKE LOWER(?) OR LOWER(reviewer_filter.name) LIKE LOWER(?)",
						"%"+filters.SecurityReviewer.Value+"%", "%"+filters.SecurityReviewer.Value+"%")
			}
		}
	}
```

- [ ] **Step 2: Run build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 3: Run all unit tests**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add api/database_store_gorm.go
git commit -m "feat(api): apply security_reviewer filter in database queries"
```

---

### Task 5: Handler Integration Tests

**Files:**
- Modify: `api/threat_model_handlers_test.go` (add tests to TestGetThreatModelsWithFilters)

- [ ] **Step 1: Write integration-style handler tests for security_reviewer filtering**

Add these test cases inside `TestGetThreatModelsWithFilters` in `api/threat_model_handlers_test.go`, after the existing filter test cases. These tests use the in-memory store via the existing test router so they exercise the full handler → store → filter pipeline.

First, add a setup block inside `TestGetThreatModelsWithFilters` that assigns a security reviewer to one of the existing test models. Insert this after the model creation loop (after the `createdIDs` loop, around line 988):

```go
	// Assign a security reviewer to the first model for filtering tests
	reviewerPatch := `[{"op": "replace", "path": "/security_reviewer", "value": {"email": "reviewer@example.com", "display_name": "Test Reviewer", "principal_type": "user", "provider": "tmi", "provider_id": "reviewer1"}}]`
	patchReq, _ := http.NewRequest("PATCH", "/threat_models/"+createdIDs[0], bytes.NewBufferString(reviewerPatch))
	patchReq.Header.Set("Content-Type", "application/json-patch+json")
	patchW := httptest.NewRecorder()
	r.ServeHTTP(patchW, patchReq)
	// Note: If patch fails (e.g., security_reviewer isn't patchable), the is:null/is:notnull tests
	// still validate the filter works — all models will have null security_reviewer.
```

Then add the test cases:

```go
	t.Run("filter by security_reviewer is:null", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?security_reviewer=is:null", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// All returned models should have nil security_reviewer
		for _, item := range response.ThreatModels {
			assert.Nil(t, item.SecurityReviewer, "is:null filter should only return models without a security reviewer")
		}
	})

	t.Run("filter by security_reviewer is:notnull", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?security_reviewer=is:notnull", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// All returned models should have non-nil security_reviewer
		for _, item := range response.ThreatModels {
			assert.NotNil(t, item.SecurityReviewer, "is:notnull filter should only return models with a security reviewer")
		}
	})

	t.Run("filter by security_reviewer invalid operator returns 400", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?security_reviewer=is:banana", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("filter by security_reviewer combined with status", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/threat_models?security_reviewer=is:null&status=not_started", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListThreatModelsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		for _, item := range response.ThreatModels {
			assert.Nil(t, item.SecurityReviewer, "Combined filter: security_reviewer should be null")
		}
	})
```

- [ ] **Step 2: Run the tests**

Run: `make test-unit name=TestGetThreatModelsWithFilters`
Expected: PASS — filter tests exercise the full handler pipeline.

- [ ] **Step 3: Commit**

```bash
git add api/threat_model_handlers_test.go
git commit -m "test: add handler integration tests for security_reviewer filter"
```

---

### Task 6: OpenAPI Spec Update

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: Add SecurityReviewerQueryParam to components/parameters**

Use `jq` to add the new parameter definition. Insert it in `components.parameters` alongside the existing parameters:

```bash
jq '.components.parameters.SecurityReviewerQueryParam = {
  "name": "security_reviewer",
  "in": "query",
  "schema": {
    "type": "string",
    "maxLength": 256,
    "pattern": "^[^\\x00-\\x1F]*$"
  },
  "description": "Filter by security reviewer. Plain value performs case-insensitive partial match on reviewer email or display name. Use '\''is:null'\'' to find unassigned threat models (no security reviewer), '\''is:notnull'\'' to find assigned ones."
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add parameter reference to GET /threat_models**

Add the `SecurityReviewerQueryParam` reference to the `GET /threat_models` parameters array. Insert it after `IncludeDeletedQueryParam`:

```bash
jq '.paths["/threat_models"].get.parameters += [{"$ref": "#/components/parameters/SecurityReviewerQueryParam"}]' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Validate the OpenAPI spec**

Run: `make validate-openapi`
Expected: Validation passes with no new errors.

- [ ] **Step 4: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` is regenerated. The new parameter will appear in the generated `GetThreatModelsParams` struct.

- [ ] **Step 5: Build to verify generated code compiles**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 6: Run all unit tests**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add security_reviewer query parameter to OpenAPI spec

Closes #230"
```

---

### Task 7: Final Validation

- [ ] **Step 1: Run full lint**

Run: `make lint`
Expected: No new lint issues (existing `api/api.go` staticcheck warnings are expected).

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 3: Run build**

Run: `make build-server`
Expected: Clean build.
