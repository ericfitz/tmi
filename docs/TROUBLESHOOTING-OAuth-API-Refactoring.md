# OAuth API Refactoring Troubleshooting Guide

## Problem Summary
When refactoring OAuth2 endpoints from path parameters (`/oauth2/authorize/{provider}`) to query parameters (`/oauth2/authorize?idp=provider`), the endpoints appeared to work but had authentication issues and inconsistent URL generation.

## Diagnosis Process - Step by Step

### Phase 1: Initial Problem Identification
**Symptoms Observed:**
- OAuth endpoints returned 401 "Missing Authorization header" errors
- `/oauth2/providers` endpoint returned old URL format despite code changes
- Middleware logs were not appearing for OAuth requests
- Route conflicts suspected

### Phase 2: Architecture Analysis
**Key Investigation Steps:**

#### 1. **Router Analysis** (`cmd/server/main.go`)
```bash
# Check for multiple router creations
grep -n "gin.New\|gin.Default" cmd/server/main.go
```
- **Finding**: Single router created at line 977: `r := gin.New()`
- **Conclusion**: No multiple router conflicts

#### 2. **Middleware Chain Analysis**
```bash
# Check middleware registration order
grep -A 10 -B 5 "Use(" cmd/server/main.go
```
- **Finding**: Proper middleware chain order established
- **Order**: CORS → Request ID → Logging → Public Paths → JWT → OpenAPI Validation

#### 3. **Route Registration Analysis**
```bash
# Check for duplicate route registrations
grep -r "oauth2/authorize" auth/ api/
```
- **Critical Finding**: Duplicate route registrations in `auth/handlers.go`
- **Problem**: Old auth module routes conflicted with new OpenAPI routes

### Phase 3: Route Conflict Resolution
**Root Cause Identified:**
- Auth module registered routes: `/oauth2/authorize/:provider` and `/oauth2/token/:provider`
- OpenAPI specification registered routes: `/oauth2/authorize?idp=provider` and `/oauth2/token?idp=provider`
- **First-match-wins routing** meant old routes intercepted requests

**Fix Applied:**
```go
// In auth/handlers.go - REMOVED these conflicting registrations:
// auth.GET("/authorize/:provider", h.Authorize)  // REMOVED
// auth.POST("/token/:provider", h.Exchange)      // REMOVED
```

### Phase 4: URL Generation Fix
**Additional Problem Found:**
- `/oauth2/providers` endpoint returned old URL format
- **Location**: `auth/handlers.go:118`
- **Problem**: Hard-coded path construction

**Before:**
```go
authURL := fmt.Sprintf("%s/oauth2/authorize/%s", getBaseURL(c), id)
```

**After:**
```go
authURL := fmt.Sprintf("%s/oauth2/authorize?idp=%s", getBaseURL(c), id)
```

### Phase 5: Server Restart Requirements
**Critical Discovery:**
- Code changes didn't take effect immediately
- **Required**: Complete binary rebuild and server restart
- **Commands Used:**
```bash
pkill -f "bin/server"
rm -f bin/server
make build-server
make clean-all && make dev-start
```

## Complete Checklist for Similar Issues

### 1. **Route Registration Conflicts**
**Files to Check:**
- `auth/handlers.go` - Auth module route registrations
- `api/server.go` - OpenAPI route registrations  
- `cmd/server/main.go` - Main server setup
- Any files with `router.GET`, `router.POST`, etc.

**Commands:**
```bash
# Find all route registrations
grep -r "\.GET\|\.POST\|\.PUT\|\.DELETE" --include="*.go" .

# Check for conflicting OAuth routes specifically
grep -r "oauth2" --include="*.go" . | grep -E "GET\(|POST\("

# Look for path parameter routes that might conflict
grep -r "/:.*," --include="*.go" .
```

### 2. **Hard-coded URL Construction**
**Files to Check:**
- `auth/handlers.go` - Provider URL generation
- Any files with `fmt.Sprintf` and URL paths
- Test files that might have hard-coded URLs
- Documentation files with example URLs

**Commands:**
```bash
# Find all hard-coded OAuth URL constructions
grep -r "oauth2/authorize" --include="*.go" .
grep -r "oauth2/token" --include="*.go" .

# Find sprintf statements with URL patterns
grep -r "fmt.Sprintf.*http" --include="*.go" .
grep -r "fmt.Sprintf.*/" --include="*.go" .
```

