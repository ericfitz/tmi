# CATS Remediation Plan (Non-401 Errors)

**Created:** 2025-12-06
**Status:** Active
**Based on:** [security-reports/cats/non-success-results-report-excluding-401.md](../../../security-reports/cats/non-success-results-report-excluding-401.md)

**Current State:**
-  Middleware ordering fixed (commit `0eb4bf7`)
-  Validation runs before authentication (RFC 9110 compliant)
- **Current Success Rate:** 90.21% (excluding 401 errors)
- **Target Success Rate:** 98%+ (industry standard)

**Remaining Issues:** 2,160 errors (9.79% failure rate)

---

## Executive Summary

With the authentication/middleware ordering issues resolved (commit `0eb4bf7`), we now focus on the **remaining 2,160 functional errors** that prevent achieving industry-standard API reliability (98%+ success rate).

This plan addresses four critical issue categories:

1. **Critical (Priority 1):** 181 production bugs - 500 errors, schema failures, Happy Path failures
2. **High (Priority 2):** 94 stability issues - unimplemented endpoints, 409 conflicts
3. **Medium (Priority 3):** 1,145 security hardening - Unicode attacks on admin endpoints
4. **Low (Priority 4):** 740 optimization - error message quality, 404 handling

**Key Insight:** The top 10 error-prone endpoints account for **54.8% of all remaining failures** - targeted fixes will have outsized impact.

---

## Issue Breakdown

### By Severity

| Priority | Category | Errors | % of Total | Impact |
|----------|----------|--------|------------|--------|
| **P1** | Critical Bugs | 181 | 8.4% | Production stability |
| **P2** | High Priority | 94 | 4.4% | API completeness |
| **P3** | Medium Priority | 1,145 | 53.0% | Security hardening |
| **P4** | Low Priority | 740 | 34.3% | Polish & UX |

### By Error Type

| Error Reason | Count | % | Status |
|--------------|-------|---|--------|
| Unexpected 400 | 1,391 | 64.4% | Mostly expected (input validation working) |
| Not found (404) | 231 | 10.7% | Mixed (some valid, some bugs) |
| Schema mismatch | 130 | 6.0% | **CRITICAL - API contract violations** |
| Unexpected 200 | 107 | 5.0% | **CRITICAL - Missing validation** |
| Internal 500 | 150 | 6.9% | **CRITICAL - Application crashes** |
| Not implemented (501) | 47 | 2.2% | Incomplete API surface |
| Conflict (409) | 47 | 2.2% | Possible race conditions |
| Other | 57 | 2.6% | Various edge cases |

### Top 10 Error-Prone Endpoints

| Rank | Endpoint | Errors | % of Total | Primary Issues |
|------|----------|--------|------------|----------------|
| 1 | `/threat_models/{id}/diagrams/{id}` | 234 | 10.8% | Mixed errors |
| 2 | `/admin/groups` | 183 | 8.5% | Unicode + 501 errors |
| 3 | `/admin/users` | 153 | 7.1% | Input validation |
| 4 | `/addons` | 119 | 5.5% | **500 errors (42)** + schema (1) |
| 5 | `/client-credentials` | 97 | 4.5% | **Schema errors (38)** |
| 6 | `/admin/quotas/webhooks/{user_id}` | 93 | 4.3% | **500 errors (28)** |
| 7 | `/admin/quotas/addons/{user_id}` | 81 | 3.8% | **500 errors (14)** |
| 8 | `/admin/users/{internal_uuid}` | 78 | 3.6% | Mixed errors |
| 9 | `/invocations/{id}/status` | 72 | 3.3% | Validation issues |
| 10 | `/admin/quotas/users/{user_id}` | 70 | 3.2% | **500 errors (14)** |
| | **Total Top 10** | **1,180** | **54.8%** | |

**Key Finding:** Top 10 endpoints contain **100 of 150 total 500 errors (66.7%)** - highest priority fixes.

---

## Priority 1: Critical Bugs (Must Fix)

### 1.1 Internal Server Errors (150 errors, 6.9%)

**Impact:** Production crashes, potential data corruption, security vulnerabilities

#### Root Cause Analysis

**Endpoint Breakdown:**
- `/addons` - 42 errors (28%)
- `/admin/quotas/webhooks/{user_id}` - 28 errors (18.7%)
- `/admin/quotas/addons/{user_id}` - 14 errors (9.3%)
- `/admin/quotas/users/{user_id}` - 14 errors (9.3%)
- Other endpoints - 52 errors (34.7%)

**Fuzzer Breakdown:**
| Fuzzer | Errors | Issue Type |
|--------|--------|------------|
| ZeroWidthCharsInValuesFields | 12 | Unicode handling crash |
| AcceptLanguageHeaders | 20 | Header parsing crash |
| HangulFillerFields | 6 | Unicode handling crash |
| RandomResources | 12 | Invalid reference crash |
| InvalidReferencesFields | 12 | Foreign key crash |
| ExtremePositiveNumbers* | 12 | Numeric overflow crash |
| IntegerFieldsRightBoundary | 4 | Boundary condition crash |
| MinimumExactNumbers* | 4 | Numeric validation crash |

#### Fix Strategy

**Step 1: Add Panic Recovery Middleware** (Already exists, verify enabled)
```go
// Verify in cmd/server/main.go
r.Use(gin.Recovery()) // Should be first middleware
```

**Step 2: Fix `/addons` Endpoint (42 errors)** - File: [api/addons.go](../../../api/addons.go)

Issues:
- 12 ZeroWidthCharsInValuesFields errors (Unicode crash)
- 6 HangulFillerFields errors (Unicode crash)
- 5 AcceptLanguageHeaders errors (header parsing crash)
- 4 AbugidasInStringFields errors (Unicode crash)
- 4 FullwidthBracketsFields errors (Unicode crash)
- 3+ each from various fuzzers

