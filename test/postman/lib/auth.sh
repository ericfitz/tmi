#!/bin/bash
# Shared OAuth-stub authentication helpers for the Postman runners.
# Source this file; it defines functions only and does not change shell options.
#
#   OAUTH_STUB_URL  OAuth stub base URL (default http://127.0.0.1:8079)
#   TMI_BASE_URL    TMI server base URL (default http://127.0.0.1:8080)
#
# Tokens are never printed to stderr or logged; only authenticate_user writes a
# token, to stdout, for the caller to capture.

# token_is_valid <token>: succeed only if the server accepts the token (GET /me -> 200).
# Any other result (401, 000 unreachable, 5xx) means invalid.
token_is_valid() {
    local token="$1" code
    code="$(_token_check_code "$token")"
    [ "$code" = "200" ]
}

# _token_check_code <token>: print the HTTP status of GET /me with the token (000 if unreachable).
_token_check_code() {
    local token="$1" code
    code="$(curl -s -o /dev/null -w '%{http_code}' \
        -H "Authorization: Bearer ${token}" \
        "${TMI_BASE_URL:-http://127.0.0.1:8080}/me" 2>/dev/null)" || true
    printf '%s' "${code:-000}"
}

# authenticate_user <username>: print a server-accepted access token to stdout.
# Reuses the stub's cached token only if the server accepts it; otherwise runs a
# fresh automated flow (POST /flows/start, poll /flows/{id} up to ~10s).
authenticate_user() {
    local username="$1"
    local stub="${OAUTH_STUB_URL:-http://127.0.0.1:8079}"
    echo "Checking existing token for $username..." >&2

    local existing_token
    existing_token="$(curl -s "$stub/creds?userid=$username" 2>/dev/null | jq -r '.access_token // empty' 2>/dev/null)" || existing_token=""

    if [ -n "$existing_token" ] && [ "$existing_token" != "null" ] && [ "$existing_token" != "undefined" ]; then
        local code
        code="$(_token_check_code "$existing_token")"
        if [ "$code" = "200" ]; then
            echo "✅ Using existing cached token for $username" >&2
            printf "%s" "$existing_token"
            return 0
        fi
        echo "cached token for $username rejected by server (HTTP $code); re-authenticating" >&2
    else
        echo "🔄 No cached token found, authenticating $username..." >&2
    fi

    local flow_response flow_id
    flow_response="$(curl -s -X POST "$stub/flows/start" \
        -H "Content-Type: application/json" \
        -d "{\"userid\": \"$username\"}")" || flow_response=""
    flow_id="$(echo "$flow_response" | jq -r '.flow_id' 2>/dev/null)" || flow_id=""

    if [ "$flow_id" == "null" ] || [ -z "$flow_id" ]; then
        echo "❌ Failed to start OAuth flow for $username" >&2
        echo "Response: $flow_response" >&2
        return 1
    fi

    local status_response status tokens_ready token error
    for _ in 1 2 3 4 5 6 7 8 9 10; do
        status_response="$(curl -s "$stub/flows/$flow_id")" || status_response=""
        status="$(echo "$status_response" | jq -r '.status' 2>/dev/null)" || status=""
        tokens_ready="$(echo "$status_response" | jq -r '.tokens_ready' 2>/dev/null)" || tokens_ready=""

        if [ "$tokens_ready" == "true" ]; then
            token="$(echo "$status_response" | jq -r '.tokens.access_token' 2>/dev/null)" || token=""
            if [ "$token" != "null" ] && [ -n "$token" ]; then
                echo "✅ Token retrieved for $username" >&2
                printf "%s" "$token"
                return 0
            fi
        fi

        if [ "$status" == "failed" ] || [ "$status" == "error" ]; then
            error="$(echo "$status_response" | jq -r '.error' 2>/dev/null)" || error=""
            echo "❌ OAuth flow failed for $username: $error" >&2
            return 1
        fi

        sleep 1
    done

    echo "❌ Timeout waiting for OAuth flow completion for $username" >&2
    return 1
}
