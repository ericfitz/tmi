# Add rate limit headers to all audit trail endpoint 200 responses

def rate_limit_headers:
  {
    "X-RateLimit-Limit": {
      "description": "Maximum number of requests allowed in the current time window",
      "schema": {
        "type": "integer",
        "example": 1000
      }
    },
    "X-RateLimit-Remaining": {
      "description": "Number of requests remaining in the current time window",
      "schema": {
        "type": "integer",
        "example": 999
      }
    },
    "X-RateLimit-Reset": {
      "description": "Unix epoch seconds when the rate limit window resets",
      "schema": {
        "type": "integer",
        "example": 1735689600
      }
    }
  };

def rate_limit_extension:
  {
    "scope": "user",
    "tier": "resource-operations",
    "limits": [
      {
        "type": "requests_per_minute",
        "default": 100,
        "configurable": true,
        "quota_source": "user_api_quotas"
      }
    ]
  };

# Fix all audit trail paths
(.paths | to_entries | map(select(.key | test("audit_trail"))) | .[].key) as $paths |
reduce ($paths // empty) as $path (.;
  # Fix each HTTP method in the path
  reduce (.paths[$path] | keys | .[]) as $method (.;
    # Add rate limit headers to 200 response
    if .paths[$path][$method].responses["200"] then
      .paths[$path][$method].responses["200"].headers = rate_limit_headers
    else . end
    |
    # Add rate limit headers to 410 response if it exists
    if .paths[$path][$method].responses["410"] then
      .paths[$path][$method].responses["410"].headers = rate_limit_headers
    else . end
    |
    # Add x-rate-limit extension
    .paths[$path][$method]["x-rate-limit"] = rate_limit_extension
  )
)