**Action Items:**
1. Add comprehensive Unicode validation before processing
2. Sanitize all string inputs using existing `UnicodeNormalizationMiddleware`
3. Add proper error handling for header parsing
4. Add numeric boundary validation
5. Add nil checks for all pointer dereferences

**Test Cases:**
```go
// api/addons_test.go - Add these tests
func TestListAddons_UnicodeHandling(t *testing.T) {
    // Test zero-width characters
    // Test Hangul filler
    // Test bidirectional override
}

func TestListAddons_NumericBoundaries(t *testing.T) {
    // Test extreme positive numbers
    // Test integer right boundary
}

func TestListAddons_InvalidHeaders(t *testing.T) {
    // Test malformed Accept-Language
}
```

**Step 3: Fix Admin Quota Endpoints (56 errors)**

Files:
- [api/admin_quotas.go](../../../api/admin_quotas.go)

Endpoints:
- `/admin/quotas/webhooks/{user_id}` - 28 errors
- `/admin/quotas/addons/{user_id}` - 14 errors
- `/admin/quotas/users/{user_id}` - 14 errors

**Common Issues:**
- AcceptLanguageHeaders (15 errors) - Header parsing crash
- RandomResources (12 errors) - Invalid user_id crash
- InvalidReferencesFields (9 errors) - Foreign key crash
- ExtremePositiveNumbers (12 errors) - Numeric overflow crash

**Action Items:**
1. Validate `user_id` parameter before database queries
2. Add proper error handling for foreign key violations
3. Validate numeric inputs for quota values (prevent overflow)
4. Add proper header parsing with error recovery
5. Return 404 for non-existent user_id (not 500)

**Test Cases:**
```go
func TestAdminQuotas_InvalidUserID(t *testing.T) {
    // Should return 404, not 500
}

func TestAdminQuotas_NumericOverflow(t *testing.T) {
    // Should return 400, not 500
}

func TestAdminQuotas_ForeignKeyViolation(t *testing.T) {
    // Should return 404, not 500
}
```

**Step 4: Add Comprehensive Error Logging**

For all 500 errors, add structured logging:
```go
logger.Error("Internal server error in %s: %v",
    c.Request.URL.Path,
    map[string]interface{}{
        "error": err.Error(),
        "fuzzer": c.GetHeader("X-CATS-Fuzzer"),
        "request_id": c.GetHeader("X-Request-ID"),
        "stack_trace": debug.Stack(),
    },
)
```

#### Success Criteria

- [ ] Zero 500 errors on `/addons` endpoint
- [ ] Zero 500 errors on `/admin/quotas/*` endpoints
- [ ] All 500 errors return appropriate 400/404 instead
- [ ] Comprehensive unit tests for each fuzzer scenario
- [ ] Error logging captures stack traces for debugging
- [ ] Post-mortem document for each root cause

**Estimated Effort:** 2-3 days

---

### 1.2 Schema Validation Failures (130 errors, 6.0%)

**Impact:** API contract violations, client integration breakage, poor developer experience

#### Root Cause Analysis

**Endpoint Breakdown:**
| Endpoint | Errors | % |
|----------|--------|---|
| `/client-credentials` | 38 | 29.2% |
| `/threat_models/{id}/threats` | 10 | 7.7% |
| `/saml/providers/{idp}/users` | 10 | 7.7% |
| `/invocations` | 4 | 3.1% |
| Other endpoints | 68 | 52.3% |

**Issue Types:**
1. **Missing required fields in response** (most common)
2. **Extra fields not in OpenAPI spec**
3. **Wrong data types** (string vs number)
4. **Numeric fields exceeding schema limits**
5. **Enum values not matching spec**

#### Fix Strategy

**Step 1: `/client-credentials` Endpoint (38 errors)** - File: [api/client_credentials.go](../../../api/client_credentials.go)

**Fuzzer Breakdown:**
- ZeroWidthCharsInValuesFields - 12 errors
- HangulFillerFields - 6 errors
- AbugidasInStringFields - 4 errors
- FullwidthBracketsFields - 4 errors
- HappyPath - **2 errors** (CRITICAL - valid requests failing)
- Various others - 10 errors

**Analysis:**
```bash
# Run this to see exact schema mismatch
sqlite3 cats-results.db "
SELECT scenario, result_details
FROM test_results_filtered_view
WHERE path = '/client-credentials'
  AND result_reason = 'Not matching response schema'
LIMIT 5;
"
```

**Common Issues:**
1. Response missing `client_secret` field (only shown once at creation)
2. Response includes internal fields not in OpenAPI spec
3. Date fields in wrong format (RFC 3339 vs ISO 8601)
4. UUID fields not validating format

**Action Items:**
1. Review OpenAPI spec for `/client-credentials` endpoints:
   - `POST /client-credentials` (201 response)
   - `GET /client-credentials` (200 response)
   - `DELETE /client-credentials/{id}` (204 response)

2. Compare actual response structure to spec:
```go
// Current response (example - verify actual)
type ClientCredential struct {
    ID          string    `json:"id"`
    ClientID    string    `json:"client_id"`
    ClientSecret string   `json:"client_secret,omitempty"` // Only on create
    Name        string    `json:"name"`
    Description string    `json:"description"`
    CreatedAt   time.Time `json:"created_at"`
    ExpiresAt   *time.Time `json:"expires_at,omitempty"`
    LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
    IsActive    bool      `json:"is_active"`
}

// Verify matches OpenAPI spec exactly
// Check for:
// - Extra fields (remove or add to spec)
// - Missing fields (add to response)
// - Wrong field names (snake_case vs camelCase)
// - Wrong data types
```

