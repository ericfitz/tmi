# TMI API Comprehensive Test Plan

## Overview

This document provides a complete analysis of the current API test coverage and identifies gaps that need to be filled to achieve 100% endpoint and status code coverage for the TMI API.

## Current Test Coverage Analysis

### ✅ **Well-Covered Areas**

#### **Discovery & OAuth Endpoints**
- **Current Coverage**: `comprehensive-test-collection.json` covers basic discovery endpoints
- **Status Codes Tested**: 200, 500
- **Missing**: 400 error cases, complete OAuth flow testing

#### **Threat Models CRUD**  
- **Current Coverage**: Excellent coverage in `comprehensive-test-collection.json`
- **Status Codes Tested**: 200, 201, 204, 400, 403, 404, 500
- **Workflows**: Complete CRUD lifecycle with multi-user permissions

#### **Threat CRUD**
- **Current Coverage**: Complete coverage in `threat-crud-tests-collection.json`
- **Status Codes Tested**: 200, 201, 204, 400, 404, 500  
- **Workflows**: Full lifecycle with validation testing

#### **Bulk Operations**
- **Current Coverage**: Comprehensive in `bulk-operations-tests-collection.json`
- **Status Codes Tested**: 200, 201, 400
- **Workflows**: Performance and atomicity testing

#### **Collaboration**
- **Current Coverage**: Complete in `collaboration-tests-collection.json`
- **Status Codes Tested**: 200, 201, 204, 403, 404, 409, 500
- **Workflows**: Full session management lifecycle

#### **Authorization Testing**
- **Current Coverage**: Excellent in `permission-matrix-tests-collection.json` and `unauthorized-tests-collection.json`
- **Status Codes Tested**: 200, 201, 204, 401, 403
- **Workflows**: Multi-user RBAC scenarios

---

## 🔍 **Major Gaps Requiring New Test Cases**

### **1. Complete OAuth Flow Testing**
**Missing Collections**: OAuth flow end-to-end testing
**Endpoints Missing**:
- `GET /oauth2/authorize` - **Missing 302, 400, 500**
- `GET /oauth2/callback` - **Missing 200, 302, 400, 401, 500**
- `POST /oauth2/token` - **Missing all status codes** 
- `POST /oauth2/refresh` - **Missing all status codes**
- `POST /oauth2/introspect` - **Missing all status codes**
- `GET /oauth2/userinfo` - **Missing all status codes**
- `POST /oauth2/revoke` - **Missing all status codes**

**Required Workflows**:
```
1. Authorization Code Flow (complete)
2. Token exchange testing  
3. Token refresh lifecycle
4. Token introspection scenarios
5. User info retrieval
6. Token revocation testing
```

### **2. Documents CRUD Testing**
**Missing Collections**: Complete documents testing
**Endpoints Missing**: ALL document endpoints
- `GET /threat_models/{id}/documents` - **Missing all status codes**
- `POST /threat_models/{id}/documents` - **Missing all status codes**
- `GET /threat_models/{id}/documents/{doc_id}` - **Missing all status codes**
- `PUT /threat_models/{id}/documents/{doc_id}` - **Missing all status codes**  
- `DELETE /threat_models/{id}/documents/{doc_id}` - **Missing all status codes**
- `POST /threat_models/{id}/documents/bulk` - **Missing all status codes**

**Required Workflows**:
```
1. Document CRUD lifecycle
2. Document validation (title, description, content)
3. Bulk document operations
4. Multi-user document permissions
5. Parent threat model validation
```

### **3. Sources CRUD Testing** 
**Missing Collections**: Complete sources testing
**Endpoints Missing**: ALL source endpoints except bulk create
- `GET /threat_models/{id}/sources` - **Missing all status codes**
- `POST /threat_models/{id}/sources` - **Missing all status codes** 
- `GET /threat_models/{id}/sources/{source_id}` - **Missing all status codes**
- `PUT /threat_models/{id}/sources/{source_id}` - **Missing all status codes**
- `DELETE /threat_models/{id}/sources/{source_id}` - **Missing all status codes**

**Required Workflows**:
```  
1. Source CRUD lifecycle
2. Source validation (name, type, description)
3. Multi-user source permissions
4. Parent threat model validation
```

