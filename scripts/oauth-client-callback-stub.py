#!/usr/bin/env python3

# /// script
# dependencies = ["requests>=2.32.0"]
# ///

"""
OAuth Client Callback Stub - Development Testing Tool for PKCE

This stub provides a comprehensive OAuth 2.0 testing harness with PKCE support.
It plays two roles in one process:

A) Client-side callback receiver - drives flows that hit TMI as the OAuth IdP.
B) Upstream provider stub - acts as an OAuth 2.0 + PKCE provider that TMI's
   delegated content-OAuth subsystem can call out to (issue #301).

Client-side endpoints:
1. POST /oauth/init - Initialize OAuth flow with PKCE parameters
2. POST /refresh - Refresh access token using refresh token
3. POST /flows/start - Start automated end-to-end OAuth flow
4. GET /flows/{flow_id} - Poll flow status and retrieve tokens
5. GET /creds?userid=<user> - Retrieve saved credentials for user
6. GET /latest - Get latest OAuth callback data
7. GET / - OAuth callback receiver (redirect from TMI server)

Provider-stub endpoints (RFC 6749 + RFC 7636 + RFC 7009 shaped):
8. GET /provider/authorize - Authorization endpoint; redirects with code+state
9. POST /provider/token - Token endpoint; authorization_code & refresh_token grants
10. GET /provider/userinfo - OIDC-style userinfo for issued access tokens
11. POST /provider/revoke - RFC 7009 token revocation

Usage: make start-oauth-stub
Docs: See function docstrings for endpoint details
"""

import http.server
import socketserver
import urllib.parse
import sys
import signal
import argparse
import json
import logging
import datetime
import os
import glob
import re
import tempfile
import base64
import uuid
import requests  # ty:ignore[unresolved-import]
import hashlib
import secrets
import threading
import time

# Global reference to the server for shutdown from signal/request handlers
_server_instance: "ReusableTCPServer | None" = None

# Global storage for latest OAuth credentials
latest_oauth_credentials: dict[str, str | None] = {
    "flow_type": None,
    "code": None,
    "state": None,
    "access_token": None,
    "refresh_token": None,
    "token_type": None,
    "expires_in": None,
}

# Global storage for PKCE verifiers indexed by state parameter
pkce_verifiers = {}

# Global storage for OAuth flows (flow_id -> flow_data)
oauth_flows = {}

# Provider-stub state: code -> {client_id, code_challenge, code_challenge_method,
# scope, redirect_uri, expires_at}. Single-use; consumed by /provider/token.
provider_auth_codes: dict = {}

# Provider-stub state: access_token -> {sub, email, name, scope, client_id,
# refresh_token, expires_at}. Looked up by /provider/userinfo.
provider_access_tokens: dict = {}

# Provider-stub state: refresh_token -> {sub, email, name, scope, client_id}.
# Used by /provider/token grant_type=refresh_token.
provider_refresh_tokens: dict = {}

# Provider-stub configuration (set in main from CLI flags).
provider_stub_account_id = "stub-user-123"
provider_stub_account_label = "stub-user@stub.local"
provider_stub_simulate_down = False
provider_stub_simulate_token_error = False

# Lifetimes (seconds) for provider-stub artifacts.
PROVIDER_AUTH_CODE_TTL = 60
PROVIDER_ACCESS_TOKEN_TTL = 3600

# Global logger instance - initialized with NullHandler, configured in setup_logging()
logger: logging.Logger = logging.getLogger("oauth_stub")
logger.addHandler(logging.NullHandler())

# Default configuration
DEFAULT_IDP = "tmi"
DEFAULT_SCOPES = "openid profile email"
DEFAULT_TMI_SERVER = "http://localhost:8080"
DEFAULT_CALLBACK_URL = "http://localhost:8079/"


class PKCEHelper:
    """Helper class for PKCE (Proof Key for Code Exchange) operations."""

    @staticmethod
    def generate_code_verifier():
        """
        Generate a cryptographically random code verifier.

        Returns a 43-character base64url-encoded string (32 random bytes).
        Per RFC 7636, verifier must be 43-128 characters.
        """
        # Generate 32 random bytes
        verifier_bytes = secrets.token_bytes(32)
        # Encode as base64url without padding
        verifier = base64.urlsafe_b64encode(verifier_bytes).decode("utf-8").rstrip("=")
        return verifier

    @staticmethod
    def generate_code_challenge(verifier):
        """
        Generate S256 code challenge from verifier.

        Args:
            verifier: The code verifier string

        Returns:
            base64url(SHA256(verifier)) without padding
        """
        # Compute SHA-256 hash of the verifier
        digest = hashlib.sha256(verifier.encode("utf-8")).digest()
        # Encode as base64url without padding
        challenge = base64.urlsafe_b64encode(digest).decode("utf-8").rstrip("=")
        return challenge


def generate_state():
    """Generate a cryptographically random state parameter for CSRF protection."""
    return secrets.token_urlsafe(32)


def build_authorization_url(
    idp, state, code_challenge, scopes, login_hint=None, tmi_server=None
):
    """
    Build TMI OAuth authorization URL with all required parameters.

    Args:
        idp: OAuth provider ID (e.g., "tmi", "google")
        state: CSRF protection state parameter
        code_challenge: PKCE code challenge
        scopes: Space-separated OAuth scopes
        login_hint: Optional user identity hint for TMI provider
        tmi_server: TMI server base URL (defaults to DEFAULT_TMI_SERVER)

    Returns:
        Complete authorization URL ready for browser redirect
    """
    if not tmi_server:
        tmi_server = DEFAULT_TMI_SERVER

    params = {
        "idp": idp,
        "scope": scopes,
        "code_challenge": code_challenge,
        "code_challenge_method": "S256",
        "state": state,
        "client_callback": DEFAULT_CALLBACK_URL,
    }

    if login_hint:
        params["login_hint"] = login_hint

    query_string = urllib.parse.urlencode(params)
    return f"{tmi_server}/oauth2/authorize?{query_string}"


def create_flow(
    userid=None,
    idp=None,
    scopes=None,
    state=None,
    code_verifier=None,
    code_challenge=None,
    login_hint=None,
    tmi_server=None,
):
    """
    Create a new OAuth flow with generated or caller-provided parameters.

    Args:
        userid: Optional user identifier for credential storage
        idp: OAuth provider (defaults to DEFAULT_IDP if not specified)
        scopes: OAuth scopes (defaults to DEFAULT_SCOPES if not specified)
        state: CSRF state (auto-generated if not specified)
        code_verifier: PKCE verifier (auto-generated if not specified)
        code_challenge: PKCE challenge (auto-generated from verifier if not specified)
        login_hint: User identity hint for TMI provider
        tmi_server: TMI server URL (defaults to DEFAULT_TMI_SERVER)

    Returns:
        Dictionary with flow_id, authorization_url, and flow metadata
    """
    global oauth_flows, pkce_verifiers

    # Apply defaults for unspecified parameters
    if not idp:
        idp = DEFAULT_IDP
    if not scopes:
        scopes = DEFAULT_SCOPES
    if not state:
        state = generate_state()
    if not code_verifier:
        code_verifier = PKCEHelper.generate_code_verifier()
    if not code_challenge:
        code_challenge = PKCEHelper.generate_code_challenge(code_verifier)
    if not tmi_server:
        tmi_server = DEFAULT_TMI_SERVER

    # Generate flow ID
    flow_id = str(uuid.uuid4())

    # Build authorization URL
    authorization_url = build_authorization_url(
        idp=idp,
        state=state,
        code_challenge=code_challenge,
        scopes=scopes,
        login_hint=login_hint or userid,
        tmi_server=tmi_server,
    )

    # Store PKCE verifier for token exchange
    pkce_verifiers[state] = code_verifier

    # Create flow record
    flow_data = {
        "flow_id": flow_id,
        "userid": userid,
        "idp": idp,
        "scopes": scopes,
        "state": state,
        "code_verifier": code_verifier,
        "code_challenge": code_challenge,
        "authorization_url": authorization_url,
        "tmi_server": tmi_server,
        "status": "initialized",
        "created_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "authorization_code": None,
        "tokens": None,
        "error": None,
    }

    oauth_flows[flow_id] = flow_data

    logger.info(
        f"Created OAuth flow {flow_id} for user {userid or 'anonymous'} with provider {idp}"
    )

    return flow_data


