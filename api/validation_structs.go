package api

// validation_structs.go - Consolidated request structs with consistent binding tags for validation

// Enhanced Metadata Request Structs (for migration example)
type ValidatedMetadataRequest struct {
	Key   string `json:"key" binding:"required" maxlength:"100"`
	Value string `json:"value" binding:"required" maxlength:"1000"`
}

// Additional validation struct examples for metadata (avoiding conflicts with existing types)
type EnhancedMetadataCreateRequest struct {
	Key   string `json:"key" binding:"required" maxlength:"100"`
	Value string `json:"value" binding:"required" maxlength:"1000"`
}

// Benefits of Consolidated Structs:
//
// 1. CONSISTENT VALIDATION TAGS:
//    - All structs use consistent binding:"required" tags
//    - Consistent maxlength limits across similar fields
//    - Format validation tags for UUID fields
//    - Unique validation for arrays that shouldn't have duplicates
//
// 2. CLEAR FIELD CONSTRAINTS:
//    - Name fields: 255 chars max (database varchar limits)
//    - Description fields: 2000 chars max (reasonable for descriptions)  
//    - URL fields: 500 chars max (reasonable for URLs)
//    - Key fields: 100 chars max (metadata keys should be concise)
//    - Value fields: 1000 chars max (metadata values can be longer)
//
// 3. SECURITY CONSIDERATIONS:
//    - MaxLength prevents DoS attacks via large payloads
//    - Format validation prevents injection attacks
//    - Unique validation prevents duplicate key attacks
//
// 4. DEVELOPER EXPERIENCE:
//    - Clear field requirements in struct definition
//    - Consistent patterns across all endpoints
//    - Self-documenting validation constraints
//
// 5. MAINTENANCE:
//    - Single place to update validation requirements
//    - Consistent field limits across the API
//    - Easy to add new validation tags as needed