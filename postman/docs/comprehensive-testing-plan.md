# TMI API Comprehensive Testing Plan - Final Version

## Current Test Infrastructure Analysis

### Existing Test Components
- **Main Collections**: 9 JSON collections with inconsistent naming
- **Test Framework**: Newman/Postman with OAuth stub integration
- **Coverage**: Claims 100% endpoint coverage, needs verification

### Current Test Collections (Need Standardization)
1. `comprehensive-test-collection.json` ✅ (already standardized)
2. `unauthorized-tests-collection.json` ✅ (already standardized) 
3. `threat-crud-tests.json` → `threat-crud-tests-collection.json`
4. `metadata-tests.json` → `metadata-tests-collection.json`
5. `permission-matrix-tests.json` → `permission-matrix-tests-collection.json`
6. `bulk-operations-tests.json` → `bulk-operations-tests-collection.json`
7. `collaboration-tests.json` → `collaboration-tests-collection.json`
8. `tmi-postman-collection.json` → `legacy/tmi-postman-collection.json` (67K lines)

## Implementation Plan

### Phase 1: File Organization & Standardization
1. **Create directory structure**:
   - `postman/docs/` for documentation files
   - `postman/legacy/` for archived files
2. **Move documentation files**:
   - `api-workflows.json` → `docs/api-workflows.json`
   - `endpoints-statuscodes.md` → `docs/endpoints-statuscodes.md`
   - `README-comprehensive-testing.md` → `docs/README-comprehensive-testing.md`
3. **Move legacy collection**: `tmi-postman-collection.json` → `legacy/`
4. **Delete backup file**: `tmi-postman-collection.json.backup`
5. **Rename collections** to include "collection" suffix consistently
6. **Update run-tests.sh** to reference new collection names
7. **Update documentation references** to new file locations

### Phase 2: Authentication Token Standardization
1. **Audit all collections** for generic `{{access_token}}` usage
2. **Replace with specific tokens**:
   - `{{access_token}}` → `{{token_alice}}` (default user)
   - Add `{{token_bob}}`, `{{token_charlie}}`, `{{token_diana}}` as needed
3. **Update collection-level auth** configurations
4. **Standardize token variable names** in run-tests.sh script
5. **Keep 401 tests isolated** in unauthorized-tests-collection only

### Phase 3: Test Data Factory Standardization
1. **Create unified TMITestDataFactory** in separate JS file
2. **Standardize factory usage** across all collections
3. **Remove duplicate factory code** from individual collections
4. **Add factory methods** for all entity types (threats, diagrams, documents, sources)
5. **Include validation data generation** (invalid/boundary cases)

### Phase 4: Status Code & Workflow Coverage Verification
1. **Cross-reference docs/endpoints-statuscodes.md** (70+ endpoints) with existing tests
2. **Validate docs/api-workflows.json** (91 workflow methods) coverage
3. **Create coverage matrix**: endpoints × status codes × test collections
4. **Identify and document gaps** in current test coverage
5. **Add missing test cases** systematically

### Phase 5: Enhanced Test Organization
1. **Consolidate 401 tests** into unauthorized-tests-collection only
2. **Remove auth token clearing** from other collections
3. **Standardize test naming conventions** across collections
4. **Add missing WebSocket collaboration tests**
5. **Enhance error scenario coverage** (409 conflicts, 422 validation errors)

## File Structure After Standardization
```
postman/
├── comprehensive-test-collection.json (main suite)
├── unauthorized-tests-collection.json (401 only)
├── threat-crud-tests-collection.json
├── metadata-tests-collection.json
├── permission-matrix-tests-collection.json
├── bulk-operations-tests-collection.json
├── collaboration-tests-collection.json
├── test-data-factory.js (unified factory)
├── multi-user-auth.js (auth helpers)
├── run-tests.sh (updated paths)
├── docs/
│   ├── api-workflows.json
│   ├── endpoints-statuscodes.md
│   ├── README-comprehensive-testing.md
│   └── comprehensive-testing-plan.md (this document)
├── legacy/
│   └── tmi-postman-collection.json (67K lines archive)
└── test-results/ (existing results directory)
```

## Files to Delete
- `tmi-postman-collection.json.backup` (cleanup old backup)

## Deliverables
1. **Clean directory structure** with docs/ and legacy/ separation
2. **Standardized file names** with consistent "collection" suffix
3. **Unified test data factory** used across all collections
4. **Specific token names** (alice/bob/charlie/diana) replacing generic tokens
5. **401 test consolidation** in single unauthorized collection
6. **Complete coverage verification** with gap analysis
7. **Updated documentation** reflecting new organization
8. **Legacy collection archived** and backup files cleaned up
9. **Updated run-tests.sh** with correct file paths

## Execution Status
This plan ensures consistent organization, standardized tooling, verified complete test coverage, and a clean file structure with proper documentation organization.

## Next Steps
After executing this plan:
1. Verify all tests pass with new structure
2. Create coverage matrix to identify gaps
3. Add missing test scenarios
4. Update project documentation to reflect new organization