3. Add response validation tests:
```go
func TestClientCredentials_ResponseSchema(t *testing.T) {
    // Create client credential
    resp := CreateClientCredential(...)

    // Validate against OpenAPI schema
    validateOpenAPIResponse(t, "POST /client-credentials", 201, resp)
}
```

4. Fix Unicode handling (already covered by middleware, but verify)

**Step 2: `/threat_models/{id}/threats` Endpoint (10 errors)**

**Issue:** Numeric fields exceeding OpenAPI schema limits

**Fuzzers:**
- ExtremeNegativeNumbersInDecimalFields - 5 errors
- ExtremePositiveNumbersInDecimalFields - 5 errors

**Analysis:**
The OpenAPI spec likely defines numeric constraints like:
```yaml
severity_score:
  type: number
  format: decimal
  minimum: 0
  maximum: 10
```

But the API accepts values like `999999999999` or `-999999999999`.

**Action Items:**
1. Review OpenAPI spec for threat model numeric fields
2. Add validation middleware for numeric boundaries
3. Return 400 with clear error for out-of-range values
4. Update OpenAPI spec if current limits are too restrictive

**Code Fix:**
```go
// In threat model validation
func ValidateThreatModel(tm *ThreatModel) error {
    if tm.SeverityScore != nil {
        if *tm.SeverityScore < 0 || *tm.SeverityScore > 10 {
            return fmt.Errorf("severity_score must be between 0 and 10")
        }
    }
    // ... other validations
}
```

**Step 3: `/saml/providers/{idp}/users` Endpoint (10 errors)**

**Issue:** Unicode handling in user fields

**Fuzzers:**
- ZeroWidthCharsInValuesFields - 6 errors
- HangulFillerFields - 4 errors

**Action Items:**
1. Review SAML user response structure
2. Ensure Unicode normalization middleware is applied
3. Add tests for Unicode user attributes
4. Verify response matches OpenAPI spec exactly

**Step 4: `/invocations` Endpoint (4 errors)**

**Issue:** Enum values not matching spec

**Fuzzer:** IterateThroughEnumValuesFields - 4 errors

**Analysis:**
CATS is sending valid enum values from the OpenAPI spec, but the API is rejecting them or returning wrong values in response.

**Action Items:**
1. Review enum definitions in OpenAPI spec for invocations
2. Ensure all enum values are handled in code
3. Add validation that rejects invalid enum values (return 400)
4. Add tests for each enum value

**Example:**
```yaml
# In OpenAPI spec
invocation_status:
  type: string
  enum: [pending, running, completed, failed, cancelled]
```

```go
// In code - ensure all values handled
type InvocationStatus string

const (
    InvocationStatusPending   InvocationStatus = "pending"
    InvocationStatusRunning   InvocationStatus = "running"
    InvocationStatusCompleted InvocationStatus = "completed"
    InvocationStatusFailed    InvocationStatus = "failed"
    InvocationStatusCancelled InvocationStatus = "cancelled"
)

func (s InvocationStatus) Valid() bool {
    switch s {
    case InvocationStatusPending, InvocationStatusRunning,
         InvocationStatusCompleted, InvocationStatusFailed,
         InvocationStatusCancelled:
        return true
    }
    return false
}
```

#### Validation Strategy

**Add OpenAPI Response Validation Tests:**

```go
// api/openapi_validation_test.go
func TestAllEndpoints_ResponseSchema(t *testing.T) {
    endpoints := []struct {
        method string
        path   string
        status int
    }{
        {"POST", "/client-credentials", 201},
        {"GET", "/client-credentials", 200},
        {"POST", "/threat_models/{id}/threats", 201},
        {"GET", "/saml/providers/{idp}/users", 200},
        {"POST", "/invocations", 201},
        // ... all endpoints
    }

    for _, ep := range endpoints {
        t.Run(ep.method+" "+ep.path, func(t *testing.T) {
            // Make request
            resp := makeRequest(ep.method, ep.path)

            // Validate against OpenAPI schema
            err := validateResponseSchema(ep.path, ep.status, resp)
            require.NoError(t, err, "Response must match OpenAPI schema")
        })
    }
}
```

**Use existing OpenAPI validation library:**
```go
import (
    "github.com/getkin/kin-openapi/openapi3"
    "github.com/getkin/kin-openapi/openapi3filter"
)

func validateResponseSchema(path string, status int, response interface{}) error {
    // Load OpenAPI spec
    loader := openapi3.NewLoader()
    doc, err := loader.LoadFromFile("docs/reference/apis/tmi-openapi.json")
    if err != nil {
        return err
    }

    // Validate response
    // ... implementation
}
```

#### Success Criteria

- [ ] Zero "Not matching response schema" errors
- [ ] All endpoints return responses matching OpenAPI spec exactly
- [ ] Comprehensive response validation tests added
- [ ] OpenAPI spec updated if needed (with version bump)
- [ ] Documentation for schema changes

**Estimated Effort:** 3-4 days

---

### 1.3 Happy Path Failures (31 errors, 1.44%)

**Impact:** Valid requests failing - catastrophic user experience

**CRITICAL:** These are the highest priority fixes - valid, well-formed requests should NEVER fail.

#### Analysis

Most Happy Path failures are actually **401 errors** (authentication issues, not functional bugs):
- 401 errors: 116 of 131 Happy Path tests

**Remaining non-401 Happy Path failures: 15 errors**

**Breakdown:**
| Error Type | Count | Example Endpoints |
|------------|-------|-------------------|
| Schema validation | 6 | `/addons`, `/client-credentials`, `/invocations` |
| 500 Internal Error | 3 | `/addons`, `/admin/quotas/*` |
| 404 Not Found | 8 | `/admin/groups`, `/admin/users`, `/admin/quotas/*` |
| 409 Conflict | 1 | `/admin/groups` |
| 400 Bad Request | 6 | `/addons/{id}`, `/invocations/*` |
| 501 Not Implemented | 1 | `/admin/groups` |