def refresh_token(refresh_token_value, userid=None, idp=None, tmi_server=None):
    """
    Refresh access token using refresh token.

    Args:
        refresh_token_value: The refresh token from previous authorization
        userid: Optional user identifier for logging
        idp: OAuth provider (defaults to DEFAULT_IDP)
        tmi_server: TMI server URL (defaults to DEFAULT_TMI_SERVER)

    Returns:
        Dictionary with new tokens or error information
    """
    if not idp:
        idp = DEFAULT_IDP
    if not tmi_server:
        tmi_server = DEFAULT_TMI_SERVER

    token_url = f"{tmi_server}/oauth2/refresh?idp={idp}"

    try:
        logger.info(
            f"Refreshing token for user {userid or 'anonymous'} with provider {idp}"
        )

        response = requests.post(
            token_url,
            json={"refresh_token": refresh_token_value},
            headers={"Content-Type": "application/json"},
            timeout=10,
        )

        if response.status_code == 200:
            token_data = response.json()
            logger.info(
                f"Successfully refreshed token for user {userid or 'anonymous'}"
            )
            return {
                "success": True,
                "access_token": token_data.get("access_token"),
                "refresh_token": token_data.get("refresh_token"),
                "token_type": token_data.get("token_type", "Bearer"),
                "expires_in": token_data.get("expires_in", 3600),
            }
        else:
            error_msg = (
                f"Token refresh failed: {response.status_code} - {response.text}"
            )
            logger.error(error_msg)
            return {
                "success": False,
                "error": error_msg,
                "status_code": response.status_code,
            }

    except Exception as e:
        error_msg = f"Token refresh exception: {str(e)}"
        logger.error(error_msg)
        return {
            "success": False,
            "error": error_msg,
        }


def setup_logging():
    """Set up dual logging to file and console with RFC3339 timestamps."""
    # Configure the global logger (already created at module level)
    logger.setLevel(logging.INFO)

    # Clear any existing handlers (including NullHandler from initialization)
    logger.handlers.clear()

    # Create custom formatter with RFC3339 timestamp
    class RFC3339Formatter(logging.Formatter):
        def formatTime(self, record, datefmt=None):
            dt = datetime.datetime.fromtimestamp(
                record.created, tz=datetime.timezone.utc
            )
            return (
                dt.strftime("%Y-%m-%dT%H:%M:%S.%fZ")[:-3] + "Z"
            )  # RFC3339 with milliseconds

    formatter = RFC3339Formatter("%(asctime)s %(message)s")

    # File handler for /tmp/oauth-stub.log
    try:
        file_handler = logging.FileHandler("/tmp/oauth-stub.log")
        file_handler.setLevel(logging.INFO)
        file_handler.setFormatter(formatter)
        logger.addHandler(file_handler)
    except Exception as e:
        # If we can't write to /tmp, continue with console-only logging
        print(f"Warning: Cannot write to /tmp/oauth-stub.log: {e}")

    # Console handler for stdout
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(logging.INFO)
    console_handler.setFormatter(formatter)
    logger.addHandler(console_handler)

    return logger


def signal_handler(sig, frame):
    """Handle SIGTERM for graceful shutdown."""
    global _server_instance
    logger.info("Received SIGTERM, shutting down gracefully...")
    cleanup_temp_files()
    if _server_instance is not None:
        _server_instance.shutdown()


def _provider_send_json(handler, status, payload):
    """Send a JSON response with the given status code from a provider-stub handler."""
    body = json.dumps(payload).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Content-Length", str(len(body)))
    handler.send_header("Cache-Control", "no-store")
    handler.send_header("Pragma", "no-cache")
    handler.end_headers()
    handler.wfile.write(body)


def _provider_send_oauth_error(handler, status, error_code, description=None):
    """Send an RFC 6749 §5.2 / RFC 7009 §2.2 shaped error response."""
    payload = {"error": error_code}
    if description:
        payload["error_description"] = description
    _provider_send_json(handler, status, payload)


def _provider_check_simulate_down(handler):
    """When --simulate-down is set, return 503 from any /provider/* route.

    Returns True if the handler already wrote a response (caller should return).
    """
    if provider_stub_simulate_down:
        _provider_send_oauth_error(
            handler, 503, "temporarily_unavailable", "provider stub simulate-down"
        )
        return True
    return False


def _provider_issue_token_pair(client_id, scope):
    """Mint a (access_token, refresh_token) pair backed by the stub's account."""
    access_token = "stub-at-" + secrets.token_urlsafe(32)
    refresh_token = "stub-rt-" + secrets.token_urlsafe(32)
    now = time.time()
    sub = provider_stub_account_id
    label = provider_stub_account_label
    provider_access_tokens[access_token] = {
        "sub": sub,
        "email": label,
        "name": label,
        "scope": scope,
        "client_id": client_id,
        "refresh_token": refresh_token,
        "expires_at": now + PROVIDER_ACCESS_TOKEN_TTL,
    }
    provider_refresh_tokens[refresh_token] = {
        "sub": sub,
        "email": label,
        "name": label,
        "scope": scope,
        "client_id": client_id,
    }
    return access_token, refresh_token


def _provider_parse_form_body(handler):
    """Parse an application/x-www-form-urlencoded request body into a flat dict.

    Returns (params, error_response_sent). When error_response_sent is True the
    caller should return immediately (a 400 has already been written).
    """
    content_length = int(handler.headers.get("Content-Length", "0") or "0")
    raw = handler.rfile.read(content_length).decode("utf-8") if content_length else ""
    parsed = urllib.parse.parse_qs(raw, keep_blank_values=True)
    return {k: v[0] for k, v in parsed.items()}, False