### 3. **OpenAPI Specification Consistency**
**Files to Check:**
- `shared/api-specs/tmi-openapi.json` - API specification
- Generated API code after running `make generate-api`

**Verification:**
```bash
# Check OpenAPI spec for correct parameter definitions
jq '.paths."/oauth2/authorize".get.parameters' shared/api-specs/tmi-openapi.json

# Verify API regeneration
make generate-api
git diff api/api.go
```

### 4. **Middleware Chain Issues**
**Files to Check:**
- `cmd/server/main.go` - Middleware registration order
- `api/middleware.go` - Custom middleware implementations
- Public paths configuration

**Debug Commands:**
```bash
# Add diagnostic logging to trace middleware execution
# Check middleware order in main.go around line 980-1000

# Test middleware execution with verbose logging
curl -v http://localhost:8080/oauth2/providers 2>&1 | grep -E "HTTP|X-Request-Id"
```

### 5. **Server Binary and Caching Issues**
**Always Required After Changes:**
```bash
# Complete cleanup and restart sequence
make clean-all
rm -f bin/server
make build-server  
make dev-start

# Verify process replacement
ps aux | grep "bin/server"
```

## Testing Strategy

### 1. **Endpoint Verification**
```bash
# Test new query parameter format works
curl -s -i "http://localhost:8080/oauth2/authorize?idp=test" | head -5

# Verify old path format is rejected
curl -s -i "http://localhost:8080/oauth2/authorize/test" | head -5

# Check providers endpoint returns correct URLs
curl -s http://localhost:8080/oauth2/providers | jq '.providers[].auth_url'
```

### 2. **End-to-End OAuth Flow**
```bash
# Test complete flow
TEST_URL=$(curl -s "http://localhost:8080/oauth2/authorize?idp=test&login_hint=alice" | grep -o 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g')
echo "Generated callback URL: $TEST_URL"
```

### 3. **Middleware Execution Verification**
```bash
# Check request ID generation and logging
curl -v http://localhost:8080/oauth2/providers 2>&1 | grep "X-Request-Id"

# Monitor logs for middleware execution (if debug logging enabled)
tail -f /path/to/server/logs
```

## Prevention Strategies

### 1. **Code Review Checklist**
- [ ] Check for route registration conflicts when adding new endpoints
- [ ] Verify URL construction uses constants or configuration, not hard-coded strings
- [ ] Ensure OpenAPI specification matches handler implementations
- [ ] Test both new and deprecated endpoint formats during transitions

### 2. **Automated Testing**
- Add integration tests that verify endpoint URL generation
- Include tests for deprecated endpoint rejection
- Test complete OAuth flow in CI/CD pipeline

### 3. **Documentation Updates**
- Update API documentation when changing endpoint formats
- Document migration path for clients using old endpoints
- Include troubleshooting steps in README

## Key Lessons Learned

1. **Route Order Matters**: First-registered routes win in Gin router
2. **Server Restart Required**: Go binary changes require complete process restart
3. **Multiple Code Locations**: URL changes must be updated in multiple places:
   - OpenAPI specification
   - Handler parameter extraction
   - URL generation in responses
   - Test files and documentation
4. **Middleware Chain Dependencies**: Route conflicts can cause requests to bypass middleware entirely
5. **Binary Compilation**: Changes in Go code require rebuild and restart, unlike interpreted languages

## Emergency Recovery

If similar issues occur in production:

1. **Immediate Assessment:**
```bash
# Check which routes are actually registered
curl -s http://localhost:8080/debug/routes 2>/dev/null || echo "Debug routes not available"

# Test both old and new endpoint formats
curl -I http://localhost:8080/oauth2/authorize/test
curl -I "http://localhost:8080/oauth2/authorize?idp=test"
```

2. **Quick Rollback Strategy:**
   - Keep previous binary version available
   - Document exact working configuration
   - Have rollback procedure tested and ready

3. **Gradual Migration:**
   - Support both old and new formats temporarily
   - Use feature flags or configuration to control routing
   - Monitor usage patterns before removing old endpoints