**Critical Issues:**

1. **`/addons` - Schema validation on Happy Path (1 error)**
   - Valid request returns 200 but response doesn't match schema
   - **FIX:** Covered in 1.2 above

2. **`/addons` - 500 error on Happy Path (1 error)**
   - Valid request crashes the server
   - **FIX:** Covered in 1.1 above

3. **`/client-credentials` - Schema validation (2 errors)**
   - 200 and 201 responses don't match schema
   - **FIX:** Covered in 1.2 above

4. **`/admin/groups` - 404 on valid UUID (2 errors)**
   - Valid UUID returns 404 instead of empty array or specific group
   - **FIX:** Check if endpoint expects group to exist (test data issue) or endpoint bug

5. **`/admin/groups` - 409 Conflict (1 error)**
   - Creating group with valid data returns 409
   - **FIX:** Check for duplicate name/slug validation, may be test data issue

6. **`/admin/groups` - 501 Not Implemented (1 error)**
   - Basic endpoint not implemented
   - **FIX:** Covered in Priority 2 below

7. **`/admin/quotas/*` - 404 on valid user_id (3 errors)**
   - Valid user_id returns 404
   - **FIX:** Check if quotas exist for user (test data issue) or return empty default quota

8. **`/admin/quotas/*` - 500 on Happy Path (2 errors)**
   - Valid requests crash
   - **FIX:** Covered in 1.1 above

9. **`/admin/users` - 404 on valid request (1 error)**
   - Valid request returns 404
   - **FIX:** Test data issue or pagination bug

10. **`/addons/{id}`, `/invocations/*` - 400 on Happy Path (6 errors)**
    - Valid requests return 400
    - **FIX:** Overly strict validation, need to review validation rules

#### Fix Strategy

**Step 1: Create Golden Path Integration Tests**

```go
// api/happy_path_test.go
func TestHappyPath_AllEndpoints(t *testing.T) {
    // Test every endpoint with valid, well-formed requests
    // ALL should succeed (200, 201, 204, etc.)

    tests := []struct {
        name     string
        method   string
        path     string
        body     interface{}
        wantCode int
    }{
        {
            name: "List addons",
            method: "GET",
            path: "/addons",
            wantCode: 200,
        },
        {
            name: "Create client credential",
            method: "POST",
            path: "/client-credentials",
            body: ClientCredentialRequest{
                Name: "Test Credential",
                Description: "Valid test credential",
            },
            wantCode: 201,
        },
        // ... all endpoints
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            resp := makeAuthenticatedRequest(tt.method, tt.path, tt.body)
            require.Equal(t, tt.wantCode, resp.StatusCode,
                "Happy path requests must succeed")

            // Validate response schema
            validateSchema(t, tt.path, resp)
        })
    }
}
```

**Step 2: Fix Test Data Issues**

Many 404 errors on Happy Path may be test data issues:

```go
// Ensure test data exists before running tests
func setupTestData(t *testing.T) {
    // Create test admin groups
    CreateAdminGroup("test-group-1")

    // Create test users with quotas
    user := CreateTestUser("test-user")
    SetDefaultQuotas(user.ID)

    // Create test addons
    CreateTestAddon("test-addon-1")
}
```

**Step 3: Review Validation Rules**

For 400 errors on Happy Path:

```go
// api/validation.go
// Review these - may be too strict
func ValidateAddonID(id string) error {
    // Is this rejecting valid IDs?
}

func ValidateInvocationRequest(req *InvocationRequest) error {
    // Is this rejecting valid requests?
}
```

**Step 4: Fix 404 to Return Empty Results**

For endpoints that return 404 when resource not found, consider returning empty array instead:

```go
// api/admin_quotas.go
func GetUserQuotas(c *gin.Context) {
    userID := c.Param("user_id")

    quotas, err := s.store.GetQuotasForUser(userID)
    if err == ErrNotFound {
        // Return default quotas instead of 404
        quotas = GetDefaultQuotas()
    }

    c.JSON(200, quotas)
}
```

#### Success Criteria

- [ ] 100% Happy Path success rate (excluding 401 auth issues)
- [ ] All valid requests return appropriate 2XX responses
- [ ] All responses match OpenAPI schema
- [ ] Comprehensive golden path integration tests
- [ ] Test data setup documented and automated

**Estimated Effort:** 2 days

---

## Priority 2: High Priority Issues

### 2.1 Unimplemented Functionality (47 errors, 2.18%)

**Impact:** Incomplete API surface, client confusion

#### Analysis

**Breakdown by Root Cause:**

1. **Transfer-Encoding Header (23 errors)**
   - Fuzzer: DummyTransferEncodingHeaders
   - Endpoints return 501 when `Transfer-Encoding: chunked` header is sent
   - Affects: `/threat_models/{id}/diagrams/{id}` and other DELETE endpoints

2. **`/admin/groups` Endpoint (37 errors)**
   - Multiple fuzzers getting 501 responses
   - Indicates partial or missing implementation
   - Breakdown:
     - ZeroWidthCharsInValuesFields - 12 errors
     - HangulFillerFields - 8 errors
     - AcceptLanguageHeaders - 5 errors
     - AbugidasInStringFields - 4 errors
     - FullwidthBracketsFields - 4 errors
     - DummyTransferEncodingHeaders - 3 errors
     - HappyPath - **1 error** (CRITICAL)

3. **Other Endpoints (24 errors)**
   - Various endpoints with DummyTransferEncodingHeaders
   - All appear to be Transfer-Encoding related

#### Fix Strategy

**Option 1: Implement Transfer-Encoding Support** (NOT RECOMMENDED)