def _provider_handle_authorize(handler, query_params):
    """GET /provider/authorize - issue a single-use code and redirect."""
    if _provider_check_simulate_down(handler):
        return

    client_id = query_params.get("client_id", [None])[0]
    redirect_uri = query_params.get("redirect_uri", [None])[0]
    state = query_params.get("state", [None])[0]
    code_challenge = query_params.get("code_challenge", [None])[0]
    code_challenge_method = (
        query_params.get("code_challenge_method", ["S256"])[0] or "S256"
    )
    scope = query_params.get("scope", [""])[0]
    response_type = query_params.get("response_type", ["code"])[0]

    # RFC 6749 §4.1.1: missing/invalid redirect_uri → cannot redirect, 400 directly.
    if not redirect_uri:
        _provider_send_oauth_error(
            handler, 400, "invalid_request", "redirect_uri is required"
        )
        return
    if not client_id:
        _provider_send_oauth_error(
            handler, 400, "invalid_request", "client_id is required"
        )
        return

    # Past this point, errors must redirect back per RFC 6749 §4.1.2.1.
    def _redirect_error(err_code, description):
        q = {"error": err_code, "error_description": description}
        if state:
            q["state"] = state
        sep = "&" if "?" in redirect_uri else "?"
        location = redirect_uri + sep + urllib.parse.urlencode(q)
        handler.send_response(302)
        handler.send_header("Location", location)
        handler.end_headers()

    if response_type != "code":
        _redirect_error("unsupported_response_type", "only response_type=code supported")
        return
    if not code_challenge:
        _redirect_error("invalid_request", "code_challenge is required (PKCE)")
        return
    if code_challenge_method != "S256":
        _redirect_error(
            "invalid_request",
            "code_challenge_method must be S256 (plain not supported)",
        )
        return

    code = "stub-code-" + secrets.token_urlsafe(24)
    provider_auth_codes[code] = {
        "client_id": client_id,
        "code_challenge": code_challenge,
        "code_challenge_method": code_challenge_method,
        "scope": scope,
        "redirect_uri": redirect_uri,
        "expires_at": time.time() + PROVIDER_AUTH_CODE_TTL,
    }

    q = {"code": code}
    if state:
        q["state"] = state
    sep = "&" if "?" in redirect_uri else "?"
    location = redirect_uri + sep + urllib.parse.urlencode(q)
    handler.send_response(302)
    handler.send_header("Location", location)
    handler.end_headers()
    logger.info(f"provider-stub: issued code for client_id={client_id} → {redirect_uri}")


def _provider_handle_userinfo(handler):
    """GET /provider/userinfo - return account info for a Bearer access token."""
    if _provider_check_simulate_down(handler):
        return

    auth = handler.headers.get("Authorization", "")
    if not auth.startswith("Bearer "):
        handler.send_response(401)
        handler.send_header(
            "WWW-Authenticate", 'Bearer realm="provider-stub", error="invalid_token"'
        )
        handler.end_headers()
        return

    token = auth[len("Bearer "):].strip()
    rec = provider_access_tokens.get(token)
    if not rec or rec["expires_at"] < time.time():
        handler.send_response(401)
        handler.send_header(
            "WWW-Authenticate",
            'Bearer realm="provider-stub", error="invalid_token", '
            'error_description="token unknown or expired"',
        )
        handler.end_headers()
        return

    _provider_send_json(
        handler,
        200,
        {
            "sub": rec["sub"],
            "email": rec["email"],
            "name": rec["name"],
            "email_verified": True,
        },
    )


def _provider_handle_token(handler):
    """POST /provider/token - authorization_code or refresh_token grants."""
    if _provider_check_simulate_down(handler):
        return
    if provider_stub_simulate_token_error:
        _provider_send_oauth_error(
            handler, 400, "invalid_grant", "provider stub simulate-token-error"
        )
        return

    content_type = handler.headers.get("Content-Type", "")
    if "application/x-www-form-urlencoded" not in content_type:
        _provider_send_oauth_error(
            handler,
            400,
            "invalid_request",
            "Content-Type must be application/x-www-form-urlencoded",
        )
        return

    params, _ = _provider_parse_form_body(handler)
    grant_type = params.get("grant_type", "")

    if grant_type == "authorization_code":
        code = params.get("code", "")
        code_verifier = params.get("code_verifier", "")
        client_id = params.get("client_id", "")
        redirect_uri = params.get("redirect_uri", "")

        rec = provider_auth_codes.pop(code, None)
        if not rec:
            _provider_send_oauth_error(
                handler, 400, "invalid_grant", "code unknown or already used"
            )
            return
        if rec["expires_at"] < time.time():
            _provider_send_oauth_error(handler, 400, "invalid_grant", "code expired")
            return
        if rec["client_id"] != client_id:
            _provider_send_oauth_error(
                handler, 400, "invalid_client", "client_id does not match code"
            )
            return
        if redirect_uri and redirect_uri != rec["redirect_uri"]:
            _provider_send_oauth_error(
                handler, 400, "invalid_grant", "redirect_uri does not match"
            )
            return
        if not code_verifier:
            _provider_send_oauth_error(
                handler, 400, "invalid_request", "code_verifier required (PKCE)"
            )
            return
        expected = PKCEHelper.generate_code_challenge(code_verifier)
        if expected != rec["code_challenge"]:
            _provider_send_oauth_error(
                handler, 400, "invalid_grant", "PKCE verification failed"
            )
            return

        access_token, refresh_token = _provider_issue_token_pair(client_id, rec["scope"])
        _provider_send_json(
            handler,
            200,
            {
                "access_token": access_token,
                "token_type": "Bearer",
                "expires_in": PROVIDER_ACCESS_TOKEN_TTL,
                "refresh_token": refresh_token,
                "scope": rec["scope"],
            },
        )
        return

    if grant_type == "refresh_token":
        refresh_token = params.get("refresh_token", "")
        client_id = params.get("client_id", "")
        rec = provider_refresh_tokens.get(refresh_token)
        if not rec:
            _provider_send_oauth_error(
                handler, 400, "invalid_grant", "refresh_token unknown"
            )
            return
        if rec["client_id"] != client_id:
            _provider_send_oauth_error(
                handler, 400, "invalid_client", "client_id does not match refresh_token"
            )
            return
        new_access, new_refresh = _provider_issue_token_pair(client_id, rec["scope"])
        # Rotate: invalidate the old refresh token.
        provider_refresh_tokens.pop(refresh_token, None)
        _provider_send_json(
            handler,
            200,
            {
                "access_token": new_access,
                "token_type": "Bearer",
                "expires_in": PROVIDER_ACCESS_TOKEN_TTL,
                "refresh_token": new_refresh,
                "scope": rec["scope"],
            },
        )
        return

    _provider_send_oauth_error(
        handler, 400, "unsupported_grant_type", f"grant_type={grant_type!r}"
    )


def _provider_handle_revoke(handler):
    """POST /provider/revoke - RFC 7009 token revocation. Always 200 for unknown."""
    if _provider_check_simulate_down(handler):
        return

    content_type = handler.headers.get("Content-Type", "")
    if "application/x-www-form-urlencoded" not in content_type:
        _provider_send_oauth_error(
            handler,
            400,
            "invalid_request",
            "Content-Type must be application/x-www-form-urlencoded",
        )
        return

    params, _ = _provider_parse_form_body(handler)
    token = params.get("token", "")
    if not token:
        _provider_send_oauth_error(handler, 400, "invalid_request", "token required")
        return

    # Revoke whichever bucket the token belongs to. Per RFC 7009 §2.2, the
    # response is 200 even if the token is unknown.
    provider_access_tokens.pop(token, None)
    provider_refresh_tokens.pop(token, None)
    handler.send_response(200)
    handler.send_header("Content-Length", "0")
    handler.end_headers()


