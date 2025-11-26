#!/usr/bin/env python3

# /// script
# dependencies = ["requests>=2.32.0"]
# ///

"""
OAuth Client Callback Stub - Development Testing Tool for PKCE

This stub handles OAuth 2.0 Authorization Code Flow with PKCE. It generates
PKCE parameters (code_verifier and code_challenge), initiates the OAuth flow,
receives the authorization code, and exchanges it for tokens using the code_verifier.

PKCE Flow:
1. Generate code_verifier (cryptographically random string)
2. Compute code_challenge = BASE64URL(SHA256(code_verifier))
3. Send code_challenge to authorization endpoint
4. Receive authorization code
5. Exchange code + code_verifier for tokens at token endpoint

Usage: make start-oauth-stub
API: GET /creds?userid=<user> to retrieve saved credentials
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
import time
import uuid
import requests
import hashlib
import secrets

# Global flag to control server shutdown
should_exit = False

# Global storage for latest OAuth credentials
latest_oauth_credentials = {
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

# Global logger instance
logger = None


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
        verifier = base64.urlsafe_b64encode(verifier_bytes).decode('utf-8').rstrip('=')
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
        digest = hashlib.sha256(verifier.encode('utf-8')).digest()
        # Encode as base64url without padding
        challenge = base64.urlsafe_b64encode(digest).decode('utf-8').rstrip('=')
        return challenge


def setup_logging():
    """Set up dual logging to file and console with RFC3339 timestamps."""
    global logger

    # Create logger
    logger = logging.getLogger("oauth_stub")
    logger.setLevel(logging.INFO)

    # Clear any existing handlers
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
    global should_exit
    logger.info("Received SIGTERM, shutting down gracefully...")
    cleanup_temp_files()
    should_exit = True


class OAuthRedirectHandler(http.server.BaseHTTPRequestHandler):
    """Custom handler for OAuth redirect requests."""

    def log_message(self, format, *args):
        """Override default logging to prevent duplicate logs."""
        # Suppress the default HTTP server logs since we'll do our own structured logging
        pass

    def do_GET(self):
        """Handle GET requests to the redirect URI and API endpoints."""
        global should_exit, latest_oauth_credentials

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

                # Extract additional OAuth parameters that may help identify the user
                login_hint = query_params.get("login_hint", [None])[0]

                # TMI only supports Authorization Code Flow with PKCE
                if code:
                    flow_type = "authorization_code"
                    logger.info(
                        "  FLOW TYPE: Authorization Code Flow with PKCE"
                    )
                    
                    # For authorization code flow, generate access tokens for testing
                    # Extract user info from login_hint or create a default user from the authorization code
                    user_id = None
                    login_hint_user = None
                    
                    if login_hint and login_hint not in ["exit"]:
                        # Validate login_hint format
                        if re.match(r"^[a-zA-Z0-9-]{3,20}$", login_hint):
                            user_id = f"{login_hint}@test.tmi"
                            login_hint_user = login_hint
                    
                    # If no login_hint, use default user for testing
                    if not user_id and code:
                        # For API testing, use postman-user as the default user ID
                        # This ensures consistency with test collection expectations
                        login_hint_user = "postman-user"
                        user_id = f"{login_hint_user}@test.tmi"
                        logger.info(f"  Using default test user ID: {user_id}")
                    
                    # If we have a valid user_id, exchange the code for real tokens using TMI server
                    if user_id and login_hint_user and code:
                        # Use TMI server's token exchange endpoint to get real tokens with PKCE
                        try:
                            # Retrieve code_verifier for this state (if it exists)
                            # If not found, generate a new one for testing
                            code_verifier = pkce_verifiers.get(state)
                            if not code_verifier:
                                logger.warning(f"  No PKCE verifier found for state {state}, generating new one for testing")
                                code_verifier = PKCEHelper.generate_code_verifier()

                            token_url = "http://localhost:8080/oauth2/token?idp=test"
                            token_data = {
                                "grant_type": "authorization_code",
                                "code": code,
                                "code_verifier": code_verifier,
                                "redirect_uri": "http://localhost:8079/"
                            }

                            logger.info(f"  Exchanging authorization code for real tokens via TMI server (PKCE)...")
                            logger.info(f"    Token URL: {token_url}")
                            logger.info(f"    Code: {code}")
                            logger.info(f"    Code Verifier: {code_verifier[:20]}... (length: {len(code_verifier)})")

                            # Make the token exchange request to TMI server
                            response = requests.post(
                                token_url,
                                json=token_data,
                                headers={"Content-Type": "application/json"},
                                timeout=10
                            )
                            
                            if response.status_code == 200:
                                token_response = response.json()
                                access_token = token_response.get("access_token")
                                refresh_token = token_response.get("refresh_token") 
                                token_type = token_response.get("token_type", "Bearer")
                                expires_in = str(token_response.get("expires_in", 3600))
                                
                                logger.info(f"  Successfully exchanged code for real tokens:")
                                logger.info(f"    Access Token: {access_token[:50] if access_token else 'None'}...")
                                logger.info(f"    Refresh Token: {refresh_token}")
                                logger.info(f"    Token Type: {token_type}")
                                logger.info(f"    Expires In: {expires_in}s")
                            else:
                                logger.error(f"  Token exchange failed: {response.status_code} - {response.text}")
                                # Fall back to storing just the code for client to handle
                                access_token = None
                                refresh_token = None
                                token_type = "Bearer"
                                expires_in = "3600"
                                
                        except Exception as e:
                            logger.error(f"  Failed to exchange authorization code: {e}")
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
                logger.info(f"OAUTH REDIRECT ANALYSIS:")
                logger.info(f"  Authorization Code: {code}")
                logger.info(f"  State: {state}")
                logger.info(f"  Access Token: {access_token}")
                logger.info(f"  Refresh Token: {refresh_token}")
                logger.info(f"  Token Type: {token_type}")
                logger.info(f"  Expires In: {expires_in}")
                logger.info(f"  Flow Type: {flow_type}")

                # Send simple response - authorization code flow always uses query params
                response_body = b"OAuth callback received. Check server logs for details."
                self.send_response(200)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(response_body)

                # Log API request
                logger.info(
                    f'API request: {client_ip} {method} {self.path} {http_version} 200 "Redirect received. Check server logs for details."'
                )

                # Check if code is 'exit' to trigger graceful shutdown
                if code == "exit":
                    logger.info(
                        "Received 'exit' in code parameter, shutting down gracefully..."
                    )
                    cleanup_temp_files()
                    should_exit = True

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

            # Route 3: API endpoint to retrieve credentials for specific user (/creds)
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
                complete_user_id = f"{userid_part}@test.tmi"
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

                # Return credentials
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
            except:
                # If we can't even send an error response, just log it
                logger.error("Failed to send error response to client")



def run_server(port):
    """Run the HTTP server on the specified port."""
    try:
        # Set up the server with the custom handler, binding to localhost
        server = socketserver.TCPServer(("localhost", port), OAuthRedirectHandler)
        logger.info(f"Server listening on http://localhost:{port}/...")

        # Handle SIGTERM for graceful shutdown
        signal.signal(signal.SIGTERM, signal_handler)

        # Serve until shutdown is requested
        while not should_exit:
            server.handle_request()

        # Close the server
        server.server_close()
        logger.info("Server has shut down.")
        cleanup_temp_files()
        sys.exit(0)

    except KeyboardInterrupt:
        logger.info("Received KeyboardInterrupt, shutting down gracefully...")
        server.server_close()
        cleanup_temp_files()
        sys.exit(0)
    except Exception as e:
        logger.error(f"Server error: {str(e)}")
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
    # For TMI test provider, the user ID would typically be in the access token or state
    # Since we don't decode JWTs here, we'll look for patterns in the state or other fields

    # Try to extract from state parameter (common pattern: contains user info)
    state = credentials.get("state")
    if state:
        # Look for email patterns in state - TMI includes login_hint in state
        email_match = re.search(
            r"([a-zA-Z0-9][a-zA-Z0-9-]{1,18}[a-zA-Z0-9])@test\.tmi", state
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
                return f"{login_hint}@test.tmi"
        except:
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
                if email and email.endswith("@test.tmi"):
                    return email
        except:
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


def main():
    """Parse command-line arguments and start the server."""
    # Set up logging before doing anything else
    setup_logging()

    # Clean up temp files on startup
    cleanup_temp_files()

    parser = argparse.ArgumentParser(description="OAuth Redirect URI Receiver")
    parser.add_argument(
        "--port", type=int, default=8079, help="Port to listen on (default: 8079)"
    )
    args = parser.parse_args()

    if args.port < 1 or args.port > 65535:
        logger.error(f"Port {args.port} is invalid. Must be between 1 and 65535.")
        sys.exit(1)

    run_server(args.port)


if __name__ == "__main__":
    main()
