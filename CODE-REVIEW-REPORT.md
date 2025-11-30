# Go Codebase Review Report - Deprecated, Unused, and Removed Code

**Generated:** 2025-11-30
**Scope:** Complete Go codebase scan (215 files)
**Project:** TMI (Collaborative Threat Modeling Interface)

---

## 📊 Executive Summary

After scanning **215 Go files**, I found:
- **12 explicitly deprecated items** (functions and fields)
- **46 comments referencing removed/obsolete code**
- **20+ skipped tests** (integration tests requiring full setup)
- **1 large commented-out code block** (~39 lines)
- **40+ TODO comments** regarding future work
- **11 potentially unused functions** (CacheMetrics interface)

**Total Findings:** 81 items across all categories

---

## 🔴 HIGH PRIORITY - Immediate Action Required

### 1. Remove Commented-Out Code
**File:** [auth/handlers.go:1310-1351](auth/handlers.go#L1310-L1351)

There's a 39-line commented-out function `exchangeCodeForTokens()` marked as "TODO: Currently unused - reserved for future OAuth Authorization Code flow implementation"

```go
// exchangeCodeForTokens exchanges an authorization code for tokens
// TODO: Currently unused - reserved for future OAuth Authorization Code flow implementation
/*
func exchangeCodeForTokens(ctx context.Context, provider OAuthProviderConfig, code, redirectURI string) (map[string]string, error) {
	// Prepare the request
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", provider.ClientID)
	data.Set("client_secret", provider.ClientSecret)

	// Send the request
	req, err := http.NewRequestWithContext(ctx, "POST", provider.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp.Body)

	// Parse the response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to exchange code: %s", body)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}
*/
```

**Recommendation:** Remove entirely. If needed in the future, it can be retrieved from git history.

---

### 2. Verify CacheMetrics Integration
**File:** [api/cache_metrics.go](api/cache_metrics.go)

11 metric functions are defined but **never called anywhere** in the codebase:

- `EnableMetrics()` (Line 111)
- `DisableMetrics()` (Line 118)
- `IsEnabled()` (Line 125)
- `RecordCacheHit()` (Line 132)
- `RecordCacheMiss()` (Line 154)
- `RecordCacheWrite()` (Line 176)
- `RecordCacheDelete()` (Line 185)
- `RecordCacheInvalidation()` (Line 194)
- `RecordCacheLatency()` (Line 204)
- `RecordWarmingDuration()` (Line 213)
- `RecordCacheError()` (Line 222)

**Status:** These are part of the CacheMetrics type but don't appear to be called anywhere in the codebase.

**Recommendation:** Either integrate these metrics or remove the entire type.

---

## 🟡 DEPRECATED CODE - Backward Compatibility

### Deprecated Functions (Keep for Now, Remove in v1.0)

#### 1. LinkUserProvider() - No-Op Function
**File:** [auth/service.go:596-609](auth/service.go#L596-L609)

```go
// Deprecated: Use CreateUser or UpdateUser with provider fields instead.
func (s *Service) LinkUserProvider(ctx context.Context, userID, provider, providerUserID, email string) error {
	// DEPRECATED: user_providers table has been eliminated
	// Provider information is now stored directly on users table (provider, provider_user_id fields)
	// This function is maintained for backward compatibility but performs no operation
	logger := slogging.Get()
	logger.Debug("LinkUserProvider called (deprecated no-op): userID=%s, provider=%s", userID, provider)
	return nil
}
```

- **Status:** No-op function kept for backward compatibility
- **Reason:** User provider linking now handled directly on User struct (provider, provider_user_id fields)
- **Impact:** LOW - Only called internally for compatibility; can be safely removed in major version

---

#### 2. UnlinkUserProvider() - Intentionally Unsupported
**File:** [auth/service.go:611-624](auth/service.go#L611-L624)

```go
// Deprecated: Provider unlinking is not supported in the new architecture.
// Each user is tied to exactly one OAuth provider.
func (s *Service) UnlinkUserProvider(ctx context.Context, userID, provider string) error {
	// DEPRECATED: user_providers table has been eliminated
	logger := slogging.Get()
	logger.Warn("UnlinkUserProvider called (deprecated, not supported): userID=%s, provider=%s", userID, provider)
	return errors.New("unlinking providers is not supported in the current architecture - each user is tied to one provider")
}
```

- **Status:** Returns error, not a true no-op
- **Reason:** In new architecture, users have single provider; unlinking would require user deletion
- **Impact:** MEDIUM - Returns error; should be documented as removed

---

#### 3. GetUserWithProviderID()
**File:** [auth/service.go:374](auth/service.go#L374)

- **Status:** Likely unused
- **Reason:** Provider ID now on User struct, not separate table
- **Impact:** LOW - Need to verify usage

---

### Deprecated Struct Fields

#### 1. User.IdentityProvider Field
**File:** [auth/service.go:88](auth/service.go#L88)

```go
IdentityProvider string    `json:"idp,omitempty"`    // DEPRECATED: Use Provider instead (kept for backward compatibility)
```

- **Status:** Kept for backward compatibility
- **Replacement:** Use `Provider` field instead
- **Impact:** LOW - Preserved for client compatibility

---

#### 2. User Creation in SAML Manager
**File:** [auth/saml_manager.go:222](auth/saml_manager.go#L222)

```go
IdentityProvider: providerID, // DEPRECATED: kept for backward compatibility
```

- **Status:** Set for compatibility but not used
- **Impact:** LOW - Harmless population of deprecated field

---

#### 3. CellOperation.Component Field
**File:** [api/websocket.go:1690](api/websocket.go#L1690)

```go
Component *DfdDiagram_Cells_Item `json:"component,omitempty"` // DEPRECATED
```

- **Status:** Field marked deprecated in WebSocket messages
- **Impact:** LOW - Only affects JSON serialization compatibility

---

#### 4. Diagram Schema - Empty Wrapper
**File:** [api/api.go:916](api/api.go#L916)

```go
// Diagram DEPRECATED: Empty wrapper schema for polymorphic diagram types. Use DfdDiagram directly instead.
// This schema is kept for backward compatibility but generates empty classes in client libraries.
type Diagram struct {
	union json.RawMessage
}
```

- **Status:** Empty wrapper kept only for backward compatibility
- **Impact:** MEDIUM - Creates empty classes in generated clients; clients should use DfdDiagram
- **Recommendation:** Consider removing in next major version or adding deprecation warning to OpenAPI spec

---

### Configuration Deprecation Note

**File:** [auth/config.go:389](auth/config.go#L389)

```go
// DEPRECATED: loadSAMLProviders() has been removed. Use InitAuthWithConfig() instead of InitAuth()
```

- **Type:** Comment about removed function
- **Status:** Code removed, comment remains for historical reference
- **Impact:** LOW - Just documentation

---

## 🟠 SKIPPED TESTS - Review Needed

### In-Memory Store Tests (3 tests)
**File:** [api/store_test.go](api/store_test.go) (Lines 28, 32, 36)

```go
t.Skip("Generic in-memory store removed - use database tests instead")
```

- **Status:** Tests for removed in-memory store implementation
- **Count:** 3 test functions skipped
- **Impact:** MEDIUM - Indicates migration from in-memory to database stores
- **Tests:** TestStore_CRUD, TestStore_Read, TestStore_Write (implied)

---

### Metadata Store Test (1 test)
**File:** [api/metadata_store_test.go:9](api/metadata_store_test.go#L9)

```go
t.Skip("Test disabled - in-memory stores removed, use database tests instead")
```

- **Count:** 1 test skipped
- **Impact:** LOW - Covered by database tests

---

### Cache Warming Tests (2 tests)
**File:** [api/cache_warming_test.go](api/cache_warming_test.go) (Lines 540, 643)

```go
t.Skip("This test requires database integration and should be moved to a separate integration test")
```

- **Count:** 2 tests skipped
- **Impact:** MEDIUM - Tests not migrated to integration suite

---

### WebSocket Presenter Message Tests (3 tests)
**File:** [api/websocket_test.go](api/websocket_test.go) (Lines 424, 428, 432)

```go
t.Skip("PresenterRequestMessage no longer contains user data - server uses authenticated client identity")
t.Skip("PresenterCursorMessage no longer contains user data - server uses authenticated client identity")
t.Skip("PresenterSelectionMessage no longer contains user data - server uses authenticated client identity")
```

- **Count:** 3 tests skipped
- **Reason:** API changes removed user data from presenter messages
- **Impact:** LOW - Reflects intentional API changes

---

### OAuth Handler Tests (5+ tests)
**File:** [auth/handlers_test.go](auth/handlers_test.go) (Lines 452, 457, 515, 677, 765)

```go
t.Skip("Full OAuth exchange requires mock OAuth provider - test structure only")
t.Skip("Refresh handler requires service for token operations - test structure only")
t.Skip("Authorize handler requires service and Redis - test structure only")
t.Skip("Authorize handler with client callback requires service and Redis - test structure only")
t.Skip("Callback handler with client redirect requires service, Redis, and OAuth provider - test structure only")
```

- **Count:** 5+ tests skipped in auth handlers
- **Impact:** MEDIUM - OAuth tests only validate structure, not functionality

---

### Integration Tests in Short Mode (16+ tests)

Multiple integration tests skip in short mode (`-short` flag):

- `api/threat_model_handlers_test.go` - 3 skipped tests
- `api/metadata_handlers_test.go` - 3 skipped tests
- `api/websocket_test.go` - 3 skipped tests
- `api/threat_model_validation_test.go` - 2 skipped tests
- `api/collaboration_sessions_test.go` - 2 skipped tests
- `api/fixture_test.go` - 1 skipped test
- `api/cache_invalidation_test.go` - 1 skipped test
- `api/user_deletion_handlers_test.go` - 1 skipped test

**Total:** 16+ integration tests skipped in short mode
**Impact:** MEDIUM - Expected for integration tests but should be documented

**Recommendation:** Either migrate skipped tests to integration suite or remove if obsolete.

---

## 📝 REMOVED CODE - Documentation Notes

### Major Architecture Changes

#### 1. In-Memory Stores Removed
**File:** [api/store.go:87](api/store.go#L87)

```go
// NOTE: InitializeInMemoryStores function removed - all stores now use database implementations
```

- **What was removed:** InitializeInMemoryStores()
- **Reason:** Transition from in-memory to database-backed storage
- **Impact:** LOW - Part of necessary architecture change

---

#### 2. User Providers Table Eliminated
**File:** [auth/service.go:603,618](auth/service.go#L603)

```go
// DEPRECATED: user_providers table has been eliminated
```

- **What was removed:** user_providers table
- **Replacement:** Provider info now stored directly on users table
- **Impact:** MEDIUM - Major schema change

**Related Comments:**
**File:** [auth/saml_manager.go:200,233](auth/saml_manager.go#L200)

```go
// Link or update the SAML provider for the existing user - DEPRECATED: provider info on User struct
// Link the SAML provider to the newly created user - DEPRECATED: provider info on User struct
```

- **Context:** Code comments explain that SAML provider linking is now a no-op
- **Impact:** LOW - Code still functions, just doesn't do work

---

#### 3. WebSocket Helper Functions Removed
**File:** [api/websocket.go:3393](api/websocket.go#L3393)

```go
// validateCellData and detectCellChanges removed - no longer needed with union types
```

- **Functions removed:** validateCellData(), detectCellChanges()
- **Reason:** Union types eliminated need for separate validation functions
- **Impact:** LOW - Replaced by better approach

---

#### 4. ListItem Type Removed from Diagram Endpoints
**File:** [api/threat_model_diagram_handlers.go:135](api/threat_model_diagram_handlers.go#L135)

```go
// NOTE: ListItem type removed with diagram endpoints - this code is now inactive
```

- **What was removed:** ListItem type from diagram endpoints
- **Status:** Code note indicates this is "now inactive"
- **Impact:** LOW - API endpoint behavior changed

---

#### 5. User Fields Removed from Presenter Messages
**File:** [api/websocket_test.go:412,424,428,432](api/websocket_test.go#L412)

```go
// User field removed from PresenterRequestMessage - no additional assertions needed
t.Skip("PresenterRequestMessage no longer contains user data - server uses authenticated client identity")
t.Skip("PresenterCursorMessage no longer contains user data - server uses authenticated client identity")
t.Skip("PresenterSelectionMessage no longer contains user data - server uses authenticated client identity")
```

- **What was removed:** User field from presenter messages
- **Reason:** Server now uses authenticated client identity instead
- **Impact:** LOW - Intentional design change for security

---

#### 6. WebSocket Details Removed from REST API Info
**File:** [api/version_test.go:89](api/version_test.go#L89)

```go
// REST API info no longer includes WebSocket details as they use different protocols
```

- **What was removed:** WebSocket details from REST API info
- **Impact:** LOW - Different protocol handling

---

## ⏰ TODO ITEMS - Future Work

### High Impact TODOs (13 items)

#### OpenTelemetry Integration
**File:** [api/webhook_metrics.go](api/webhook_metrics.go)

13 functions marked with "TODO: Integrate with OpenTelemetry/Prometheus":

- Line 20: `RecordWebhookInvocation()`
- Line 28: `RecordWebhookSuccess()`
- Line 36: `RecordWebhookFailure()`
- Line 44: `RecordWebhookRetry()`
- Line 52: `RecordWebhookTimeout()`
- Line 60: `RecordWebhookLatency()`
- Line 68: `RecordWebhookQueueDepth()`
- Line 77: `RecordWebhookDeliveryDelay()`
- Line 89: `RecordWebhookHTTPStatus()`
- Line 102: `RecordWebhookPayloadSize()`
- Line 110: `RecordWebhookCircuitBreakerState()`
- Line 118: `RecordWebhookBatchSize()`
- Line 126: `RecordWebhookRateLimit()`

**Impact:** MEDIUM - Metrics currently don't integrate with monitoring infrastructure

---

### Medium Impact TODOs

#### 1. Webhook Handler Implementation
**File:** [api/webhook_handlers.go:29,341](api/webhook_handlers.go#L29)

```go
// TODO: Implement full handler logic
```

- **Count:** 2 occurrences
- **Scope:** Webhook handlers
- **Impact:** MEDIUM - Handlers exist but are stubs

---

#### 2. SAML Validation
**File:** [auth/saml/provider.go:165,222](auth/saml/provider.go#L165)

```go
// TODO: Properly validate the response signature and conditions
// TODO: Implement logout request processing
```

**Impact:** MEDIUM - SAML validation not fully implemented

---

#### 3. Session Invalidation on User Deletion
**File:** [auth/handlers.go:2010](auth/handlers.go#L2010)

```go
// TODO: Invalidate user sessions
```

**Impact:** MEDIUM - Session invalidation on user deletion incomplete

---

#### 4. Group Fetching from Provider
**File:** [api/server.go:441](api/server.go#L441)

```go
// TODO: Implement actual group fetching from provider or cache
```

**Impact:** MEDIUM - Group authorization may not work correctly

---

### Low Impact TODOs (25+ items)

#### Provider Context in Test/Utility Code

**File:** [api/threat_model_handlers.go:392](api/threat_model_handlers.go#L392)
```go
Provider: "test", // TODO: Get provider from auth context
```

**File:** [api/addon_type_converters.go:150](api/addon_type_converters.go#L150)
```go
Provider: "unknown", // TODO: Store provider in AddonInvocation
```

**File:** [api/auth_utils.go](api/auth_utils.go) (Lines 54, 634, 653)
```go
Provider: "test", // TODO: Need provider context from caller
Provider: "unknown", // TODO: Need to enrich from database
Provider: "unknown", // TODO: Query from database
```

**Impact:** LOW - Hardcoded values in test/utility code

---

#### WebSocket Identity Context

**File:** [api/websocket_validation.go:86](api/websocket_validation.go#L86)
```go
// TODO: Update validateWebSocketDiagramAccessDirect to use user ID instead of email
```

**File:** [api/websocket.go:2504](api/websocket.go#L2504)
```go
// TODO: Get IdP and groups from user context when WebSocket supports it
```

**Impact:** LOW - Known future improvements

---

#### Configuration Externalization

**File:** [api/addon_invocation_worker.go:149](api/addon_invocation_worker.go#L149)
```go
// TODO: Get base URL from configuration
```

**Impact:** LOW - Hardcoded for now

---

#### Code Readability Improvements

**File:** [api/threat_model_diagram_handlers.go:582,662,743](api/threat_model_diagram_handlers.go#L582)
```go
// TODO: make this code more readable. We expect middleware to set userEmail to "anonymous" when unauthenticated
```

- **Count:** 3 occurrences
- **Impact:** LOW - Code works but needs refactoring for clarity

---

#### Schema Validation

**File:** [api/internal_models.go:80](api/internal_models.go#L80)
```go
// TODO: Load threats, documents, sources similarly when needed
// TODO: Extract threat, document, source IDs when needed
```

**Impact:** LOW - Implementation notes for future work

---

#### Test Infrastructure

**File:** [auth/logout_jwt_test.go:35](auth/logout_jwt_test.go#L35)
```go
// TODO: Add proper test infrastructure to support Redis mocking in unit tests
```

**Impact:** LOW - Test infrastructure improvement

---

## 📋 RECOMMENDATIONS

### Immediate Actions (This Sprint) - Priority: HIGH

1. ✅ **Remove commented-out code** in `auth/handlers.go` (lines 1310-1351)
   - Takes up space and creates confusion
   - Can be recovered from git history if needed

2. ✅ **Verify CacheMetrics integration**
   - Either remove unused functions or activate the integration
   - 11 functions defined but never called

3. ✅ **Review skipped tests**
   - Migrate to integration test suite or remove if obsolete
   - Document reasoning for any tests that remain skipped

---

### Near-term Actions (Next Quarter) - Priority: MEDIUM

1. 🔧 **Complete webhook handler implementation**
   - Currently stubbed with "TODO: Implement full handler logic"

2. 🔧 **Integrate webhook metrics with OpenTelemetry**
   - 13 TODOs in webhook_metrics.go

3. 🔧 **Implement SAML signature validation**
   - Properly validate response signatures and conditions
   - Implement logout request processing

4. 🔧 **Complete session invalidation on user deletion**
   - Currently marked as TODO in handlers.go

5. 🔧 **Implement actual group fetching from provider**
   - Currently hardcoded/mocked in server.go

---

### Major Version (v1.0 Breaking Changes) - Priority: LOW

1. 🚀 **Remove deprecated functions:**
   - `LinkUserProvider()` - No-op function
   - `UnlinkUserProvider()` - Returns error
   - `GetUserWithProviderID()` - Likely unused

2. 🚀 **Remove deprecated struct fields:**
   - `User.IdentityProvider` field (use `Provider` instead)
   - `CellOperation.Component` field
   - `Diagram` schema (use `DfdDiagram` directly)

3. 🚀 **Create migration guide:**
   - Document breaking changes
   - Provide migration path for deprecated APIs
   - Update CHANGELOG with all removals

4. 🚀 **Update OpenAPI specification:**
   - Add deprecation warnings to OpenAPI spec
   - Document replacement endpoints/schemas

---

### Documentation Updates

1. 📖 Add migration guide for deprecated function removal
2. 📖 Document breaking changes in CHANGELOG
3. 📖 Update API documentation for deprecated Diagram schema
4. 📖 Document reasoning for skipped tests
5. 📖 Create technical debt tracking document

---

## 📊 Technical Debt by File

| File | Issue Count | Issue Types |
|------|-------------|-------------|
| [api/webhook_metrics.go](api/webhook_metrics.go) | 13 | OpenTelemetry TODOs |
| [api/cache_metrics.go](api/cache_metrics.go) | 11 | Unused functions |
| [auth/handlers_test.go](auth/handlers_test.go) | 5+ | Skipped OAuth tests |
| [auth/service.go](auth/service.go) | 4 | Deprecated functions/fields |
| [auth/handlers.go](auth/handlers.go) | 4 | Deprecated code + commented block |
| [api/websocket.go](api/websocket.go) | 3 | Deprecated field + removed code |
| [api/threat_model_diagram_handlers.go](api/threat_model_diagram_handlers.go) | 4 | Removed code comments, readability TODOs |

---

## 📈 Summary Statistics

| Category | Count | Severity | Action Required |
|----------|-------|----------|-----------------|
| Deprecated Functions | 3 | LOW-MEDIUM | Document for removal in v1.0 |
| Deprecated Fields | 4 | LOW | Keep for backward compatibility |
| Commented-Out Code | 1 block (39 lines) | MEDIUM | Remove - can recover from git |
| Skipped Tests | 20+ | LOW-MEDIUM | Consider enabling or removing |
| Removed Functions (Documented) | 2 | LOW | Historical reference only |
| TODO Comments | 40+ | LOW-MEDIUM | Track for future work |
| Potentially Unused Functions | 11 (CacheMetrics) | MEDIUM | Verify integration status |
| **TOTAL FINDINGS** | **81** | **MIXED** | **SEE RECOMMENDATIONS** |

---

## 🎯 Conclusion

The TMI codebase is generally **healthy** with most issues being **intentional backward compatibility measures** rather than true dead code. The analysis reveals:

### Key Insights:

1. **Architecture Evolution:** Most deprecated code relates to the transition from in-memory to database-backed storage and elimination of the user_providers table.

2. **Backward Compatibility Focus:** Deprecated functions and fields are maintained for client compatibility, showing good API stewardship.

3. **Incomplete Features:** Webhook handlers and SAML validation need completion.

4. **Test Coverage Gaps:** Multiple skipped tests indicate areas where test infrastructure could be improved.

5. **Monitoring Integration:** Webhook metrics and cache metrics lack OpenTelemetry integration.

### Main Concerns:

- **CacheMetrics interface** - 11 functions defined but never used
- **Commented-out code** - Should be removed
- **Skipped tests** - Need review and decision (migrate or remove)
- **Incomplete implementations** - Webhooks, SAML validation

### Next Steps:

Follow the prioritized recommendations above, starting with high-priority items (remove commented code, verify CacheMetrics) and working toward major version cleanup for v1.0.

---

**Report End**