class OAuthRedirectHandler(http.server.BaseHTTPRequestHandler):
    """Custom handler for OAuth redirect requests."""

    def log_message(self, format, *args):
        """Override default logging to prevent duplicate logs."""
        # Suppress the default HTTP server logs since we'll do our own structured logging
        pass

    def do_GET(self):
        """Handle GET requests to the redirect URI and API endpoints."""
        global _server_instance, latest_oauth_credentials

        # Get client IP and HTTP version for logging
        client_ip = self.client_address[0]
        http_version = self.request_version
        method = "GET"

        try:
            # Parse the URL path and query parameters
            parsed_url = urllib.parse.urlparse(self.path)
            path = parsed_url.path
            query_params = urllib.parse.parse_qs(parsed_url.query)

            # DETAILED LOGGING: Log everything received from server
            logger.info(f"INCOMING REQUEST: {method} {self.path}")
            logger.info(f"  Path: {path}")
            logger.info(f"  Query string: {parsed_url.query}")
            logger.info(f"  All query params: {dict(query_params)}")

            # Log all query parameters individually
            for param_name, param_values in query_params.items():
                logger.info(f"  Param '{param_name}': {param_values}")

            # Route 1: OAuth callback endpoint (/)
            if path == "/":
                # Extract 'code' and 'state' parameters
                code = query_params.get("code", [None])[0]
                state = query_params.get("state", [None])[0]

                # Check if code is 'exit' to trigger graceful shutdown BEFORE any processing
                if code == "exit":
                    logger.info(
                        "Received 'exit' in code parameter, shutting down gracefully..."
                    )

                    # Send simple response
                    response_body = b"OAuth stub shutting down..."
                    self.send_response(200)
                    self.send_header("Content-type", "text/plain")
                    self.end_headers()
                    self.wfile.write(response_body)

                    # Log API request
                    client_ip = self.client_address[0]
                    http_version = self.request_version
                    logger.info(
                        f'API request: {client_ip} {method} {self.path} {http_version} 200 "Shutdown requested"'
                    )

                    cleanup_temp_files()
                    global _server_instance
                    if _server_instance is not None:
                        threading.Thread(
                            target=_server_instance.shutdown, daemon=True
                        ).start()
                    return

                # Extract additional OAuth parameters that may help identify the user
                login_hint = query_params.get("login_hint", [None])[0]

                # Initialize token variables with defaults (will be set in all code paths)
                access_token: str | None = None
                refresh_token: str | None = None
                token_type: str | None = None
                expires_in: str | None = None

                # TMI only supports Authorization Code Flow with PKCE
                if code:
                    flow_type = "authorization_code"
                    logger.info("  FLOW TYPE: Authorization Code Flow with PKCE")

                    # For authorization code flow, generate access tokens for testing
                    # Extract user info from login_hint or create a default user from the authorization code
                    user_id = None
                    login_hint_user = None

                    if login_hint and login_hint not in ["exit"]:
                        # Validate login_hint format
                        if re.match(r"^[a-zA-Z0-9-]{3,20}$", login_hint):
                            user_id = f"{login_hint}@tmi.local"
                            login_hint_user = login_hint

                    # If no login_hint, use default user for testing
                    if not user_id and code:
                        # For API testing, use postman-user as the default user ID
                        # This ensures consistency with test collection expectations
                        login_hint_user = "postman-user"
                        user_id = f"{login_hint_user}@tmi.local"
                        logger.info(f"  Using default test user ID: {user_id}")

                    # If we have a valid user_id, exchange the code for real tokens using TMI server
                    if user_id and login_hint_user and code:
                        # Use TMI server's token exchange endpoint to get real tokens with PKCE
                        try:
                            # Retrieve code_verifier for this state (required for PKCE)
                            code_verifier = pkce_verifiers.get(state)
                            if not code_verifier:
                                logger.error(
                                    f"  PKCE verifier not found for state {state} - cannot exchange code without verifier"
                                )
                                logger.error(
                                    "  This likely means the OAuth flow was not initiated through this stub"
                                )
                                logger.error(
                                    f"  Available states: {list(pkce_verifiers.keys())}"
                                )
                                # Update flow with error if this belongs to a tracked flow
                                for fid, fdata in oauth_flows.items():
                                    if fdata.get("state") == state:
                                        oauth_flows[fid]["status"] = "error"
                                        oauth_flows[fid]["error"] = (
                                            "PKCE verifier not found - flow was not initiated through this stub"
                                        )
                                        oauth_flows[fid]["authorization_code"] = code
                                        break
                                # Skip token exchange - just store the code
                                access_token = None
                                refresh_token = None
                                token_type = "Bearer"
                                expires_in = "3600"
                            else:
                                # PKCE verifier found - proceed with token exchange
                                # Determine TMI server URL from the flow data (if this callback belongs to a tracked flow)
                                flow_tmi_server = DEFAULT_TMI_SERVER
                                for fid, fdata in oauth_flows.items():
                                    if fdata.get("state") == state and fdata.get("tmi_server"):
                                        flow_tmi_server = fdata["tmi_server"]
                                        break
                                token_url = f"{flow_tmi_server}/oauth2/token?idp=tmi"
                                token_data = {
                                    "grant_type": "authorization_code",
                                    "code": code,
                                    "code_verifier": code_verifier,
                                    "redirect_uri": "http://localhost:8079/",
                                }

                                logger.info(
                                    "  Exchanging authorization code for real tokens via TMI server (PKCE)..."
                                )
                                logger.info(f"    Token URL: {token_url}")
                                logger.info(f"    Code: {code}")
                                logger.info(
                                    f"    Code Verifier: {code_verifier[:20]}... (length: {len(code_verifier)})"
                                )

                                # Make the token exchange request to TMI server
                                # Retry on 429 (rate limit) with short backoff
                                response = None
                                for attempt in range(5):
                                    response = requests.post(
                                        token_url,
                                        json=token_data,
                                        headers={"Content-Type": "application/json"},
                                        timeout=10,
                                    )
                                    if response.status_code != 429:
                                        break
                                    retry_after = min(int(response.headers.get("Retry-After", 1)), 3)
                                    logger.info(
                                        f"  Token exchange rate limited (attempt {attempt + 1}/5), retrying in {retry_after}s..."
                                    )
                                    time.sleep(retry_after)

                                assert response is not None  # loop always runs at least once
                                if response.status_code == 200:
                                    token_response = response.json()
                                    access_token = token_response.get("access_token")
                                    refresh_token = token_response.get("refresh_token")
                                    token_type = token_response.get(
                                        "token_type", "Bearer"
                                    )
                                    expires_in = str(
                                        token_response.get("expires_in", 3600)
                                    )

                                    logger.info(
                                        "  Successfully exchanged code for real tokens:"
                                    )
                                    logger.info(
                                        f"    Access Token: {access_token[:50] if access_token else 'None'}..."
                                    )
                                    logger.info(f"    Refresh Token: {refresh_token}")
                                    logger.info(f"    Token Type: {token_type}")
                                    logger.info(f"    Expires In: {expires_in}s")

                                    # Update flow record if this redirect belongs to a flow
                                    for fid, fdata in oauth_flows.items():
                                        if fdata.get("state") == state:
                                            oauth_flows[fid]["status"] = "completed"
                                            oauth_flows[fid]["tokens"] = {
                                                "access_token": access_token,
                                                "refresh_token": refresh_token,
                                                "token_type": token_type,
                                                "expires_in": int(expires_in)
                                                if expires_in
                                                else 3600,
                                            }
                                            oauth_flows[fid]["error"] = (
                                                None  # Clear any timeout errors
                                            )
                                            logger.info(
                                                f"  Updated flow {fid} with tokens"
                                            )
                                            break

                                else:
                                    logger.error(
                                        f"  Token exchange failed: {response.status_code} - {response.text}"
                                    )
                                    # Fall back to storing just the code for client to handle
                                    access_token = None
                                    refresh_token = None
                                    token_type = "Bearer"
                                    expires_in = "3600"

                                    # Update flow record with error if this redirect belongs to a flow
                                    for fid, fdata in oauth_flows.items():
                                        if fdata.get("state") == state:
                                            oauth_flows[fid]["status"] = "error"
                                            oauth_flows[fid]["error"] = (
                                                f"Token exchange failed: {response.status_code}"
                                            )
                                            oauth_flows[fid]["authorization_code"] = (
                                                code
                                            )
                                            logger.info(
                                                f"  Updated flow {fid} with error"
                                            )
                                            break

                        except Exception as e:
                            logger.error(
                                f"  Failed to exchange authorization code: {e}"
                            )
                            # Fall back to storing just the code for client to handle
                            access_token = None
                            refresh_token = None
                            token_type = "Bearer"
                            expires_in = "3600"
                else:
                    flow_type = "unknown"
                    access_token = None
                    refresh_token = None
                    token_type = None
                    expires_in = None
                    logger.info("  FLOW TYPE: Unknown - no authorization code received")

                # Store the latest OAuth credentials with flow type
                latest_oauth_credentials.update(
                    {
                        "flow_type": flow_type,
                        "code": code,
                        "state": state,
                        "access_token": access_token,
                        "refresh_token": refresh_token,
                        "token_type": token_type,
                        "expires_in": expires_in,
                    }
                )

                # If we have valid credentials, try to extract user ID and save to file
                if flow_type != "unknown" and (access_token or code):
                    user_id = extract_user_id_from_credentials(latest_oauth_credentials)
                    if user_id:
                        # Create the same response format as /latest endpoint
                        if flow_type == "authorization_code":
                            if access_token:
                                # Save the final exchanged tokens
                                credentials_to_save = {
                                    "flow_type": "authorization_code",
                                    "state": state,
                                    "access_token": access_token,
                                    "refresh_token": refresh_token,
                                    "token_type": token_type,
                                    "expires_in": expires_in,
                                    "tokens_ready": True,
                                }
                            else:
                                # Save just the authorization code for later exchange
                                credentials_to_save = {
                                    "flow_type": "authorization_code",
                                    "code": code,
                                    "state": state,
                                    "ready_for_token_exchange": code is not None,
                                }
                        else:
                            credentials_to_save = latest_oauth_credentials.copy()

                        save_credentials_to_file(credentials_to_save, user_id)

                # Enhanced logging
                logger.info("OAUTH REDIRECT ANALYSIS:")
                logger.info(f"  Authorization Code: {code}")
                logger.info(f"  State: {state}")
                logger.info(f"  Access Token: {access_token}")
                logger.info(f"  Refresh Token: {refresh_token}")
                logger.info(f"  Token Type: {token_type}")
                logger.info(f"  Expires In: {expires_in}")
                logger.info(f"  Flow Type: {flow_type}")

                # Send simple response - authorization code flow always uses query params
                response_body = (
                    b"OAuth callback received. Check server logs for details."
                )
                self.send_response(200)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(response_body)

                # Log API request
                logger.info(
                    f'API request: {client_ip} {method} {self.path} {http_version} 200 "Redirect received. Check server logs for details."'
                )

            # Route 2: API endpoint to retrieve latest OAuth credentials (/latest)
            elif path == "/latest":
                # Build response based on flow type
                flow_type = latest_oauth_credentials.get("flow_type")

                if flow_type == "authorization_code":
                    # Authorization Code Flow - client needs code and state for token exchange
                    response_data = {
                        "flow_type": "authorization_code",
                        "code": latest_oauth_credentials["code"],
                        "state": latest_oauth_credentials["state"],
                        "ready_for_token_exchange": latest_oauth_credentials["code"]
                        is not None,
                    }
                else:
                    # Unknown or no data yet
                    response_data = {
                        "flow_type": flow_type or "none",
                        "error": "No OAuth data received yet"
                        if not flow_type
                        else "Unknown flow type",
                        "raw_data": latest_oauth_credentials,
                    }

                # Send JSON response
                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()

                response_json = json.dumps(response_data, indent=2)
                self.wfile.write(response_json.encode())

                # Log API request with JSON payload (truncated for readability)
                summary = {
                    "flow_type": response_data.get("flow_type"),
                    "has_tokens": bool(response_data.get("access_token")),
                    "has_code": bool(response_data.get("code")),
                }
                logger.info(
                    f"API request: {client_ip} {method} {self.path} {http_version} 200 {json.dumps(summary)}"
                )
                logger.info(f"Full response: {response_json}")

            # Route 3: API endpoint to poll OAuth flow status (/flows/{flow_id})
            elif path.startswith("/flows/"):
                # Extract flow_id from path
                flow_id = path.split("/")[-1]

                # Look up flow
                flow_data = oauth_flows.get(flow_id)

                if not flow_data:
                    error_msg = f"Flow not found: {flow_id}"
                    self.send_response(404)
                    self.send_header("Content-type", "application/json")
                    self.end_headers()
                    error_response = {"error": error_msg}
                    self.wfile.write(json.dumps(error_response).encode())
                    logger.info(
                        f'API request: {client_ip} {method} {self.path} {http_version} 404 "{error_msg}"'
                    )
                    return

                # Build response based on flow status
                response_data = {
                    "flow_id": flow_data["flow_id"],
                    "status": flow_data["status"],
                    "created_at": flow_data["created_at"],
                }

                # Include tokens if available
                if flow_data.get("tokens"):
                    response_data["tokens"] = flow_data["tokens"]
                    response_data["tokens_ready"] = True
                else:
                    response_data["tokens_ready"] = False

                # Include error if any
                if flow_data.get("error"):
                    response_data["error"] = flow_data["error"]

                # Include authorization code if present (for debugging)
                if flow_data.get("authorization_code"):
                    response_data["authorization_code"] = flow_data[
                        "authorization_code"
                    ]

                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                response_json = json.dumps(response_data, indent=2)
                self.wfile.write(response_json.encode())

                logger.info(f"Flow {flow_id} status: {flow_data['status']}")
                logger.info(f"Response: {response_json}")

            # Route 4: API endpoint to retrieve credentials for specific user (/creds)
            elif path == "/creds":
                # Extract userid parameter
                userid_part = query_params.get("userid", [None])[0]

                # Validate userid parameter
                if not userid_part:
                    error_msg = "Missing required parameter: userid"
                    self.send_response(400)
                    self.send_header("Content-type", "application/json")
                    self.end_headers()
                    error_response = {"error": error_msg}
                    self.wfile.write(json.dumps(error_response).encode())
                    logger.info(
                        f'API request: {client_ip} {method} {self.path} {http_version} 400 "{error_msg}"'
                    )
                    return

                if not validate_userid_parameter(userid_part):
                    error_msg = f"Invalid userid parameter: {userid_part}. Must match pattern ^[a-zA-Z0-9][a-zA-Z0-9-]{{1,18}}[a-zA-Z0-9]$"
                    self.send_response(400)
                    self.send_header("Content-type", "application/json")
                    self.end_headers()
                    error_response = {"error": error_msg}
                    self.wfile.write(json.dumps(error_response).encode())
                    logger.info(
                        f'API request: {client_ip} {method} {self.path} {http_version} 400 "{error_msg}"'
                    )
                    return

                # Form complete user ID
                complete_user_id = f"{userid_part}@tmi.local"
                logger.info(f"Looking up credentials for user: {complete_user_id}")

                # Read credentials file
                credentials, error = read_credentials_file(complete_user_id)

                if error:
                    # File not found or read error
                    if "not found" in error:
                        self.send_response(404)
                        error_response = {
                            "error": f"No credentials found for user: {complete_user_id}"
                        }
                    else:
                        self.send_response(500)
                        error_response = {
                            "error": "Internal server error reading credentials"
                        }

                    self.send_header("Content-type", "application/json")
                    self.end_headers()
                    self.wfile.write(json.dumps(error_response).encode())
                    logger.info(
                        f'API request: {client_ip} {method} {self.path} {http_version} {404 if "not found" in error else 500} "{error}"'
                    )
                    return

                # Return credentials (credentials is guaranteed non-None here since we returned on error)
                assert credentials is not None
                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()

                response_json = json.dumps(credentials, indent=2)
                self.wfile.write(response_json.encode())

                # Log successful request
                summary = {
                    "user_id": complete_user_id,
                    "flow_type": credentials.get("flow_type"),
                }
                logger.info(
                    f"API request: {client_ip} {method} {self.path} {http_version} 200 {json.dumps(summary)}"
                )
                logger.info(f"Returned credentials: {response_json}")

            # Provider-stub routes (issue #301): act as an upstream OAuth provider.
            elif path == "/provider/authorize":
                _provider_handle_authorize(self, query_params)
                logger.info(
                    f"API request: {client_ip} {method} {self.path} {http_version} (provider-stub /authorize)"
                )

            elif path == "/provider/userinfo":
                _provider_handle_userinfo(self)
                logger.info(
                    f"API request: {client_ip} {method} {self.path} {http_version} (provider-stub /userinfo)"
                )

            # Unknown route
            else:
                error_msg = f"Not Found: {path}"
                self.send_response(404)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(error_msg.encode())

                # Log API request
                logger.info(
                    f'API request: {client_ip} {method} {self.path} {http_version} 404 "{error_msg}"'
                )

        except Exception as e:
            # Handle any errors during request processing
            error_msg = f"Server error: {str(e)}"
            logger.error(f"Error processing request: {str(e)}")

            try:
                self.send_response(500)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(error_msg.encode())

                # Log API request
                logger.info(
                    f'API request: {client_ip} {method} {self.path} {http_version} 500 "{error_msg}"'
                )
            except Exception:
                # If we can't even send an error response, just log it
                logger.error("Failed to send error response to client")

    def do_POST(self):
        """Handle POST requests for OAuth flow management endpoints."""
        global oauth_flows

        client_ip = self.client_address[0]
        http_version = self.request_version
        method = "POST"

        try:
            # Parse the URL path
            parsed_url = urllib.parse.urlparse(self.path)
            path = parsed_url.path

            logger.info(f"INCOMING REQUEST: {method} {self.path}")
            logger.info(f"  Path: {path}")

            # Provider-stub POST routes (issue #301) consume their own
            # form-encoded body, so dispatch them before the JSON parser below.
            if path == "/provider/token":
                _provider_handle_token(self)
                logger.info(
                    f"API request: {client_ip} {method} {self.path} {http_version} (provider-stub /token)"
                )
                return
            if path == "/provider/revoke":
                _provider_handle_revoke(self)
                logger.info(
                    f"API request: {client_ip} {method} {self.path} {http_version} (provider-stub /revoke)"
                )
                return

            # Read request body
            content_length = int(self.headers.get("Content-Length", 0))
            request_body = (
                self.rfile.read(content_length).decode("utf-8")
                if content_length > 0
                else "{}"
            )

            try:
                request_data = json.loads(request_body) if request_body else {}
            except json.JSONDecodeError:
                self.send_response(400)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                error_response = {"error": "Invalid JSON in request body"}
                self.wfile.write(json.dumps(error_response).encode())
                logger.error(f"Invalid JSON in request body: {request_body}")
                return

            logger.info(f"  Request data: {request_data}")

            # Route 1: POST /oauth/init - Initialize OAuth flow with PKCE
            if path == "/oauth/init":
                # Extract parameters (all optional with smart defaults)
                userid = request_data.get("userid")
                idp = request_data.get("idp")
                scopes = request_data.get("scopes")
                state = request_data.get("state")
                code_verifier = request_data.get("code_verifier")
                code_challenge = request_data.get("code_challenge")
                login_hint = request_data.get("login_hint")
                tmi_server = request_data.get("tmi_server")

                # Create flow with smart defaults
                flow_data = create_flow(
                    userid=userid,
                    idp=idp,
                    scopes=scopes,
                    state=state,
                    code_verifier=code_verifier,
                    code_challenge=code_challenge,
                    login_hint=login_hint,
                    tmi_server=tmi_server,
                )

                # Return initialization data (exclude sensitive verifier)
                response_data = {
                    "state": flow_data["state"],
                    "code_challenge": flow_data["code_challenge"],
                    "authorization_url": flow_data["authorization_url"],
                    "idp": flow_data["idp"],
                    "scopes": flow_data["scopes"],
                }

                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                response_json = json.dumps(response_data, indent=2)
                self.wfile.write(response_json.encode())

                logger.info(f"OAuth init successful for state {flow_data['state']}")
                logger.info(f"Response: {response_json}")

            # Route 2: POST /refresh - Refresh access token
            elif path == "/refresh":
                refresh_token_value = request_data.get("refresh_token")
                userid = request_data.get("userid")
                idp = request_data.get("idp")
                tmi_server = request_data.get("tmi_server")

                if not refresh_token_value:
                    self.send_response(400)
                    self.send_header("Content-type", "application/json")
                    self.end_headers()
                    error_response = {"error": "Missing required field: refresh_token"}
                    self.wfile.write(json.dumps(error_response).encode())
                    logger.error("Missing refresh_token in request")
                    return

                # Call refresh helper
                result = refresh_token(
                    refresh_token_value=refresh_token_value,
                    userid=userid,
                    idp=idp,
                    tmi_server=tmi_server,
                )

                status_code = (
                    200 if result.get("success") else result.get("status_code", 500)
                )
                self.send_response(status_code)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                response_json = json.dumps(result, indent=2)
                self.wfile.write(response_json.encode())

                logger.info(f"Token refresh result: {result.get('success')}")

            # Route 3: POST /flows/start - Start automated e2e OAuth flow
            elif path == "/flows/start":
                # Extract parameters (all optional with smart defaults)
                userid = request_data.get("userid")
                idp = request_data.get("idp")
                scopes = request_data.get("scopes")
                login_hint = request_data.get("login_hint")
                tmi_server = request_data.get("tmi_server")

                # Create flow
                flow_data = create_flow(
                    userid=userid,
                    idp=idp,
                    scopes=scopes,
                    login_hint=login_hint,
                    tmi_server=tmi_server,
                )

                flow_id = flow_data["flow_id"]

                # Initiate authorization in a background thread.  Instead of
                # following redirects back to our own callback endpoint (which
                # causes self-referential connection timeouts under load), we
                # fetch the authorization URL with redirects disabled, parse
                # the code/state from the redirect location, and perform the
                # token exchange directly.
                def _run_authorization(fid, auth_url, flow):
                    try:
                        # Step 1: Hit TMI /oauth2/authorize without following redirects
                        auth_response = requests.get(
                            auth_url, allow_redirects=False, timeout=10
                        )

                        if auth_response.status_code not in (302, 303, 307):
                            oauth_flows[fid]["status"] = "error"
                            oauth_flows[fid]["error"] = (
                                f"Authorization failed: expected redirect, got {auth_response.status_code}"
                            )
                            logger.error(
                                f"Flow {fid}: Authorization failed with status {auth_response.status_code}"
                            )
                            return

                        # Step 2: Parse code and state from the redirect Location header
                        location = auth_response.headers.get("Location", "")
                        parsed = urllib.parse.urlparse(location)
                        params = urllib.parse.parse_qs(parsed.query)
                        code = params.get("code", [None])[0]
                        state = params.get("state", [None])[0]

                        if not code or not state:
                            oauth_flows[fid]["status"] = "error"
                            oauth_flows[fid]["error"] = (
                                f"Authorization redirect missing code/state: {location}"
                            )
                            logger.error(f"Flow {fid}: Missing code/state in redirect")
                            return

                        oauth_flows[fid]["authorization_code"] = code

                        # Step 3: Exchange code for tokens (same logic as do_GET callback)
                        code_verifier = pkce_verifiers.get(state)
                        if not code_verifier:
                            oauth_flows[fid]["status"] = "error"
                            oauth_flows[fid]["error"] = "PKCE verifier not found for state"
                            logger.error(f"Flow {fid}: PKCE verifier not found")
                            return

                        tmi_server = flow.get("tmi_server", DEFAULT_TMI_SERVER)
                        token_url = f"{tmi_server}/oauth2/token?idp=tmi"
                        token_data = {
                            "grant_type": "authorization_code",
                            "code": code,
                            "code_verifier": code_verifier,
                            "redirect_uri": "http://localhost:8079/",
                        }

                        logger.info(f"  Flow {fid}: Exchanging code for tokens...")

                        # Retry on 429 with short backoff (cap at 3s to stay
                        # within the Go test's 30s polling timeout)
                        response = None
                        for attempt in range(5):
                            response = requests.post(
                                token_url,
                                json=token_data,
                                headers={"Content-Type": "application/json"},
                                timeout=10,
                            )
                            if response.status_code != 429:
                                break
                            retry_after = min(int(response.headers.get("Retry-After", 1)), 3)
                            logger.info(
                                f"  Flow {fid}: Rate limited (attempt {attempt + 1}/5), retrying in {retry_after}s..."
                            )
                            time.sleep(retry_after)

                        assert response is not None  # loop always runs at least once
                        if response.status_code == 200:
                            token_response = response.json()
                            oauth_flows[fid]["status"] = "authorization_completed"
                            oauth_flows[fid]["tokens"] = {
                                "access_token": token_response.get("access_token"),
                                "refresh_token": token_response.get("refresh_token"),
                                "token_type": token_response.get("token_type", "Bearer"),
                                "expires_in": token_response.get("expires_in", 3600),
                            }
                            oauth_flows[fid]["error"] = None
                            # Save credentials to temp file
                            user_email = f"{flow.get('userid', 'unknown')}@tmi.local"
                            creds_file = os.path.join(
                                tempfile.gettempdir(), f"{user_email}.json"
                            )
                            with open(creds_file, "w") as f:
                                json.dump(
                                    {
                                        "access_token": token_response.get("access_token"),
                                        "refresh_token": token_response.get("refresh_token"),
                                        "token_type": token_response.get("token_type", "Bearer"),
                                        "expires_in": token_response.get("expires_in", 3600),
                                        "user_id": user_email,
                                    },
                                    f,
                                )
                            logger.info(f"  Flow {fid}: Token exchange successful")
                            logger.info(
                                f"  Updated flow {fid} with tokens"
                            )
                        else:
                            oauth_flows[fid]["status"] = "error"
                            oauth_flows[fid]["error"] = (
                                f"Token exchange failed: {response.status_code} - {response.text}"
                            )
                            logger.error(
                                f"Flow {fid}: Token exchange failed: {response.status_code}"
                            )

                    except Exception as e:
                        oauth_flows[fid]["status"] = "error"
                        oauth_flows[fid]["error"] = str(e)
                        logger.error(f"Flow {fid}: Authorization request failed: {e}")

                auth_thread = threading.Thread(
                    target=_run_authorization,
                    args=(flow_id, flow_data["authorization_url"], flow_data),
                    daemon=True,
                )
                auth_thread.start()

                # Return flow info immediately for polling
                response_data = {
                    "flow_id": flow_id,
                    "status": oauth_flows[flow_id]["status"],
                    "poll_url": f"/flows/{flow_id}",
                }

                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                response_json = json.dumps(response_data, indent=2)
                self.wfile.write(response_json.encode())

                logger.info(f"Started flow {flow_id}")
                logger.info(f"Response: {response_json}")

            # Unknown POST route
            else:
                error_msg = f"Not Found: {path}"
                self.send_response(404)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                error_response = {"error": error_msg}
                self.wfile.write(json.dumps(error_response).encode())
                logger.info(
                    f'API request: {client_ip} {method} {self.path} {http_version} 404 "{error_msg}"'
                )

        except Exception as e:
            error_msg = f"Server error: {str(e)}"
            logger.error(f"Error processing POST request: {str(e)}")

            try:
                self.send_response(500)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                error_response = {"error": error_msg}
                self.wfile.write(json.dumps(error_response).encode())
            except Exception:
                logger.error("Failed to send error response to client")


