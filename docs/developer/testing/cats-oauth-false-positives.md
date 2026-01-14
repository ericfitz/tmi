# CATS OAuth/Auth False Positives

## Overview

CATS fuzzer may flag legitimate OAuth 2.0 and authentication responses as "errors" due to keywords like "Unauthorized", "Forbidden", "InvalidToken", etc. These are **not security vulnerabilities** - they are correct, RFC-compliant protocol responses.

## What Are OAuth False Positives?

OAuth false positives occur when:
- Response code is 401 (Unauthorized) or 403 (Forbidden)
- Response contains OAuth/auth error keywords
- These responses are **intentional and correct** per RFCs 6749, 8414, 7517, etc.

### Examples of Legitimate OAuth Responses

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "invalid_token",
  "error_description": "The access token is invalid or expired"
}
```

```http
HTTP/1.1 403 Forbidden
Content-Type: application/json

{
  "error": "insufficient_scope",
  "error_description": "The request requires higher privileges"
}
```

These are **expected, correct responses** when:
- Token is missing, expired, or invalid
- User lacks required permissions
- OAuth flow is incomplete

## Detection Criteria

The `parse-cats-results.py` script automatically identifies OAuth false positives using these criteria:

1. **Response Code**: 401 or 403
2. **Keywords Present**: One or more of:
   - `unauthorized`, `forbidden`
   - `invalidtoken`, `invalid_token`
   - `invalidgrant`, `invalid_grant`
   - `authenticationfailed`, `authenticationerror`
   - `authorizationerror`, `access_denied`

## Using Filtered Results

### Parse CATS Reports with False Positive Detection

```bash
# Parse CATS reports into database
uv run scripts/parse-cats-results.py \
  --input test/outputs/cats/report/ \
  --output test/outputs/cats/cats-results.db \
  --create-schema
```

The parser will:
- Mark OAuth false positives with `is_oauth_false_positive = 1`
- Provide statistics for both raw and filtered results
- Create views for easy analysis

### Query Actual Errors (Excluding False Positives)

```bash
# Use the query helper script
./scripts/query-cats-results.sh test/outputs/cats/cats-results.db
```

Or query directly:

```sql
-- All actual errors (excluding OAuth false positives)
SELECT * FROM test_results_filtered_view
WHERE result = 'error';

-- Count errors by path
SELECT path, COUNT(*) as error_count
FROM test_results_filtered_view
WHERE result = 'error'
GROUP BY path
ORDER BY error_count DESC;

-- View OAuth false positives separately
SELECT * FROM test_results_view
WHERE is_oauth_false_positive = 1;
```

### Database Views

The database provides two main views:

1. **`test_results_view`** - All tests, includes `is_oauth_false_positive` flag
2. **`test_results_filtered_view`** - Excludes OAuth false positives (recommended for analysis)

## Statistics Output

When parsing completes, you'll see statistics like:

```
Result distribution:
  error: 7449
  warn: 1234
  success: 5678

OAuth/Auth false positives (expected 401/403): 3215

Result distribution (excluding OAuth false positives):
  error: 4234
  warn: 1234
  success: 5678
```

## Why Not Remove Keywords from CATS?

We **previously tried** creating a custom error keywords file that excluded OAuth terms. This caused problems:

- CATS flagged ALL tests as errors when the keyword list was too minimal
- CATS apparently requires a comprehensive keyword list to function properly
- Custom keyword files broke CATS's internal validation

The **better approach** is:
1. Let CATS use its default error leak detection (comprehensive, well-tested)
2. Use `parse-cats-results.py` to filter OAuth false positives during analysis
3. Focus on the **actual errors** shown in filtered results

## Related Documentation

- [CATS Public Endpoints](cats-public-endpoints.md) - Why some endpoints intentionally lack authentication
- RFC 6749 - OAuth 2.0 Authorization Framework (defines error codes)
- RFC 8414 - OAuth 2.0 Authorization Server Metadata
- RFC 7517 - JSON Web Key (JWK)

## Quick Reference

| Scenario | Is False Positive? | Reason |
|----------|-------------------|---------|
| 401 with "invalid_token" | ✅ Yes | Correct OAuth error response |
| 401 with "unauthorized" | ✅ Yes | Standard HTTP auth response |
| 403 with "forbidden" | ✅ Yes | Correct permission denied |
| 409 on POST /admin/groups | ✅ Yes | Duplicate name from fuzzed values |
| 400 with text/plain content-type | ✅ Yes | Go HTTP layer transport error |
| 400 from header fuzzers | ✅ Yes | Correct header validation |
| 500 with "NullPointerException" | ❌ No | Actual server error |
| 400 with "invalid_request" | ❌ No | Input validation error |
| 200 with "unauthorized" in body | ⚠️ Maybe | Needs manual review |

## Additional False Positive Categories

### 409 Conflict on POST /admin/groups

When CATS fuzzers modify field values (zero-width characters, Hangul fillers, etc.), the modified group name may still collide with existing groups created during test data setup. The API correctly returns 409 Conflict for duplicate names. This is expected behavior, not a security issue.

**Example**: `ZeroWidthCharsInValuesFields` inserts invisible characters into the group name field. The API strips or normalizes these, resulting in the same effective name as an existing group → 409 Conflict.

### Non-JSON Content Types from Go HTTP Layer

When the `InvalidContentLengthHeaders` fuzzer sends requests with mismatched `Content-Length` headers (e.g., `Content-Length: 1` with a larger body), Go's `net/http` package rejects the request at the **transport layer** before it reaches Gin middleware.

Go returns:
- Status: `400 Bad Request`
- Content-Type: `text/plain; charset=utf-8`
- Body: `400 Bad Request`

This is standard HTTP behavior at the transport layer. Our `JSONErrorHandler` middleware cannot intercept it because the request is rejected before routing occurs. This is a known limitation, not a security issue.

### Header Validation Responses (400 Bad Request)

Several CATS fuzzers send malformed or unusual HTTP headers and expect the request to succeed. When TMI returns `400 Bad Request` for invalid headers, this is **correct input validation behavior**, not a security issue.

Fuzzers in this category:
- `AcceptLanguageHeaders` - Malformed Accept-Language header values
- `UnsupportedContentTypesHeaders` - Invalid Content-Type values
- `DummyContentLengthHeaders` - Invalid Content-Length values
- `LargeNumberOfRandomAlphanumericHeaders` - Header flooding
- `DuplicateHeaders` - Duplicate header injection
- `ExtraHeaders` - Unknown headers added

Returning 400 for these requests demonstrates proper header validation.

## Troubleshooting

### All tests showing as errors

- Check you're using CATS default error keywords (not custom file)
- Verify `parse-cats-results.py` ran successfully
- Use `test_results_filtered_view` for analysis

### Too many false positives detected

- Review detection criteria in `is_oauth_auth_false_positive()`
- Adjust keywords list if needed for your API
- File issue if legitimate errors are being filtered

### Missing OAuth false positive filtering

- Ensure database was created with `--create-schema` flag
- Re-parse with latest version of `parse-cats-results.py`
- Check `is_oauth_false_positive` column exists in tests table