### **4. Complete Metadata Testing for All Entity Types**
**Current**: Only threat model metadata partially covered
**Missing Entities**: 
- **Threat metadata** - ALL endpoints missing
- **Diagram metadata** - ALL endpoints missing  
- **Document metadata** - ALL endpoints missing
- **Source metadata** - ALL endpoints missing

**Missing Endpoints Per Entity** (×4 entities):
```
GET    /{entity}/{id}/metadata
POST   /{entity}/{id}/metadata  
GET    /{entity}/{id}/metadata/{key}
PUT    /{entity}/{id}/metadata/{key}
DELETE /{entity}/{id}/metadata/{key}
POST   /{entity}/{id}/metadata/bulk
```

### **5. Diagram CRUD and Collaboration Integration**
**Missing Collections**: Complete diagram testing
**Endpoints Missing**: ALL diagram endpoints except collaboration
- `GET /threat_models/{id}/diagrams` - **Missing all status codes**
- `POST /threat_models/{id}/diagrams` - **Missing all status codes**
- `GET /threat_models/{id}/diagrams/{diagram_id}` - **Missing all status codes** 
- `PUT /threat_models/{id}/diagrams/{diagram_id}` - **Missing all status codes**
- `PATCH /threat_models/{id}/diagrams/{diagram_id}` - **Missing 422 status code**
- `DELETE /threat_models/{id}/diagrams/{diagram_id}` - **Missing all status codes**

**Required Workflows**:
```
1. Diagram CRUD lifecycle
2. Diagram-collaboration integration testing
3. Collaboration session cleanup on diagram deletion
4. Multi-user diagram permissions  
5. JSON Patch validation (422 errors)
```

### **6. Advanced Error Scenarios**
**Missing Status Codes Across Endpoints**:
- **422 Unprocessable Entity**: Only tested on diagram PATCH
- **409 Conflict**: Only tested in collaboration  
- **500 Server Errors**: Limited edge case testing

**Required Scenarios**:
```
1. 422 errors for invalid JSON Patch operations
2. 409 conflicts for duplicate resource creation
3. 500 errors for server-side failures (mocked)
4. Network timeout scenarios
5. Malformed JSON requests
6. Invalid UUID formats
```

### **7. Discovery Endpoints Comprehensive Testing**
**Missing Status Codes**:
- `GET /.well-known/oauth-protected-resource` - **Missing all status codes**
- All discovery endpoints missing **400** and **500** error testing

---

## 📋 **Required New Test Collections**

### **Collection 1: OAuth Complete Flow Tests**
**File**: `oauth-complete-flow-collection.json`
**Priority**: High
**Endpoints**: 7 OAuth endpoints with full status code coverage
**Scenarios**:
- Authorization code exchange
- Token lifecycle management  
- Error handling for invalid tokens
- Token introspection and revocation

### **Collection 2: Documents CRUD Tests**
**File**: `document-crud-tests-collection.json`  
**Priority**: High
**Endpoints**: 6 document endpoints
**Scenarios**:
- Document validation testing
- Multi-user permissions  
- Bulk operations
- Parent threat model validation

### **Collection 3: Sources CRUD Tests**
**File**: `source-crud-tests-collection.json`
**Priority**: High 
**Endpoints**: 5 source endpoints
**Scenarios**:
- Source validation testing
- Multi-user permissions
- Parent threat model validation

### **Collection 4: Diagrams CRUD Tests** 
**File**: `diagram-crud-tests-collection.json`
**Priority**: Medium
**Endpoints**: 6 diagram endpoints  
**Scenarios**:
- Diagram validation testing
- Collaboration integration
- Multi-user permissions
- JSON Patch validation (422 errors)

### **Collection 5: Complete Metadata Tests**
**File**: `complete-metadata-tests-collection.json`
**Priority**: Medium
**Endpoints**: 24 metadata endpoints (6 per entity type)
**Scenarios**:
- Metadata validation across all entity types
- Key-value operations
- Bulk metadata operations
- Cross-entity metadata testing