class ReusableTCPServer(socketserver.ThreadingMixIn, socketserver.TCPServer):
    """Threaded TCPServer with SO_REUSEADDR to allow quick restarts.

    Uses ThreadingMixIn so the server can handle concurrent requests.
    This is required for the /flows/start endpoint which follows the OAuth
    redirect chain back to this server's callback handler.
    """

    allow_reuse_address = True
    daemon_threads = True


def run_server(port):
    """Run the HTTP server on the specified port."""
    global _server_instance
    server: ReusableTCPServer | None = None
    try:
        # Set up the server with the custom handler, binding to localhost
        # Using ReusableTCPServer to allow quick restarts (avoids TIME_WAIT)
        server = ReusableTCPServer(("localhost", port), OAuthRedirectHandler)
        _server_instance = server
        logger.info(f"Server listening on http://localhost:{port}/...")

        # Handle SIGTERM for graceful shutdown
        signal.signal(signal.SIGTERM, signal_handler)

        # Serve until shutdown() is called (by signal handler or exit request).
        # Unlike the previous handle_request() loop, serve_forever() lets
        # ThreadingMixIn dispatch requests concurrently, which avoids a
        # deadlock when /flows/start follows an OAuth redirect back to this
        # server's own callback endpoint.
        server.serve_forever()

        # Close the server
        server.server_close()
        logger.info("Server has shut down.")
        cleanup_temp_files()
        sys.exit(0)

    except KeyboardInterrupt:
        logger.info("Received KeyboardInterrupt, shutting down gracefully...")
        if server is not None:
            server.shutdown()
            server.server_close()
        cleanup_temp_files()
        sys.exit(0)
    except Exception as e:
        logger.error(f"Server error: {str(e)}")
        if server is not None:
            server.server_close()
        cleanup_temp_files()
        sys.exit(1)


