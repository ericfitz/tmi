# CATS 409 Conflict Analysis

**Created:** 2025-12-09
**Status:** Completed
**Related:** [CATS Remediation Plan](cats-remediation-plan.md)

## Summary

Investigation of the 47 reported 409 Conflict errors from CATS fuzzing revealed that **all 409 responses are legitimate and expected behavior**. The issues were not bugs in the implementation, but rather **missing documentation in the OpenAPI specification**.

## Root Cause

The TMI API correctly returns 409 Conflict responses when operations conflict with active collaboration sessions or violate uniqueness constraints. However, these legitimate 409 responses were not documented in the OpenAPI specification, causing CATS to flag them as unexpected errors.

## 409 Conflict Scenarios

### 1. Collaboration Session Blocking (Properly Implemented)

**Affected Endpoints:**
- `DELETE /threat_models/{threat_model_id}` - Returns 409 when diagram has active collaboration session
- `PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}` - Returns 409 during active session
- `PATCH /threat_models/{threat_model_id}/diagrams/{diagram_id}` - Returns 409 during active session
- `DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}` - Returns 409 during active session

**Implementation Details:**
- Located in: `api/threat_model_handlers.go`, `api/threat_model_diagram_handlers.go`
- Uses `WebSocketHub.HasActiveSession()` to check for active collaboration
- Prevents data corruption from concurrent modifications during real-time collaboration
- Provides clear error messages guiding users to end collaboration sessions first

**Error Messages:**
```json
{
  "error": "conflict",
  "message": "Cannot modify diagram while collaboration session is active. Please end the collaboration session first."
}
```

**Test Coverage:**
- `api/collaboration_blocking_test.go` - Comprehensive test suite validating all blocking scenarios
- Tests confirm 409 is returned for PUT, PATCH, and DELETE operations during active sessions

### 2. Duplicate Resource Prevention (Properly Implemented)

**Affected Endpoints:**
- `POST /admin/groups` - Returns 409 when group already exists for provider
- `POST /admin/groups/{internal_uuid}/members` - Returns 409 when user already in group
- `POST /admin/administrators` - Returns 409 when administrator grant already exists
- `DELETE /addons/{id}` - Returns 409 when add-on has active invocations
- `POST /invocations/{id}/status` - Returns 409 for invalid status transitions
- `POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate` - Returns 409 when collaboration session already exists

**Implementation Details:**
- Proper uniqueness constraint enforcement
- Clear error messages identifying the conflict
- Already documented in OpenAPI spec (no changes needed)

## Resolution

### Changes Made

**File:** `docs/reference/apis/tmi-openapi.json`

Added 409 response documentation to four endpoints:

1. **DELETE /threat_models/{threat_model_id}**
   ```json
   "409": {
     "description": "Conflict - Cannot delete threat model while a diagram has an active collaboration session",
     "content": {
       "application/json": {
         "schema": {"$ref": "#/components/schemas/Error"}
       }
     }
   }
   ```

2. **PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}**
   ```json
   "409": {
     "description": "Conflict - Cannot modify diagram while collaboration session is active",
     "content": {
       "application/json": {
         "schema": {"$ref": "#/components/schemas/Error"}
       }
     }
   }
   ```

3. **PATCH /threat_models/{threat_model_id}/diagrams/{diagram_id}**
   - Same 409 response as PUT

4. **DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}**
   ```json
   "409": {
     "description": "Conflict - Cannot delete diagram while collaboration session is active",
     "content": {
       "application/json": {
         "schema": {"$ref": "#/components/schemas/Error"}
       }
     }
   }
   ```

### Verification

- **OpenAPI Validation:** Passed (spec remains valid)
- **Lint:** Passed (no code changes needed)
- **Build:** Passed (no code changes needed)
- **Unit Tests:** Passed (all existing tests continue to pass)

## Impact on CATS Remediation

**Original Report:** 47 409 Conflict errors (2.18% of total errors)

**After Fix:** These 409 responses will now be recognized as valid, expected responses documented in the OpenAPI spec.

**Updated Metrics:**
- Expected reduction: ~47 errors from "unexpected" category
- No behavior changes - API continues to work correctly
- Better API contract documentation for client developers

## Conclusion

**Finding:** All 409 Conflict responses in the TMI API are legitimate, expected behavior implementing proper:
1. Concurrency control for collaboration sessions
2. Uniqueness constraint enforcement
3. State validation for resource operations

**Action Taken:** Updated OpenAPI specification to document these responses.

**Result:**
- ✅ No code changes required
- ✅ No test changes required
- ✅ Better API documentation
- ✅ CATS will now recognize these as expected responses
- ✅ Client developers have clear guidance on conflict scenarios

## Recommendations

### For Future Development

1. **Always document expected error responses in OpenAPI spec** - Even if implementation is correct, undocumented responses appear as errors in API testing tools

2. **Consider 409 for these scenarios:**
   - Concurrent modifications (optimistic locking)
   - Uniqueness constraint violations
   - Invalid state transitions
   - Resource conflicts (e.g., active sessions blocking operations)

3. **Testing Strategy:**
   - Continue comprehensive unit testing of conflict scenarios
   - Use CATS validation to verify OpenAPI spec completeness
   - Document all non-2xx responses to improve DX

### Additional Opportunities

The following endpoints currently return 409 but were already documented in the OpenAPI spec (no changes needed):
- `POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate`
- `DELETE /addons/{id}`
- `POST /invocations/{id}/status`
- `POST /admin/administrators`
- `POST /admin/groups`
- `POST /admin/groups/{internal_uuid}/members`

## References

- **Implementation:**
  - `api/threat_model_handlers.go:729`
  - `api/threat_model_diagram_handlers.go:352,457,557`
  - `api/admin_group_handlers.go:215`
  - `api/addon_handlers.go:239`

- **Tests:**
  - `api/collaboration_blocking_test.go` - Validates all collaboration blocking scenarios

- **Spec:**
  - `docs/reference/apis/tmi-openapi.json` - Updated with 409 responses

- **Related Docs:**
  - [CATS Remediation Plan](cats-remediation-plan.md) - Overall CATS work tracking
  - [CATS Phase 1 Completed](cats-phase1-completed.md) - Previous CATS work
