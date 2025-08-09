# 🚀 **Direct Migration Plan: Custom Validation → OpenAPI Middleware**

## **Migration Overview**
Direct replacement of TMI's custom validation system with `github.com/oapi-codegen/gin-middleware`. No backwards compatibility needed since TMI hasn't launched yet.

**Branch**: `feature/oapi-validation-middleware`
**Approach**: Clean replacement, preserve business logic validation

---

## **Phase 1: Setup & Foundation**

### **Step 1.1: Create Feature Branch & Add Dependency**
- [ ] Create feature branch `feature/oapi-validation-middleware`
- [ ] Add OpenAPI middleware dependency: `go get github.com/oapi-codegen/gin-middleware`
- [ ] Update go.mod with `go mod tidy`

### **Step 1.2: Create OpenAPI Validation Integration**
- [ ] Create `api/openapi_middleware.go` with error handler
- [ ] Implement `OpenAPIErrorHandler` function to convert OpenAPI errors to TMI format
- [ ] Implement `SetupOpenAPIValidation` function

### **Step 1.3: Integrate Middleware into Server Setup**
- [ ] Modify `cmd/server/main.go` to add OpenAPI validation middleware
- [ ] Ensure middleware is added BEFORE route handlers
- [ ] Test basic server startup

---

## **Phase 2: Enhance OpenAPI Spec**

### **Step 2.1: Add Comprehensive Validation Rules to OpenAPI Spec**
- [ ] Update `ThreatModel` schema with validation rules (minLength, maxLength, pattern)
- [ ] Update `Authorization` schema with email format and enum validation
- [ ] Update `Metadata` schema with pattern validation for key field
- [ ] Update `Document` schema with URI format validation
- [ ] Update `Source` schema with URL validation
- [ ] Update `Threat` schema with enum validation for severity/status
- [ ] Add HTML injection prevention patterns (no <, >, etc.)

### **Step 2.2: Add Binding Tags for Required Fields**
- [ ] Ensure all required fields have `x-oapi-codegen-extra-tags` with `binding:"required"`
- [ ] Verify enum fields have proper validation
- [ ] Add format validation (email, uri, uuid) where appropriate

### **Step 2.3: Regenerate API Structures**
- [ ] Run `make gen-api` to regenerate Go structures
- [ ] Verify generated structs have proper binding tags
- [ ] Test that new validation tags are present

---

## **Phase 3: Update All Handlers**