def cleanup_temp_files():
    """Delete all .json files in $TMP directory."""
    tmp_dir = tempfile.gettempdir()
    json_files = glob.glob(os.path.join(tmp_dir, "*.json"))

    if json_files:
        logger.info(f"Cleaning up {len(json_files)} .json files from {tmp_dir}")
        for file_path in json_files:
            try:
                os.remove(file_path)
                logger.info(f"Deleted: {file_path}")
            except OSError as e:
                logger.warning(f"Failed to delete {file_path}: {e}")
    else:
        logger.info(f"No .json files found in {tmp_dir} to clean up")


def extract_user_id_from_credentials(credentials):
    """Extract user ID from OAuth credentials if available."""
    # For TMI provider, the user ID would typically be in the access token or state
    # Since we don't decode JWTs here, we'll look for patterns in the state or other fields

    # Try to extract from state parameter (common pattern: contains user info)
    state = credentials.get("state")
    if state:
        # Look for email patterns in state - TMI includes login_hint in state
        email_match = re.search(
            r"([a-zA-Z0-9][a-zA-Z0-9-]{1,18}[a-zA-Z0-9])@tmi.local\.local", state
        )
        if email_match:
            return email_match.group(0)  # Return full email

        # Also try to decode JSON state if it contains login_hint
        try:
            # State might be base64 encoded JSON
            decoded_state = base64.b64decode(state).decode("utf-8")
            state_data = json.loads(decoded_state)
            login_hint = state_data.get("login_hint")
            if login_hint and validate_userid_parameter(login_hint):
                return f"{login_hint}@tmi.local"
        except Exception:
            pass  # Not JSON or base64, continue with other methods

    # Try to decode JWT access token for email claim (simple approach)
    access_token = credentials.get("access_token")
    if access_token:
        try:
            # JWT tokens have 3 parts separated by dots
            parts = access_token.split(".")
            if len(parts) == 3:
                # Decode payload (middle part) - add padding if needed
                payload_b64 = parts[1]
                # Add padding if needed
                payload_b64 += "=" * (4 - len(payload_b64) % 4)
                payload_json = base64.b64decode(payload_b64).decode("utf-8")
                payload = json.loads(payload_json)

                # Look for email claim
                email = payload.get("email")
                if email and email.endswith("@tmi.local"):
                    return email
        except Exception:
            pass  # JWT decoding failed, continue

    # For now, if we can't extract user ID, we'll use a default pattern
    # This will result in not saving credentials to file
    return None


