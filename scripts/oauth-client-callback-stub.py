import http.server
import socketserver
import urllib.parse
import sys
import signal
import argparse
import json
import logging
import datetime

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
    "expires_in": None
}

# Global logger instance
logger = None


def setup_logging():
    """Set up dual logging to file and console with RFC3339 timestamps."""
    global logger
    
    # Create logger
    logger = logging.getLogger('oauth_stub')
    logger.setLevel(logging.INFO)
    
    # Clear any existing handlers
    logger.handlers.clear()
    
    # Create custom formatter with RFC3339 timestamp
    class RFC3339Formatter(logging.Formatter):
        def formatTime(self, record, datefmt=None):
            dt = datetime.datetime.fromtimestamp(record.created, tz=datetime.timezone.utc)
            return dt.strftime('%Y-%m-%dT%H:%M:%S.%fZ')[:-3] + 'Z'  # RFC3339 with milliseconds
    
    formatter = RFC3339Formatter('%(asctime)s %(message)s')
    
    # File handler for /tmp/oauth-stub.log
    try:
        file_handler = logging.FileHandler('/tmp/oauth-stub.log')
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

                # Determine flow type and store appropriate credentials
                if code and not access_token:
                    flow_type = "authorization_code"
                    logger.info("  FLOW TYPE: Authorization Code Flow (code present, no tokens)")
                elif access_token and not code:
                    flow_type = "implicit"
                    logger.info("  FLOW TYPE: Implicit Flow (tokens present, no code)")
                elif access_token and code:
                    flow_type = "mixed"
                    logger.info("  FLOW TYPE: Mixed Flow (both code and tokens present)")
                else:
                    flow_type = "unknown"
                    logger.info("  FLOW TYPE: Unknown or incomplete")

                # Store the latest OAuth credentials with flow type
                latest_oauth_credentials.update({
                    "flow_type": flow_type,
                    "code": code,
                    "state": state,
                    "access_token": access_token,
                    "refresh_token": refresh_token,
                    "token_type": token_type,
                    "expires_in": expires_in
                })

                # Enhanced logging
                logger.info(f"OAUTH REDIRECT ANALYSIS:")
                logger.info(f"  Authorization Code: {code}")
                logger.info(f"  State: {state}")
                logger.info(f"  Access Token: {access_token}")
                logger.info(f"  Refresh Token: {refresh_token}")
                logger.info(f"  Token Type: {token_type}")
                logger.info(f"  Expires In: {expires_in}")
                logger.info(f"  Flow Type: {flow_type}")

                # Send a simple response to the client
                response_body = b"Redirect received. Check server logs for details."
                self.send_response(200)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(response_body)
                
                # Log API request
                logger.info(f"API request: {client_ip} {method} {self.path} {http_version} 200 \"Redirect received. Check server logs for details.\"")

                # Check if code is 'exit' to trigger graceful shutdown
                if code == "exit":
                    logger.info("Received 'exit' in code parameter, shutting down gracefully...")
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
                        "ready_for_token_exchange": latest_oauth_credentials["code"] is not None
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
                        "tokens_ready": latest_oauth_credentials["access_token"] is not None
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
                        "ready_for_token_exchange": latest_oauth_credentials["code"] is not None,
                        "tokens_ready": latest_oauth_credentials["access_token"] is not None
                    }
                else:
                    # Unknown or no data yet
                    response_data = {
                        "flow_type": flow_type or "none",
                        "error": "No OAuth data received yet" if not flow_type else "Unknown flow type",
                        "raw_data": latest_oauth_credentials
                    }

                # Send JSON response
                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                
                response_json = json.dumps(response_data, indent=2)
                self.wfile.write(response_json.encode())
                
                # Log API request with JSON payload (truncated for readability)
                summary = {"flow_type": response_data.get("flow_type"), "has_tokens": bool(response_data.get("access_token")), "has_code": bool(response_data.get("code"))}
                logger.info(f"API request: {client_ip} {method} {self.path} {http_version} 200 {json.dumps(summary)}")
                logger.info(f"Full response: {response_json}")

            # Unknown route
            else:
                error_msg = f"Not Found: {path}"
                self.send_response(404)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(error_msg.encode())
                
                # Log API request
                logger.info(f"API request: {client_ip} {method} {self.path} {http_version} 404 \"{error_msg}\"")

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
                logger.info(f"API request: {client_ip} {method} {self.path} {http_version} 500 \"{error_msg}\"")
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
        sys.exit(0)

    except KeyboardInterrupt:
        logger.info("Received KeyboardInterrupt, shutting down gracefully...")
        server.server_close()
        sys.exit(0)
    except Exception as e:
        logger.error(f"Server error: {str(e)}")
        sys.exit(1)


def main():
    """Parse command-line arguments and start the server."""
    # Set up logging before doing anything else
    setup_logging()
    
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