### **Step 3.1: Update Threat Model Handlers**
- [ ] `CreateThreatModel`: Remove `ValidateAndParseRequest`, replace with `c.ShouldBindJSON`
- [ ] `UpdateThreatModel`: Remove `ValidateAndParseRequest`, replace with `c.ShouldBindJSON`
- [ ] `PatchThreatModel`: Keep existing logic (PATCH operations don't use ValidateAndParseRequest)
- [ ] Test threat model endpoints

### **Step 3.2: Update Metadata Handlers**  
- [ ] `threat_model_metadata_handlers.go`: Update all CRUD operations
- [ ] `document_metadata_handlers.go`: Update all CRUD operations
- [ ] `source_metadata_handlers.go`: Update all CRUD operations
- [ ] `cell_handlers.go`: Update metadata operations
- [ ] Test metadata endpoints

### **Step 3.3: Update Sub-Resource Handlers**
- [ ] `document_sub_resource_handlers.go`: Update CREATE/UPDATE operations
- [ ] `source_sub_resource_handlers.go`: Update CREATE/UPDATE operations  
- [ ] `threat_sub_resource_handlers.go`: Update CREATE/UPDATE operations
- [ ] Test sub-resource endpoints

### **Step 3.4: Update Other Handlers**
- [ ] `threat_model_diagram_handlers.go`: Update diagram operations
- [ ] `batch_handlers.go`: Update batch operations
- [ ] Any other handlers using `ValidateAndParseRequest`

---

## **Phase 4: Preserve Business Logic Validation**

### **Step 4.1: Identify Business Logic to Keep**
- [ ] `ValidateAuthenticatedUser` - Keep (authentication)
- [ ] `ValidateAuthorizationEntriesWithFormat` - Keep (business rules)
- [ ] `ValidateDuplicateSubjects` - Keep (business logic)
- [ ] `ValidateUniqueConstraints` - Keep (database-dependent)
- [ ] Other business-specific validation functions

### **Step 4.2: Move Simple Validation to OpenAPI**
- [ ] Migrate string length validation to `minLength`/`maxLength` in schema
- [ ] Migrate format validation to `pattern`/`format` in schema
- [ ] Migrate enum validation to `enum` in schema
- [ ] Migrate required field validation to `required` array in schema

---

## **Phase 5: Update Tests**

### **Step 5.1: Update Test Expectations**
- [ ] `TestThreatModel*`: Update error message expectations
- [ ] `TestMetadata*`: Update validation test expectations
- [ ] `TestDocument*`: Update validation test expectations
- [ ] `TestSource*`: Update validation test expectations
- [ ] `TestThreat*`: Update validation test expectations

### **Step 5.2: Remove Unnecessary Mock Expectations**
- [ ] Remove mock expectations that were only needed for custom validation
- [ ] Keep mocks for actual business logic operations
- [ ] Update test setup functions

### **Step 5.3: Test Categories**
- [ ] Basic validation tests (required fields, formats)
- [ ] Business logic tests (authorization, duplicates)
- [ ] Integration tests (end-to-end)
- [ ] Error format tests (ensure API contracts unchanged)

---

## **Phase 6: Clean Up**

### **Step 6.1: Remove Unused Validation Code**
- [ ] Remove `api/validation_config.go` (ValidationConfigs map)
- [ ] Remove simple validators from `api/validation_registry.go`
- [ ] Remove `ValidateAndParseRequest` function from `api/validation.go`
- [ ] Keep business logic validators

### **Step 6.2: Update Imports and Dependencies**
- [ ] Remove unused validation framework imports from handler files
- [ ] Clean up any unused dependencies
- [ ] Update documentation/comments

### **Step 6.3: Final Cleanup**
- [ ] Remove unused constants/variables
- [ ] Clean up any remaining references to old validation system
- [ ] Update any remaining comments/documentation

---

## **Phase 7: Testing & Validation**

### **Step 7.1: Comprehensive Testing**
- [ ] Run unit tests: `go test ./api -v`
- [ ] Run integration tests: `make test-integration`  
- [ ] Run linting: `make lint`
- [ ] Run build: `make build`

### **Step 7.2: Manual Testing**
- [ ] Test API endpoints with valid requests (should succeed)
- [ ] Test API endpoints with invalid requests (should fail with proper errors)
- [ ] Test business logic validation still works
- [ ] Verify error response format unchanged

### **Step 7.3: Performance Testing (Optional)**
- [ ] Run benchmarks to ensure no significant performance regression
- [ ] Compare before/after performance if needed

---

## **Final Steps**

### **Code Review & Merge**
- [ ] Self-review all changes
- [ ] Ensure all tests pass
- [ ] Verify error handling works correctly
- [ ] Create pull request for review
- [ ] Address any review feedback
- [ ] Merge to main branch

---

## **Success Criteria**

✅ **All handlers updated** - No more ValidateAndParseRequest calls  
✅ **All tests passing** - Same behavior, cleaner implementation  
✅ **Error format preserved** - API contracts unchanged  
✅ **Business logic intact** - Authentication, authorization work  
✅ **OpenAPI validation active** - Schema violations rejected  
✅ **Code cleaned up** - Unused validation code removed  

---

## **Key Implementation Notes**

**Handler Pattern Transformation:**
```go
// Before (Custom Validation)
request, err := ValidateAndParseRequest[T](c, ValidationConfigs["endpoint"])
if err != nil {
    HandleRequestError(c, err)
    return
}

// After (OpenAPI Middleware)  
var request T
if err := c.ShouldBindJSON(&request); err != nil {
    HandleRequestError(c, ServerError("Failed to bind validated request"))
    return
}
```

**Keep These Functions:**
- `ValidateAuthenticatedUser` (authentication)
- `ValidateAuthorizationEntriesWithFormat` (business rules)
- `ValidateDuplicateSubjects` (business logic)
- `HandleRequestError` (error handling)

**Files to Create:**
- `api/openapi_middleware.go` (new integration code)

**Files to Modify:**
- `cmd/server/main.go` (add middleware)
- `tmi-openapi.json` (enhanced validation rules)
- All handler files (remove ValidateAndParseRequest calls)
- All test files (update expectations)

**Files to Remove/Clean:**
- `api/validation_config.go` (ValidationConfigs map)
- Simple validators from `api/validation_registry.go`
- `ValidateAndParseRequest` from `api/validation.go`

---

**Estimated Timeline**: 10 days  
**Risk Level**: Low (no production system to break)  
**Benefits**: Cleaner code, automatic schema sync, reduced maintenance