def save_credentials_to_file(credentials, user_id):
    """Save credentials to a file in $TMP directory."""
    if not user_id:
        logger.warning("No user ID available, cannot save credentials to file")
        return

    tmp_dir = tempfile.gettempdir()
    file_path = os.path.join(tmp_dir, f"{user_id}.json")

    try:
        with open(file_path, "w") as f:
            json.dump(credentials, f, indent=2)
        logger.info(f"Saved credentials to: {file_path}")
    except Exception as e:
        logger.error(f"Failed to save credentials to {file_path}: {e}")


def validate_userid_parameter(userid_part):
    """Validate userid parameter against the required regex pattern."""
    if not userid_part:
        return False

    # Pattern: ^[a-zA-Z0-9][a-zA-Z0-9-]{1,18}[a-zA-Z0-9]$
    pattern = r"^[a-zA-Z0-9][a-zA-Z0-9-]{1,18}[a-zA-Z0-9]$"
    return re.match(pattern, userid_part) is not None


def read_credentials_file(user_id):
    """Read credentials file for a given user ID."""
    tmp_dir = tempfile.gettempdir()
    file_path = os.path.join(tmp_dir, f"{user_id}.json")

    try:
        if not os.path.exists(file_path):
            return None, f"Credentials file not found for user: {user_id}"

        with open(file_path, "r") as f:
            credentials = json.load(f)

        logger.info(f"Retrieved credentials from: {file_path}")
        return credentials, None
    except Exception as e:
        error_msg = f"Failed to read credentials file {file_path}: {e}"
        logger.error(error_msg)
        return None, error_msg


