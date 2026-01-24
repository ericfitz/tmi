# CATS False Positives

## Overview

CATS fuzzer may flag legitimate API responses as "errors" due to expected behavior patterns. These are **not security vulnerabilities** - they are correct, RFC-compliant responses or expected API behavior.

## What Are CATS False Positives?

CATS false positives occur when the fuzzer flags a response as an error, but the API is actually behaving correctly. The `parse-cats-results.py` script automatically detects and marks these false positives using the `is_false_positive()` function.

### False Positive Categories

The detection logic in `scripts/parse-cats-results.py` handles 16+ categories of false positives:

| Category | Response Codes | Description |
|----------|---------------|-------------|
| OAuth/Auth | 401, 403 | Expected authentication failures during fuzzing |
| Rate Limiting | 429 | Infrastructure protection, not API behavior |
| Validation | 400 | API correctly rejects malformed input |
| Not Found | 404 | Expected when fuzzing with random/invalid resource IDs |
| IDOR | 200 | Filter parameters and admin endpoints behave correctly |
| HTTP Methods | 400, 405 | Correct rejection of unsupported methods |
| Response Contract | Various | Header mismatches are spec issues, not security issues |
| Conflict | 409 | Duplicate name conflicts from fuzzed values |
| Content Type | 400 | Go HTTP layer transport errors (text/plain) |
| Injection | Various | JSON API data reflection is not XSS |
| Header Validation | 400 | Correct rejection of malformed headers |
| Leading Zeros | 400 | Correct rejection of invalid JSON number format |
| OneOf Validation | 400 | Incomplete oneOf request bodies |
| Connection Errors | 999 | Network/CATS issues, not API bugs |
| String Boundaries | 200, 201 | Empty optional fields are valid |
| Transfer Encoding | 501 | Correct rejection per RFC 7230 |

### Examples of Legitimate Responses

**OAuth 401 Response:**
```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "invalid_token",
  "error_description": "The access token is invalid or expired"
}
```

**OAuth 403 Response:**
```http
HTTP/1.1 403 Forbidden
Content-Type: application/json

{
  "error": "insufficient_scope",
  "error_description": "The request requires higher privileges"
}
```

These are **expected, correct responses** per RFC 6749 when:
- Token is missing, expired, or invalid
- User lacks required permissions
- OAuth flow is incomplete

## Detection Logic

The `scripts/parse-cats-results.py` script uses the `is_false_positive()` function (with backward-compatible alias `is_oauth_auth_false_positive()`) to automatically identify false positives based on:

1. **Response Code Analysis**: Specific codes (401, 403, 404, 429, etc.) trigger category-specific logic
2. **Fuzzer Identification**: Certain fuzzers produce expected failures (validation fuzzers, boundary testers)
3. **Keyword Matching**: Auth-related keywords in response reason/details
4. **Path Analysis**: Admin endpoints and list endpoints have different authorization models
5. **Request Context**: HTTP method and scenario information

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
- Mark false positives with `is_oauth_false_positive = 1`
- Provide statistics for both raw and filtered results
- Create views for easy analysis

### Query Actual Errors (Excluding False Positives)

```bash
# Use the query helper script
./scripts/query-cats-results.sh test/outputs/cats/cats-results.db
```

Or query directly:

```sql
-- All actual errors (excluding false positives)
SELECT * FROM test_results_filtered_view
WHERE result = 'error';

-- Count errors by path
SELECT path, COUNT(*) as error_count
FROM test_results_filtered_view
WHERE result = 'error'
GROUP BY path
ORDER BY error_count DESC;

-- View false positives separately
SELECT * FROM test_results_view
WHERE is_oauth_false_positive = 1;
```

### Database Views

The database provides two main views:

1. **`test_results_view`** - All tests, includes `is_oauth_false_positive` flag
2. **`test_results_filtered_view`** - Excludes false positives (recommended for analysis)

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

## Why Filter In Post-Processing?

We **previously tried** creating custom error keywords files that excluded OAuth terms. This caused problems:

- CATS flagged ALL tests as errors when the keyword list was too minimal
- CATS apparently requires a comprehensive keyword list to function properly
- Custom keyword files broke CATS's internal validation

The **better approach** is:
1. Let CATS use its default error leak detection (comprehensive, well-tested)
2. Use `parse-cats-results.py` to filter false positives during analysis
3. Focus on the **actual errors** shown in filtered results