Complexity: High
Risk: Medium-High
Benefit: Low (rarely used in practice)

```go
// Would need to add chunked transfer encoding support to Gin
// This is complex and rarely needed for REST APIs
```

**Option 2: Reject with 400 Instead of 501** (RECOMMENDED)

Complexity: Low
Risk: Low
Benefit: High (proper HTTP semantics)

```go
// api/transfer_encoding_middleware.go
func TransferEncodingValidationMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Check for Transfer-Encoding header
        if te := c.GetHeader("Transfer-Encoding"); te != "" {
            c.JSON(400, gin.H{
                "error": "unsupported_encoding",
                "error_description": "Transfer-Encoding header is not supported",
                "supported_encodings": []string{"identity"},
            })
            c.Abort()
            return
        }
        c.Next()
    }
}
```

Update OpenAPI spec to document this:
```yaml
# In OpenAPI spec headers section
Transfer-Encoding:
  description: Not supported - requests with this header will be rejected
  schema:
    type: string
  required: false
  deprecated: true
```

**Option 3: Update OpenAPI Spec Only** (SIMPLEST)

Document that Transfer-Encoding is not supported, return 400 instead of 501:

```yaml
# In OpenAPI spec
x-unsupported-headers:
  - Transfer-Encoding

responses:
  400:
    description: Bad Request - includes unsupported headers
    content:
      application/json:
        schema:
          $ref: '#/components/schemas/Error'
        examples:
          unsupported_header:
            summary: Unsupported Transfer-Encoding header
            value:
              error: "unsupported_header"
              error_description: "Transfer-Encoding header is not supported"
```

**Recommendation:** Use Option 2 (middleware + spec update)

**Fix `/admin/groups` Endpoint (37 errors, including 1 Happy Path failure)**

File: [api/admin_groups.go](../../../api/admin_groups.go)

**Investigation Steps:**

1. Check if endpoint is implemented:
```bash
grep -n "admin/groups" api/*.go
```

2. Check OpenAPI spec:
```bash
jq '.paths."/admin/groups"' docs/reference/apis/tmi-openapi.json
```

3. If partially implemented:
   - Complete implementation
   - Add tests
   - Update OpenAPI spec

4. If fully implemented but returning 501:
   - Debug why 501 is being returned
   - Check middleware ordering
   - Check route registration

5. If not implemented:
   - **Option A:** Implement it (if needed functionality)
   - **Option B:** Remove from OpenAPI spec (if not planned)

**Given Happy Path failure:** This endpoint MUST be implemented or removed.

#### Success Criteria

- [ ] Zero 501 "Not Implemented" responses
- [ ] Transfer-Encoding handled properly (400 if unsupported)
- [ ] `/admin/groups` fully implemented or removed from spec
- [ ] OpenAPI spec updated to reflect supported features
- [ ] All endpoints documented have working implementations

**Estimated Effort:** 1-2 days

---

### 2.2 Unexpected 409 Conflicts (47 errors, 2.18%)

**Impact:** Possible race conditions, state management issues

#### Analysis Needed

First, get details on where 409 errors are occurring:

```sql
SELECT path, fuzzer, scenario, COUNT(*) as count
FROM test_results_filtered_view
WHERE response_code = 409
GROUP BY path, fuzzer, scenario
ORDER BY count DESC;
```

**Hypothesis:** 409 errors may be legitimate in some cases:
- Creating duplicate resources (correct behavior)
- Concurrent modifications (correct behavior with optimistic locking)
- State conflicts (correct business logic validation)

**Investigation Steps:**

1. **Categorize 409 Errors:**
   - Which endpoints return 409?
   - What fuzzers trigger 409?
   - What request scenarios cause 409?

2. **Determine if Expected:**
   - Is 409 documented in OpenAPI spec for this endpoint?
   - Is it legitimate duplicate detection?
   - Is it proper conflict handling?

3. **Fix if Unexpected:**
   - Add idempotency keys if race condition
   - Fix duplicate detection logic if over-aggressive
   - Document expected 409 scenarios in OpenAPI spec

4. **Add Tests:**
```go
func TestConcurrentCreation_ProperConflictHandling(t *testing.T) {
    // Create same resource twice concurrently
    // Second should return 409 Conflict (expected)
}

func TestIdempotency_SameRequestTwice(t *testing.T) {
    // Send identical request twice
    // Should not return 409 (idempotent)
}
```

#### Success Criteria

- [ ] All 409 responses documented in OpenAPI spec, OR
- [ ] Fixed to handle requests properly (with idempotency)
- [ ] Concurrency tests added for critical endpoints
- [ ] Clear documentation of when 409 is expected

**Estimated Effort:** 2-3 days (depends on investigation findings)

---

## Priority 3: Security Hardening

### 3.1 Unicode/Encoding Attack Vulnerabilities (1,145 errors, 53.0%)

**Impact:** Potential XSS, injection attacks, display corruption, bypass attempts

**Current Status:** Middleware already exists (`UnicodeNormalizationMiddleware`) but may need tuning

#### Attack Type Breakdown

| Attack Type | Errors | % | Mitigation Status |
|-------------|--------|---|-------------------|
| BidirectionalOverrideFields | 468 | 21.7% | † Partial |
| ZeroWidthCharsInValuesFields | 372 | 17.2% | † Partial |
| HangulFillerFields | 305 | 14.1% | † Partial |
| ZalgoTextInFields | 79 | 3.7% | † Partial |
| FullwidthBracketsFields | 46 | 2.1% | † Partial |
| AbugidasInStringFields | 45 | 2.1% | † Partial |

**Total Unicode attacks:** 1,315 errors (includes some that also caused 500 errors)

#### Current Mitigation

