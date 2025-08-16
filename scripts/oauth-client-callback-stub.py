import http.server
import socketserver
import urllib.parse
import sys
import signal
import argparse
import json

# Global flag to control server shutdown
should_exit = False

# Global storage for latest OAuth credentials
latest_oauth_credentials = {"code": None, "state": None}


def signal_handler(sig, frame):
    """Handle SIGTERM for graceful shutdown."""
    global should_exit
    print("Received SIGTERM, shutting down gracefully...")
    should_exit = True


class OAuthRedirectHandler(http.server.BaseHTTPRequestHandler):
    """Custom handler for OAuth redirect requests."""

    def do_GET(self):
        """Handle GET requests to the redirect URI and API endpoints."""
        global should_exit, latest_oauth_credentials
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

                # Print the extracted values
                print(f"Received OAuth redirect:")
                print(f"  Code: {code}")
                print(f"  State: {state}")

                # Send a simple response to the client
                self.send_response(200)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(b"Redirect received. Check server logs for details.")

                # Check if code is 'exit' to trigger graceful shutdown
                if code == "exit":
                    print("Received 'exit' in code parameter, shutting down gracefully...")
                    should_exit = True

            # Route 2: API endpoint to retrieve latest OAuth credentials (/latest)
            elif path == "/latest":
                print(f"API request for latest OAuth credentials:")
                print(f"  Returning code: {latest_oauth_credentials['code']}")
                print(f"  Returning state: {latest_oauth_credentials['state']}")

                # Send JSON response with latest OAuth credentials
                self.send_response(200)
                self.send_header("Content-type", "application/json")
                self.end_headers()
                
                response_data = json.dumps(latest_oauth_credentials)
                self.wfile.write(response_data.encode())

            # Unknown route
            else:
                self.send_response(404)
                self.send_header("Content-type", "text/plain")
                self.end_headers()
                self.wfile.write(f"Not Found: {path}".encode())

        except Exception as e:
            # Handle any errors during request processing
            print(f"Error processing request: {str(e)}")
            self.send_response(500)
            self.send_header("Content-type", "text/plain")
            self.end_headers()
            self.wfile.write(f"Server error: {str(e)}".encode())
            sys.exit(1)


def run_server(port):
    """Run the HTTP server on the specified port."""
    try:
        # Set up the server with the custom handler, binding to localhost
        server = socketserver.TCPServer(("localhost", port), OAuthRedirectHandler)
        print(f"Server listening on http://localhost:{port}/...")

        # Handle SIGTERM for graceful shutdown
        signal.signal(signal.SIGTERM, signal_handler)

        # Serve until shutdown is requested
        while not should_exit:
            server.handle_request()

        # Close the server
        server.server_close()
        print("Server has shut down.")
        sys.exit(0)

    except KeyboardInterrupt:
        print("Received KeyboardInterrupt, shutting down gracefully...")
        server.server_close()
        sys.exit(0)
    except Exception as e:
        print(f"Server error: {str(e)}")
        sys.exit(1)


def main():
    """Parse command-line arguments and start the server."""
    parser = argparse.ArgumentParser(description="OAuth Redirect URI Receiver")
    parser.add_argument(
        "--port", type=int, default=8079, help="Port to listen on (default: 8079)"
    )
    args = parser.parse_args()

    if args.port < 1 or args.port > 65535:
        print(f"Error: Port {args.port} is invalid. Must be between 1 and 65535.")
        sys.exit(1)

    run_server(args.port)


if __name__ == "__main__":
    main()
