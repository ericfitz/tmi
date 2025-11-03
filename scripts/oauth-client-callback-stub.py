#!/usr/bin/env python3

# /// script
# dependencies = ["requests>=2.32.0"]
# ///

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

# Global logger instance
logger = None


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

                # Extract token-related parameters (in case server sends tokens directly)
                access_token = query_params.get("access_token", [None])[0]
                refresh_token = query_params.get("refresh_token", [None])[0]
                token_type = query_params.get("token_type", [None])[0]
                expires_in = query_params.get("expires_in", [None])[0]
                
                # Extract additional OAuth parameters that may help identify the user
                login_hint = query_params.get("login_hint", [None])[0]

                # Determine flow type and handle authorization code flow specially
                if code and not access_token:
                    flow_type = "authorization_code"
                    logger.info(
                        "  FLOW TYPE: Authorization Code Flow (code present, no tokens)"
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
                        # Use TMI server's token exchange endpoint to get real tokens
                        try:
                            token_url = "http://localhost:8080/oauth2/token?idp=test"
                            token_data = {
                                "grant_type": "authorization_code",
                                "code": code,
                                "redirect_uri": "http://localhost:8079/"
                            }
                            
                            logger.info(f"  Exchanging authorization code for real tokens via TMI server...")
                            logger.info(f"    Token URL: {token_url}")
                            logger.info(f"    Code: {code}")
                            
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
                
                elif access_token and not code:
                    flow_type = "implicit"
                    logger.info("  FLOW TYPE: Implicit Flow (tokens present, no code)")
                elif access_token and code:
                    flow_type = "mixed"
                    logger.info(
                        "  FLOW TYPE: Mixed Flow (both code and tokens present)"
                    )
                else:
                    flow_type = "unknown"
                    logger.info("  FLOW TYPE: Unknown or incomplete")

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
                        elif flow_type == "implicit":
                            credentials_to_save = {
                                "flow_type": "implicit",
                                "state": state,
                                "access_token": access_token,
                                "refresh_token": refresh_token,
                                "token_type": token_type,
                                "expires_in": expires_in,
                                "tokens_ready": access_token is not None,
                            }
                        elif flow_type == "mixed":
                            credentials_to_save = {
                                "flow_type": "mixed",
                                "code": code,
                                "state": state,
                                "access_token": access_token,
                                "refresh_token": refresh_token,
                                "token_type": token_type,
                                "expires_in": expires_in,
                                "ready_for_token_exchange": code is not None,
                                "tokens_ready": access_token is not None,
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

                # Handle response based on flow type
                if flow_type == "unknown" and not code and not access_token:
                    # Likely implicit flow with tokens in URL fragment
                    # Return HTML page with JavaScript to extract fragment tokens
                    html_response = """<!DOCTYPE html>
<html>
<head>
    <title>OAuth Callback Handler</title>
    <script>
        function handleOAuthCallback() {
            // Extract tokens from URL fragment
            const fragment = window.location.hash.substring(1);
            const params = new URLSearchParams(fragment);
            
            const credentials = {
                access_token: params.get('access_token'),
                refresh_token: params.get('refresh_token'),
                token_type: params.get('token_type'),
                expires_in: params.get('expires_in'),
                state: params.get('state')
            };
            
            if (credentials.access_token) {
                // Send tokens back to server via POST to /oauth-fragment
                fetch('/oauth-fragment', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify(credentials)
                }).then(() => {
                    document.body.innerHTML = '<h1>OAuth tokens processed successfully!</h1><p>You can close this window.</p>';
                }).catch(err => {
                    console.error('Failed to send tokens:', err);
                    document.body.innerHTML = '<h1>Error processing OAuth tokens</h1>';
                });
            } else {
                document.body.innerHTML = '<h1>No OAuth tokens found in URL fragment</h1>';
            }
        }
        
        window.onload = handleOAuthCallback;
    </script>
</head>
<body>
    <h1>Processing OAuth callback...</h1>
    <p>Please wait while we process your authentication.</p>
</body>
</html>"""
                    response_body = html_response.encode('utf-8')
                    self.send_response(200)
                    self.send_header("Content-type", "text/html")
                    self.end_headers()
                    self.wfile.write(response_body)
                else:
                    # Traditional flow with tokens/code in query params
                    response_body = b"Redirect received. Check server logs for details."
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
                elif flow_type == "implicit":
                    # Implicit Flow - client gets tokens directly, no exchange needed
                    response_data = {
                        "flow_type": "implicit",
                        "state": latest_oauth_credentials["state"],
                        "access_token": latest_oauth_credentials["access_token"],
                        "refresh_token": latest_oauth_credentials["refresh_token"],
                        "token_type": latest_oauth_credentials["token_type"],
                        "expires_in": latest_oauth_credentials["expires_in"],
                        "tokens_ready": latest_oauth_credentials["access_token"]
                        is not None,
                    }
                elif flow_type == "mixed":
                    # Mixed flow - return everything
                    response_data = {
                        "flow_type": "mixed",
                        "code": latest_oauth_credentials["code"],
                        "state": latest_oauth_credentials["state"],
                        "access_token": latest_oauth_credentials["access_token"],
                        "refresh_token": latest_oauth_credentials["refresh_token"],
                        "token_type": latest_oauth_credentials["token_type"],
                        "expires_in": latest_oauth_credentials["expires_in"],
                        "ready_for_token_exchange": latest_oauth_credentials["code"]
                        is not None,
                        "tokens_ready": latest_oauth_credentials["access_token"]
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

    def do_POST(self):
        """Handle POST requests from JavaScript fragment token extraction."""
        global latest_oauth_credentials
        
        client_ip = self.client_address[0]
        http_version = self.request_version
        method = "POST"
        
        try:
            # Parse the URL path
            parsed_url = urllib.parse.urlparse(self.path)
            path = parsed_url.path
            
            logger.info(f"INCOMING POST REQUEST: {method} {self.path}")
            logger.info(f"  Path: {path}")
            
            # Handle POST to /oauth-fragment (from JavaScript)
            if path == "/oauth-fragment":
                # Read the JSON payload
                content_length = int(self.headers.get('Content-Length', 0))
                post_data = self.rfile.read(content_length)
                
                try:
                    credentials = json.loads(post_data.decode('utf-8'))
                    logger.info("  Fragment tokens received from JavaScript:")
                    
                    # Extract credentials from JavaScript
                    access_token = credentials.get('access_token')
                    refresh_token = credentials.get('refresh_token') 
                    token_type = credentials.get('token_type')
                    expires_in = credentials.get('expires_in')
                    state = credentials.get('state')
                    
                    logger.info(f"    Access Token: {access_token}")
                    logger.info(f"    Refresh Token: {refresh_token}")
                    logger.info(f"    Token Type: {token_type}")
                    logger.info(f"    Expires In: {expires_in}")
                    logger.info(f"    State: {state}")
                    
                    if access_token:
                        # Store the credentials
                        latest_oauth_credentials.update({
                            "flow_type": "implicit",
                            "code": None,
                            "state": state,
                            "access_token": access_token,
                            "refresh_token": refresh_token,
                            "token_type": token_type,
                            "expires_in": expires_in,
                        })
                        
                        # Extract user ID and save to file
                        user_id = extract_user_id_from_credentials(latest_oauth_credentials)
                        if user_id:
                            credentials_to_save = {
                                "flow_type": "implicit",
                                "state": state,
                                "access_token": access_token,
                                "refresh_token": refresh_token,
                                "token_type": token_type,
                                "expires_in": expires_in,
                                "tokens_ready": True,
                            }
                            save_credentials_to_file(credentials_to_save, user_id)
                            logger.info(f"  Saved credentials for user: {user_id}")
                        
                        # Send success response
                        self.send_response(200)
                        self.send_header("Content-type", "application/json")
                        self.end_headers()
                        response = {"status": "success", "message": "Tokens processed successfully"}
                        self.wfile.write(json.dumps(response).encode())
                        
                        logger.info(f'API request: {client_ip} {method} {self.path} {http_version} 200 "Fragment tokens processed"')
                    else:
                        # No access token found
                        self.send_response(400)
                        self.send_header("Content-type", "application/json")
                        self.end_headers()
                        response = {"status": "error", "message": "No access token found in fragment data"}
                        self.wfile.write(json.dumps(response).encode())
                        
                        logger.info(f'API request: {client_ip} {method} {self.path} {http_version} 400 "No access token"')
                        
                except json.JSONDecodeError as e:
                    # Invalid JSON
                    self.send_response(400)
                    self.send_header("Content-type", "application/json")
                    self.end_headers()
                    response = {"status": "error", "message": f"Invalid JSON: {str(e)}"}
                    self.wfile.write(json.dumps(response).encode())
                    
                    logger.error(f"Invalid JSON in POST data: {str(e)}")
                    logger.info(f'API request: {client_ip} {method} {self.path} {http_version} 400 "Invalid JSON"')
            else:
                # Unknown POST endpoint
                self.send_response(404)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                response = {"status": "error", "message": "Endpoint not found"}
                self.wfile.write(json.dumps(response).encode())
                
                logger.info(f'API request: {client_ip} {method} {self.path} {http_version} 404 "Endpoint not found"')
                
        except Exception as e:
            # Handle any errors during POST request processing
            error_msg = f"Server error: {str(e)}"
            logger.error(f"Error processing POST request: {str(e)}")

            try:
                self.send_response(500)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                response = {"status": "error", "message": error_msg}
                self.wfile.write(json.dumps(response).encode())
                
                logger.info(f'API request: {client_ip} {method} {self.path} {http_version} 500 "{error_msg}"')
            except:
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