Existing middleware: `UnicodeNormalizationMiddleware()` in [cmd/server/main.go:1145](../../../cmd/server/main.go#L1145)

**Check implementation:**
```bash
grep -A 50 "func UnicodeNormalizationMiddleware" api/*.go
```

#### Enhancement Strategy

**Most Unicode attacks return 400 (correct behavior)**

The majority of these "errors" are actually **correct behavior** - the API is rejecting malicious Unicode. However:

1. **Some cause 500 errors** (covered in Priority 1)
2. **Some get through and may pose XSS risk**
3. **Error messages may not be clear**

**Validation Strategy:**

```go
// api/unicode_validator.go
type UnicodeValidator struct {
    // Dangerous patterns to reject
    bidirectionalOverrides []rune
    zeroWidthChars        []rune
    hangulFillers         []rune
    combiningDiacriticals [][2]rune // ranges
}

func NewUnicodeValidator() *UnicodeValidator {
    return &UnicodeValidator{
        bidirectionalOverrides: []rune{
            '\u202E', // Right-to-Left Override
            '\u202D', // Left-to-Right Override
            '\u202A', // Left-to-Right Embedding
            '\u202B', // Right-to-Left Embedding
            '\u202C', // Pop Directional Formatting
        },
        zeroWidthChars: []rune{
            '\u200B', // Zero Width Space
            '\u200C', // Zero Width Non-Joiner
            '\u200D', // Zero Width Joiner
            '\uFEFF', // Zero Width No-Break Space
        },
        hangulFillers: []rune{
            '\u3164', // Hangul Filler
        },
        combiningDiacriticals: [][2]rune{
            {'\u0300', '\u036F'}, // Combining Diacritical Marks
        },
    }
}

func (v *UnicodeValidator) ContainsDangerousChars(s string) (bool, string) {
    for _, r := range s {
        // Check each category
        if v.isBidirectionalOverride(r) {
            return true, fmt.Sprintf("contains bidirectional override character U+%04X", r)
        }
        // ... check other categories
    }
    return false, ""
}
```

**Admin Endpoint Focus:**

Admin endpoints should be MORE restrictive than user endpoints:

```go
// api/middleware/admin_unicode_validator.go
func AdminUnicodeValidationMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Only apply to /admin/* paths
        if !strings.HasPrefix(c.Request.URL.Path, "/admin") {
            c.Next()
            return
        }

        // Stricter validation for admin endpoints
        // Reject ANY non-ASCII in certain fields
        // ... implementation
    }
}
```

#### Testing Strategy

**Add comprehensive Unicode attack tests:**

```go
// api/unicode_attacks_test.go
func TestUnicodeAttacks_BidirectionalOverride(t *testing.T) {
    attacks := []string{
        "normaltext\u202Eattack",  // Right-to-Left Override
        "\u202Dattack",             // Left-to-Right Override
    }

    for _, attack := range attacks {
        resp := makeRequest("/addons", gin.H{"name": attack})
        assert.Equal(t, 400, resp.StatusCode, "Must reject bidirectional override")
        assert.Contains(t, resp.Body, "bidirectional")
    }
}

func TestUnicodeNormalization_LegitimateInternationalText(t *testing.T) {
    // Ensure we don't break legitimate use cases
    legitimate := []string{
        "Â,û",           // Japanese
        "'D91(J)",         // Arabic
        "‚—ËŸÍ",           // Hebrew
        "\m¥",           // Korean (without filler)
        "ïªª∑Ωπ∫¨",        // Greek
        "@825B",          // Russian
    }

    for _, text := range legitimate {
        resp := makeRequest("/addons", gin.H{"name": text})
        assert.NotEqual(t, 400, resp.StatusCode,
            "Must accept legitimate international text: %s", text)
    }
}
```

#### Success Criteria

- [ ] Zero 500 errors from Unicode input
- [ ] Consistent 400 responses for dangerous Unicode
- [ ] Clear error messages identifying specific Unicode issue
- [ ] Legitimate international text still accepted
- [ ] Comprehensive test suite for all attack types
- [ ] Admin endpoints have stricter validation
- [ ] Documentation of Unicode policy

**Estimated Effort:** 3-4 days

---

## Priority 4: Polish & Optimization

### 4.1 Improve Error Messages (1,391 errors, 64.4%)

**Status:** These are mostly correct 400 responses, but error messages could be better

**Current:** Generic "Bad Request" messages
**Target:** Specific, actionable error messages following RFC 7807

#### Implementation

```go
// api/errors.go
type ProblemDetails struct {
    Type     string                 `json:"type"`
    Title    string                 `json:"title"`
    Status   int                    `json:"status"`
    Detail   string                 `json:"detail"`
    Instance string                 `json:"instance"`
    Extensions map[string]interface{} `json:"-"`
}

// Example usage
func (s *Server) HandleInvalidInput(c *gin.Context, err error) {
    problem := ProblemDetails{
        Type:   "https://api.tmi.example.com/errors/invalid-input",
        Title:  "Invalid Input",
        Status: 400,
        Detail: fmt.Sprintf("The request contains invalid data: %s", err.Error()),
        Instance: c.Request.URL.Path,
    }

    c.JSON(400, problem)
}
```

**Specific error messages for each validation type:**

- Unicode errors: "Request contains bidirectional override character (U+202E)"
- Numeric errors: "Value 999999999 exceeds maximum 2147483647"
- Missing fields: "Required field 'name' is missing"
- Format errors: "Invalid UUID format in 'id' parameter"

#### Success Criteria

- [ ] All 400 responses follow RFC 7807 format
- [ ] Error messages are specific and actionable
- [ ] Error type URIs point to documentation
- [ ] Client developers find errors helpful

**Estimated Effort:** 2 days

---

### 4.2 Optimize 404 Handling (231 errors, 10.7%)

**Analysis:** Need to distinguish between legitimate 404s and bugs

**Categories:**
1. Random UUIDs (RandomResources fuzzer) - **Expected 404**
2. Valid UUIDs on Happy Path - **BUG** (covered in Priority 1)
3. Malformed UUIDs - **Should return 400, not 404**

#### Fix Strategy

```go
// api/middleware/uuid_validator.go (already exists, verify)
func UUIDValidationMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Extract all UUID parameters
        params := extractUUIDParams(c.Params)

        for name, value := range params {
            if !isValidUUID(value) {
                c.JSON(400, gin.H{
                    "error": "invalid_parameter",
                    "error_description": fmt.Sprintf(
                        "Parameter '%s' must be a valid UUID, got: %s",
                        name, value,
                    ),
                })
                c.Abort()
                return
            }
        }
        c.Next()
    }
}
```

#### Success Criteria

- [ ] Malformed UUIDs return 400, not 404
- [ ] Valid UUIDs that don't exist return 404 with clear message
- [ ] Random resource IDs properly return 404
- [ ] Error messages distinguish "not found" from "invalid format"

**Estimated Effort:** 1 day

---

## Implementation Roadmap

### Phase 1: Critical Bug Fixes (Week 1-2)

**Goal:** Zero production bugs, 100% Happy Path success

| Task | Effort | Files | Priority |
|------|--------|-------|----------|
| Fix 500 errors on `/addons` | 1d | api/addons.go | P1.1 |
| Fix 500 errors on `/admin/quotas/*` | 1d | api/admin_quotas.go | P1.1 |
| Fix schema validation on `/client-credentials` | 1d | api/client_credentials.go | P1.2 |
| Fix schema validation on threats | 0.5d | api/threats.go | P1.2 |
| Fix Happy Path failures | 1d | Multiple files | P1.3 |
| Add comprehensive error logging | 0.5d | api/middleware/ | P1.1 |
| Write post-mortem documents | 0.5d | docs/ | P1.1 |

**Total:** 5.5 days

**Deliverables:**
- [ ] Zero 500 errors
- [ ] Zero schema validation failures
- [ ] 100% Happy Path success rate
- [ ] Comprehensive test suite
- [ ] Post-mortem docs

**Success Metrics:**
- Error rate: 9.79% í <4%
- 500 errors: 150 í 0
- Schema errors: 130 í 0
- Happy Path: 98.56% í 100%

---

### Phase 2: API Completeness (Week 3)

**Goal:** Complete API surface, resolve conflicts

| Task | Effort | Files | Priority |
|------|--------|-------|----------|
| Implement Transfer-Encoding rejection | 0.5d | api/middleware/ | P2.1 |
| Fix/implement `/admin/groups` | 1d | api/admin_groups.go | P2.1 |
| Investigate 409 conflicts | 1d | Multiple files | P2.2 |
| Add idempotency handling | 1d | Multiple files | P2.2 |
| Update OpenAPI spec | 0.5d | docs/reference/apis/ | P2.1 |

**Total:** 4 days

**Deliverables:**
- [ ] Zero 501 responses
- [ ] All 409 responses documented or fixed
- [ ] Updated OpenAPI spec
- [ ] Concurrency test suite

**Success Metrics:**
- Error rate: <4% í <2%
- 501 errors: 47 í 0
- Undefined 409s: 47 í 0 or documented

---

### Phase 3: Security Hardening (Week 4)

**Goal:** Protect against Unicode attacks, harden admin endpoints

| Task | Effort | Files | Priority |
|------|--------|-------|----------|
| Audit Unicode validation middleware | 0.5d | api/middleware/ | P3.1 |
| Enhance Unicode validator | 1d | api/unicode_validator.go | P3.1 |
| Add admin-specific validation | 1d | api/middleware/ | P3.1 |
| Comprehensive Unicode tests | 1.5d | api/*_test.go | P3.1 |
| Update security documentation | 0.5d | docs/reference/security/ | P3.1 |

**Total:** 4.5 days

**Deliverables:**
- [ ] Enhanced Unicode validation
- [ ] Zero 500 from Unicode
- [ ] Admin endpoint hardening
- [ ] Comprehensive test coverage
- [ ] Security documentation

**Success Metrics:**
- Error rate: <2% í <1%
- Unicode 500s: ~50 í 0
- Admin endpoint errors: 658 í <100

---

### Phase 4: Polish (Week 5)

**Goal:** Excellent developer experience, clear error messages

| Task | Effort | Files | Priority |
|------|--------|-------|----------|
| Implement RFC 7807 errors | 1d | api/errors.go | P4.1 |
| Improve all error messages | 1d | Multiple files | P4.1 |
| Optimize 404 handling | 0.5d | api/middleware/ | P4.2 |
| Write error handling docs | 0.5d | docs/reference/apis/ | P4.1 |

**Total:** 3 days

**Deliverables:**
- [ ] RFC 7807 compliant errors
- [ ] Clear, actionable error messages
- [ ] Developer documentation
- [ ] Optimized 404 handling

**Success Metrics:**
- Error rate: <1% (target achieved)
- Developer satisfaction: High
- Error message clarity: Excellent

---

## Testing Strategy

### Continuous Testing

**During Development:**
```bash
# After each fix, run CATS on specific endpoint
make cats-fuzz-path ENDPOINT=/addons

# Parse results
make parse-cats-results

# Check improvement
make query-cats-results
```

**Regression Testing:**
```bash
# Full CATS run weekly
make cats-fuzz

# Compare to baseline
make analyze-cats-results

# Fail if error rate increases
```

### Test Coverage Requirements

| Component | Unit Test Coverage | Integration Tests |
|-----------|-------------------|-------------------|
| Error handlers | 100% | All error codes |
| Validation middleware | 100% | All Unicode attacks |
| Schema validation | 100% | All endpoints |
| Happy Path | 100% | All endpoints |
| Admin endpoints | 100% | RBAC + validation |

### Performance Testing

Monitor for performance regressions:
- Response time: <100ms p95 (no change)
- Throughput: >1000 req/s (no change)
- Memory: No leaks from error handling

---

## Monitoring & Alerting

### Key Metrics

**Production Dashboards:**
- 500 error rate (should always be 0)
- 400 error rate by endpoint
- Schema validation failures
- Response time by endpoint
- Unicode attack detection rate

**Alerts:**

**Critical (page on-call):**
- Any 500 error in production
- Schema validation failure rate >0.1%
- Happy Path success <99%

**Warning (Slack):**
- Error rate increases >10%
- New error types detected
- Unicode attack spike (>1000/min)

### Weekly Review

**Automated report:**
- CATS success rate trend
- Error breakdown by category
- Top error-prone endpoints
- New error types introduced

---

## Risk Assessment

### High Risk Changes

1. **Schema Validation Fixes**
   - **Risk:** Breaking changes to API responses
   - **Mitigation:** Version API, maintain backward compatibility
   - **Rollback:** Feature flag for schema validation

2. **500 Error Fixes**
   - **Risk:** New bugs introduced
   - **Mitigation:** Comprehensive unit tests
   - **Rollback:** Git revert individual commits

### Medium Risk Changes

1. **Unicode Validation Enhancement**
   - **Risk:** Blocking legitimate international text
   - **Mitigation:** Extensive testing with real data
   - **Rollback:** Feature flag for strict mode

2. **Admin Endpoint Changes**
   - **Risk:** Breaking admin tools
   - **Mitigation:** Staging deployment first
   - **Rollback:** Separate admin API deployment

### Low Risk Changes

1. **Error Message Improvements**
   - **Risk:** Minimal (cosmetic)
   - **Mitigation:** N/A
   - **Rollback:** Easy (text changes only)

2. **404 Optimization**
   - **Risk:** Low
   - **Mitigation:** Unit tests
   - **Rollback:** Simple revert

---

## Success Criteria

### Overall Goals

| Metric | Current | Target | Stretch |
|--------|---------|--------|---------|
| **Success Rate** | 90.21% | 98% | 99% |
| **Error Rate** | 9.79% | <2% | <1% |
| **500 Errors** | 150 (6.9%) | 0 | 0 |
| **Schema Failures** | 130 (6.0%) | 0 | 0 |
| **Happy Path** | 98.56% | 100% | 100% |

### Phase Completion Criteria

**Phase 1 (Critical):**
- [ ] 0 internal server errors (500)
- [ ] 0 schema validation failures
- [ ] 100% Happy Path success
- [ ] Error rate <4%

**Phase 2 (Complete):**
- [ ] 0 unimplemented endpoints (501)
- [ ] All 409 conflicts documented/fixed
- [ ] Error rate <2%

**Phase 3 (Secure):**
- [ ] 0 Unicode-caused 500 errors
- [ ] Admin endpoints hardened
- [ ] Error rate <1%

**Phase 4 (Polish):**
- [ ] RFC 7807 compliant errors
- [ ] Clear error messages
- [ ] Success rate e98%

---

## Appendix: Quick Reference

### Database Queries

**Get 500 error details:**
```sql
SELECT path, fuzzer, COUNT(*) as count
FROM test_results_filtered_view
WHERE (result_reason LIKE '%500%')
GROUP BY path, fuzzer
ORDER BY count DESC;
```

**Get schema validation failures:**
```sql
SELECT path, fuzzer, response_code, COUNT(*) as count
FROM test_results_filtered_view
WHERE result_reason = 'Not matching response schema'
GROUP BY path, fuzzer, response_code
ORDER BY count DESC;
```

**Get Happy Path failures (non-401):**
```sql
SELECT path, result_reason, response_code, COUNT(*) as count
FROM test_results_filtered_view
WHERE fuzzer = 'HappyPath'
  AND result != 'success'
  AND response_code != 401
GROUP BY path, result_reason, response_code
ORDER BY count DESC;
```

**Get 409 conflicts:**
```sql
SELECT path, fuzzer, COUNT(*) as count
FROM test_results_filtered_view
WHERE response_code = 409
GROUP BY path, fuzzer
ORDER BY count DESC;
```

### File Reference

**Key Implementation Files:**
- [api/addons.go](../../../api/addons.go) - Addons endpoint (42 500 errors)
- [api/admin_quotas.go](../../../api/admin_quotas.go) - Quota endpoints (56 500 errors)
- [api/client_credentials.go](../../../api/client_credentials.go) - Client creds (38 schema errors)
- [api/threats.go](../../../api/threats.go) - Threats (10 schema errors)
- [api/admin_groups.go](../../../api/admin_groups.go) - Admin groups (37 501 errors)

**Middleware Files:**
- [cmd/server/main.go:1137-1146](../../../cmd/server/main.go#L1137-L1146) - Validation middleware
- [api/unicode_validator.go](../../../api/unicode_validator.go) - Unicode validation
- [api/middleware/](../../../api/middleware/) - All middleware

**Documentation:**
- [OpenAPI Spec](../../../docs/reference/apis/tmi-openapi.json) - API contract
- [Error Handling](../../../docs/reference/apis/error-handling.md) - To be created
- [Unicode Policy](../../../docs/reference/security/unicode-handling.md) - To be created

---

## Document History

| Date | Version | Changes | Author |
|------|---------|---------|--------|
| 2025-12-06 | 1.0 | Initial plan for non-401 errors | Claude Code |

---

**Status:** Draft - Awaiting Review
**Review Required:** Engineering Lead, Security Team
**Estimated Duration:** 4-5 weeks
**Target Completion:** TBD
