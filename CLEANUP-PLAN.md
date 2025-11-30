# TMI Codebase Cleanup - Remaining Work Plan

**Status:** ✅ **Major Cleanup Phase Complete**
**Updated:** 2025-11-30
**Related:** See [CODE-REVIEW-REPORT.md](CODE-REVIEW-REPORT.md) for full analysis

---

## Executive Summary

**Progress:** 52% of original technical debt resolved (42 of 81 items)

### Completed in Commits a6a80a8 & cdd711a:
- ✅ Removed all deprecated functions and fields
- ✅ Deleted ~1,200 lines of unused code
- ✅ Implemented 6 incomplete features
- ✅ Cleaned up obsolete tests
- ✅ All HIGH priority items resolved

### Remaining Work:
- 🔧 1 MEDIUM priority item (production readiness)
- 🚀 3 LOW priority items (v1.0 cleanup)
- 📋 25+ TODOs (defer/address opportunistically)

---

## Phase 1: Production Readiness (MEDIUM Priority)

### Task: SAML Signature Validation
**Priority:** MEDIUM
**Effort:** 2-3 days
**Timeline:** Next sprint

**Location:** [auth/saml/provider.go:165,222](auth/saml/provider.go#L165)

**Requirements:**
1. Implement proper SAML response signature validation
2. Implement SAML logout request processing
3. Add comprehensive tests for signature validation
4. Verify against SAML 2.0 specification compliance

**Why:** Required for production SAML deployments with enterprise identity providers

**Acceptance Criteria:**
- [ ] SAML response signatures validated according to SAML 2.0 spec
- [ ] SAML logout requests properly handled
- [ ] Test coverage for signature validation edge cases
- [ ] Documentation updated with SAML security guarantees

---

## Phase 2: v1.0 API Cleanup (LOW Priority)

### Task 1: Remove Deprecated Diagram Schema
**Priority:** LOW
**Effort:** 1 day
**Timeline:** Before v1.0 release

**Location:** [api/api.go:916](api/api.go#L916), OpenAPI specification

**Requirements:**
1. Remove empty `Diagram` wrapper type from api/api.go
2. Update OpenAPI spec to use `DfdDiagram` directly
3. Regenerate API code with `make generate-api`
4. Test client compatibility
5. Update client integration documentation

**Why:** Breaking change - eliminates empty wrapper that confuses client generators

**Acceptance Criteria:**
- [ ] Diagram type removed from Go code
- [ ] OpenAPI spec uses DfdDiagram directly
- [ ] API code regenerated successfully
- [ ] No build or test failures
- [ ] Client integration guide updated

---

### Task 2: Create Migration Documentation
**Priority:** LOW
**Effort:** 1 day
**Timeline:** Before v1.0 release

**Requirements:**
1. Document all breaking changes from commits a6a80a8, cdd711a
2. Create migration guide for deprecated function removals
3. Update CHANGELOG with comprehensive change list
4. Document replacement patterns and upgrade path

**Why:** Help users migrate from v0.x to v1.0

**Deliverables:**
- [ ] docs/migration/v0-to-v1.md created
- [ ] CHANGELOG.md updated with breaking changes section
- [ ] Migration guide includes code examples
- [ ] All removed functions documented with replacements

---

## Phase 3: Low Priority TODOs (DEFER)

### Task: Address Remaining TODOs
**Priority:** DEFER
**Effort:** Ongoing
**Timeline:** As needed

**Categories:**
1. Provider context in test/utility code (5 TODOs)
2. WebSocket identity context improvements (2 TODOs)
3. Configuration externalization (1 TODO)
4. Code readability refactoring (3 TODOs)
5. Schema validation enhancements (2 TODOs)
6. Test infrastructure improvements (1 TODO)
7. Miscellaneous improvements (11+ TODOs)

**Strategy:**
- Create GitHub issues for tracking each category
- Address opportunistically during related work
- Prioritize based on user impact and developer pain points
- Review quarterly for items that have become more urgent

**Recommendation:**
Don't create dedicated sprints for these items. Instead, knock them off when working in nearby code.

---

## Success Metrics

### Completed (a6a80a8, cdd711a):
- ✅ 3 deprecated functions removed
- ✅ 3 deprecated fields removed
- ✅ 1 commented-out code block removed (39 lines)
- ✅ 4 obsolete test files deleted
- ✅ 24 unused metric functions removed
- ✅ 6 features implemented
- ✅ ~1,221 net lines removed

### Remaining Targets:
- 🔧 1 SAML validation implementation
- 🚀 1 deprecated schema removal
- 📖 1 migration guide creation
- 📋 25+ TODOs (no specific target - address as needed)

---

## Risk Assessment

| Task | Risk | Mitigation |
|------|------|------------|
| SAML Signature Validation | MEDIUM - Security-sensitive code | Thorough testing, spec compliance review |
| Diagram Schema Removal | LOW - Breaking change | Wait for v1.0, comprehensive testing |
| Migration Documentation | NONE | Pure documentation |
| Low Priority TODOs | NONE | Optional improvements |

---

## Recommendations

### Immediate Action (This Week):
**Nothing urgent** - All HIGH priority items are complete. Code is production-ready for current features.

### Next Sprint:
**SAML Signature Validation** - Only if production SAML deployments are planned. Otherwise, defer.

### Before v1.0:
1. Complete SAML validation (if not done earlier)
2. Remove Diagram schema
3. Create migration documentation

### Ongoing:
- Review TODO list quarterly
- Address low-priority items opportunistically
- Keep technical debt minimal through code review discipline

---

## Questions for Product Owner

1. **SAML Production Timeline:** When do we need production-grade SAML support?
   - If soon: Prioritize SAML validation next sprint
   - If later: Can defer to future sprint

2. **v1.0 Timeline:** When is v1.0 release planned?
   - Determines urgency of breaking changes (Diagram schema removal)

3. **Migration Documentation:** Who is the target audience?
   - Internal teams only: Minimal documentation OK
   - External users: Need comprehensive migration guide

---

**End of Plan**