### **Collection 6: Advanced Error Scenarios**
**File**: `error-scenarios-collection.json`
**Priority**: Low
**Endpoints**: All endpoints
**Scenarios**:
- 422 Unprocessable Entity testing
- 409 Conflict scenarios  
- 500 Server error simulation
- Malformed request testing

### **Collection 7: Discovery Complete Tests**  
**File**: `discovery-complete-collection.json`
**Priority**: Low
**Endpoints**: 5 discovery endpoints
**Scenarios**:
- Complete status code coverage
- Error condition testing
- Response format validation

---

## 🎯 **Implementation Priority Matrix**

### **Phase 1 - Critical Gaps (High Priority)**
1. **OAuth Complete Flow Tests** - Essential for authentication coverage
2. **Documents CRUD Tests** - Major endpoint gap
3. **Sources CRUD Tests** - Major endpoint gap

**Expected Timeline**: 2-3 weeks
**Test Count Estimate**: ~150 new tests

### **Phase 2 - Functional Completeness (Medium Priority)**  
4. **Diagrams CRUD Tests** - Integration with collaboration
5. **Complete Metadata Tests** - Entity completeness
6. **Enhanced Collaboration Tests** - Update session testing

**Expected Timeline**: 2-3 weeks  
**Test Count Estimate**: ~200 new tests

### **Phase 3 - Edge Cases & Polish (Low Priority)**
7. **Advanced Error Scenarios** - 422, 409, 500 coverage
8. **Discovery Complete Tests** - Error condition coverage  
9. **Performance & Load Tests** - Scalability validation

**Expected Timeline**: 1-2 weeks
**Test Count Estimate**: ~50 new tests

---

## 🔧 **Implementation Guidelines**

### **Test Data Patterns**
```javascript
// Use existing TMITestDataFactory patterns
const factory = new TMITestDataFactory();

// Document test data
const validDocument = factory.validDocument({
    title: 'Test Document',
    description: 'Test document description',
    content: 'Document content here'
});

// Source test data  
const validSource = factory.validSource({
    name: 'Test Source',
    type: 'web', 
    description: 'Test source description'
});
```

### **Multi-User Authentication Pattern**
```javascript
// Use existing multi-user auth setup
// Pre-authenticate: Alice (owner), Bob (writer), Charlie (reader), Diana (none)
const authHelper = new TMIMultiUserAuth();
authHelper.setActiveUser('alice'); // Switch context
```

### **Validation Testing Pattern**
```javascript
// Test missing required fields
pm.test('Missing title returns 400', function() {
    pm.expect(pm.response.code).to.equal(400);
    pm.expect(pm.response.json().error).to.include('title');
});

// Test invalid enum values  
pm.test('Invalid status returns 400', function() {
    pm.expect(pm.response.code).to.equal(400);
    pm.expect(pm.response.json().error).to.include('status');
});
```

### **Permission Testing Pattern**
```javascript
// Test cross-user access
pm.test('Reader cannot modify resource', function() {
    pm.expect(pm.response.code).to.equal(403);
    const error = pm.response.json();
    pm.expect(error.required_role).to.exist;
    pm.expect(error.current_role).to.equal('reader');
});
```

---

## 📊 **Success Metrics**

### **Coverage Targets**
- **Endpoint Coverage**: 100% (currently ~70%)
- **Status Code Coverage**: 100% (currently ~75%)
- **Workflow Coverage**: 100% (currently ~60%)

### **Quality Targets**
- **Test Success Rate**: >98%  
- **Response Time**: <500ms for individual operations
- **Bulk Performance**: <10s for 20-item operations
- **Error Detail**: Comprehensive error response validation

### **Maintenance Targets**
- **Documentation**: All test cases documented
- **Automation**: Full CI/CD integration
- **Monitoring**: Performance regression detection

---

## 🚀 **Next Steps**

1. **Review & Approval**: Review this plan with stakeholders
2. **Phase 1 Implementation**: Start with OAuth and Documents/Sources CRUD
3. **Integration**: Ensure new collections integrate with run-tests.sh
4. **Documentation**: Update README-comprehensive-testing.md
5. **CI/CD**: Integrate new collections with automated testing
6. **Performance Baseline**: Establish benchmarks for new endpoints

This comprehensive plan will achieve 100% API test coverage while maintaining the high quality and maintainability of the existing test suite.