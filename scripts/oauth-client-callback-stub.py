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
latest_oauth_credentials = {"code": None, "state": None}

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

            # Route 1: OAuth callback endpoint (/)
            if path == "/":
                # Extract 'code' and 'state' parameters
                code = query_params.get("code", [None])[0]
                state = query_params.get("state", [None])[0]

                # Store the latest OAuth credentials
                latest_oauth_credentials["code"] = code
                latest_oauth_credentials["state"] = state

                # Log OAuth redirect details
                logger.info(f"Received OAuth redirect: Code={code}, State={state}")

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
                # Send JSON response with latest OAuth credentials
                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                
                response_data = json.dumps(latest_oauth_credentials)
                self.wfile.write(response_data.encode())
                
                # Log API request with JSON payload
                logger.info(f"API request: {client_ip} {method} {self.path} {http_version} 200 {response_data}")

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
                # If we can't even send an error response, just log and exit
                logger.error("Failed to send error response to client")
            
            sys.exit(1)


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