def daemonize(pid_file):
    """
    Fork the process to run as a daemon.

    This implements a standard Unix double-fork to properly detach from the
    controlling terminal and become a proper daemon process.
    """
    # Convert PID file to absolute path before changing directory
    abs_pid_file = os.path.abspath(pid_file) if pid_file else None

    # First fork
    try:
        pid = os.fork()
        if pid > 0:
            # Parent process - exit
            sys.exit(0)
    except OSError as e:
        sys.stderr.write(f"First fork failed: {e}\n")
        sys.exit(1)

    # Decouple from parent environment
    os.chdir("/")
    os.setsid()
    os.umask(0)

    # Second fork
    try:
        pid = os.fork()
        if pid > 0:
            # Parent (first child) - exit
            sys.exit(0)
    except OSError as e:
        sys.stderr.write(f"Second fork failed: {e}\n")
        sys.exit(1)

    # Redirect standard file descriptors to /dev/null
    sys.stdout.flush()
    sys.stderr.flush()
    with open("/dev/null", "r") as devnull:
        os.dup2(devnull.fileno(), sys.stdin.fileno())
    with open("/dev/null", "a+") as devnull:
        os.dup2(devnull.fileno(), sys.stdout.fileno())
        os.dup2(devnull.fileno(), sys.stderr.fileno())

    # Write PID file
    if abs_pid_file:
        with open(abs_pid_file, "w") as f:
            f.write(str(os.getpid()))


def main():
    """Parse command-line arguments and start the server."""
    parser = argparse.ArgumentParser(description="OAuth Redirect URI Receiver")
    parser.add_argument(
        "--port", type=int, default=8079, help="Port to listen on (default: 8079)"
    )
    parser.add_argument(
        "--daemon", action="store_true", help="Run as a background daemon"
    )
    parser.add_argument(
        "--pid-file", type=str, default=None, help="PID file path (used with --daemon)"
    )
    parser.add_argument(
        "--tmi-server",
        type=str,
        default=None,
        help="TMI server base URL (default: http://localhost:8080)",
    )
    parser.add_argument(
        "--provider-stub-account-id",
        type=str,
        default="stub-user-123",
        help="Stable account id (sub) returned by /provider/userinfo (default: stub-user-123)",
    )
    parser.add_argument(
        "--provider-stub-account-label",
        type=str,
        default="stub-user@stub.local",
        help="Account label/email returned by /provider/userinfo (default: stub-user@stub.local)",
    )
    parser.add_argument(
        "--simulate-down",
        action="store_true",
        help="Provider-stub: return 503 from all /provider/* routes (test outage handling)",
    )
    parser.add_argument(
        "--simulate-token-error",
        action="store_true",
        help="Provider-stub: return invalid_grant from /provider/token (test token-exchange failure)",
    )
    args = parser.parse_args()

    if args.tmi_server:
        global DEFAULT_TMI_SERVER
        DEFAULT_TMI_SERVER = args.tmi_server

    global provider_stub_account_id, provider_stub_account_label
    global provider_stub_simulate_down, provider_stub_simulate_token_error
    provider_stub_account_id = args.provider_stub_account_id
    provider_stub_account_label = args.provider_stub_account_label
    provider_stub_simulate_down = args.simulate_down
    provider_stub_simulate_token_error = args.simulate_token_error

    if args.port < 1 or args.port > 65535:
        sys.stderr.write(f"Port {args.port} is invalid. Must be between 1 and 65535.\n")
        sys.exit(1)

    # Daemonize before setting up logging (parent process will exit here)
    if args.daemon:
        daemonize(args.pid_file)

    # Set up logging after daemonizing (so the daemon process gets the logger)
    setup_logging()

    # Clean up temp files on startup
    cleanup_temp_files()

    run_server(args.port)


if __name__ == "__main__":
    main()