## Related Documentation

- [CATS Public Endpoints](cats-public-endpoints.md) - Why some endpoints intentionally lack authentication
- RFC 6749 - OAuth 2.0 Authorization Framework (defines error codes)
- RFC 8414 - OAuth 2.0 Authorization Server Metadata
- RFC 7517 - JSON Web Key (JWK)

## Quick Reference

| Scenario | Is False Positive? | Reason |
|----------|-------------------|---------|
| 401 with "invalid_token" | Yes | Correct OAuth error response |
| 401 with "unauthorized" | Yes | Standard HTTP auth response |
| 403 with "forbidden" | Yes | Correct permission denied |
| 409 on POST /admin/groups | Yes | Duplicate name from fuzzed values |
| 400 with text/plain content-type | Yes | Go HTTP layer transport error |
| 400 from header fuzzers | Yes | Correct header validation |
| 429 rate limit | Yes | Infrastructure protection |
| 404 from boundary fuzzers | Yes | Expected with invalid IDs |
| 500 with "NullPointerException" | No | Actual server error |
| 400 with "invalid_request" | Depends | May be correct validation |
| 200 with "unauthorized" in body | Maybe | Needs manual review |

## Additional False Positive Categories

### 409 Conflict on POST /admin/groups

When CATS fuzzers modify field values (zero-width characters, Hangul fillers, etc.), the modified group name may still collide with existing groups created during test data setup. The API correctly returns 409 Conflict for duplicate names. This is expected behavior, not a security issue.

**Example**: `ZeroWidthCharsInValuesFields` inserts invisible characters into the group name field. The API strips or normalizes these, resulting in the same effective name as an existing group leading to 409 Conflict.

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

### Security Hardening for Unicode Attacks

TMI has explicit security middleware that rejects dangerous Unicode characters:
- Zero-width characters (invisible chars in filenames/URLs)
- Bidirectional overrides (can make malicious text appear safe)
- Hangul filler characters
- Combining diacritical marks (Zalgo text - DoS via rendering)
- Control characters

When validation fuzzers inject these characters and receive 400 responses, this is **correct security behavior**.

### Injection Testing on JSON APIs

TMI is a JSON API, not an HTML-rendering web application. When injection fuzzers report:
- "Payload reflected in response" - Data is stored and returned as JSON (not XSS)
- "NoSQL injection potential" - TMI uses PostgreSQL, not MongoDB
- XSS on query parameters - GET requests return JSON, not HTML

These are false positives because the API doesn't execute payloads - it stores them as string data.

## Troubleshooting

### All tests showing as errors

- Check you're using CATS default error keywords (not custom file)
- Verify `parse-cats-results.py` ran successfully
- Use `test_results_filtered_view` for analysis

### Too many false positives detected

- Review detection criteria in `is_false_positive()` function
- Adjust logic if needed for your API
- File issue if legitimate errors are being filtered

### Missing false positive filtering

- Ensure database was created with `--create-schema` flag
- Re-parse with latest version of `parse-cats-results.py`
- Check `is_oauth_false_positive` column exists in tests table

<!-- VERIFICATION SUMMARY
Verified 2025-01-24:
- File: scripts/parse-cats-results.py EXISTS - contains is_false_positive() function (lines 463-817)
- File: scripts/query-cats-results.sh EXISTS - contains query examples
- File: docs/developer/testing/cats-public-endpoints.md EXISTS - related documentation
- Make targets: parse-cats-results, query-cats-results, cats-fuzz, analyze-cats-results - all verified in Makefile
- Database views: test_results_view, test_results_filtered_view - verified in VIEWS_SQL (lines 161-241)
- Detection categories: 16+ categories verified in is_false_positive() function
- Function alias: is_oauth_auth_false_positive() -> is_false_positive() verified (lines 814-816)

Corrections made:
- Renamed document focus from "OAuth False Positives" to "CATS False Positives" to reflect actual scope
- Updated detection criteria section to accurately reflect 16+ categories in source code
- Added comprehensive category table matching actual implementation
- Updated function name references (is_false_positive with alias is_oauth_auth_false_positive)
- Added security hardening and injection testing sections to match code
- Retained backward-compatible section titles for link stability
-->
