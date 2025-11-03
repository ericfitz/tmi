#!/usr/bin/env python3
# /// script
# dependencies = ["click>=8.3.0"]
# ///

import json
import click
from pathlib import Path


def add_tests_to_request(request_item):
    """Add appropriate tests to a Postman request item based on its method and expected responses."""
    
    # Extract method and URL info
    method = request_item.get("request", {}).get("method", "GET")
    name = request_item.get("name", "Unknown")
    url = request_item.get("request", {}).get("url", {})
    
    # Check if this is a public endpoint
    is_public_endpoint = False
    if isinstance(url, dict):
        path_parts = url.get("path", [])
        path = "/" + "/".join(path_parts) if path_parts else "/"
        
        # List of public endpoints that don't require authentication
        public_paths = [
            "/",
            "/oauth2/providers",
            "/oauth2/authorize",
            "/oauth2/token",
            "/oauth2/refresh",
            "/.well-known/openid-configuration",
            "/.well-known/oauth-authorization-server",
            "/.well-known/jwks.json"
        ]
        
        is_public_endpoint = any(path.startswith(p) for p in public_paths)
    
    # Initialize event array if not present
    if "event" not in request_item:
        request_item["event"] = []
    
    # Check if tests already exist
    test_event = next((event for event in request_item["event"] if event.get("listen") == "test"), None)
    
    if test_event is None:
        # Create new test event
        test_event = {
            "listen": "test",
            "script": {
                "exec": [],
                "type": "text/javascript"
            }
        }
        request_item["event"].append(test_event)
    
    # Get responses to determine expected status codes
    responses = request_item.get("response", [])
    expected_statuses = []
    for response in responses:
        if "code" in response:
            expected_statuses.append(response["code"])
    
    # Default expected status based on method if no responses defined
    if not expected_statuses:
        if method == "GET":
            expected_statuses = [200]
        elif method == "POST":
            expected_statuses = [201, 200]
        elif method == "PUT" or method == "PATCH":
            expected_statuses = [200, 204]
        elif method == "DELETE":
            expected_statuses = [204, 200]
    
    # Build test scripts
    test_scripts = []
    
    # Test 1: Status code validation
    if expected_statuses:
        status_list = ", ".join(str(s) for s in expected_statuses)
        test_scripts.append(f'pm.test("Status code is valid", function () {{')
        test_scripts.append(f'    pm.expect(pm.response.code).to.be.oneOf([{status_list}]);')
        test_scripts.append('});')
    
    # Test 2: Response time
    test_scripts.append('')
    test_scripts.append('pm.test("Response time is less than 1000ms", function () {')
    test_scripts.append('    pm.expect(pm.response.responseTime).to.be.below(1000);')
    test_scripts.append('});')
    
    # Test 3: Content-Type header for non-DELETE requests
    if method != "DELETE":
        test_scripts.append('')
        test_scripts.append('pm.test("Response has Content-Type header", function () {')
        test_scripts.append('    pm.response.to.have.header("Content-Type");')
        test_scripts.append('});')
    
    # Test 4: JSON response validation (for requests expecting JSON)
    if any(response.get("header", []) for response in responses):
        has_json_response = any(
            h.get("value", "").startswith("application/json") 
            for response in responses 
            for h in response.get("header", []) 
            if h.get("key") == "Content-Type"
        )
        if has_json_response:
            test_scripts.append('')
            test_scripts.append('pm.test("Response is valid JSON", function () {')
            test_scripts.append('    pm.response.to.be.json;')
            test_scripts.append('});')
    
    # Test 5: Specific tests based on endpoint type
    if "/oauth2/" in name.lower() or "oauth" in name.lower():
        # OAuth-specific tests
        test_scripts.append('')
        test_scripts.append('// OAuth endpoint specific tests')
        if method == "POST" and "token" in name.lower():
            test_scripts.append('pm.test("Token response contains required fields", function () {')
            test_scripts.append('    if (pm.response.code === 200) {')
            test_scripts.append('        const jsonData = pm.response.json();')
            test_scripts.append('        pm.expect(jsonData).to.have.property("access_token");')
            test_scripts.append('        pm.expect(jsonData).to.have.property("token_type");')
            test_scripts.append('    }')
            test_scripts.append('});')
    
    elif "threat_model" in name.lower() or "diagram" in name.lower():
        # Domain object tests
        test_scripts.append('')
        test_scripts.append('// Domain object specific tests')
        if method == "GET" and "list" not in name.lower():
            test_scripts.append('pm.test("Response contains ID field", function () {')
            test_scripts.append('    if (pm.response.code === 200) {')
            test_scripts.append('        const jsonData = pm.response.json();')
            test_scripts.append('        pm.expect(jsonData).to.have.property("id");')
            test_scripts.append('    }')
            test_scripts.append('});')
        elif method == "GET" and "list" in name.lower():
            test_scripts.append('pm.test("Response is an array", function () {')
            test_scripts.append('    if (pm.response.code === 200) {')
            test_scripts.append('        const jsonData = pm.response.json();')
            test_scripts.append('        pm.expect(jsonData).to.be.an("array");')
            test_scripts.append('    }')
            test_scripts.append('});')
    
    # Test 6: Error response structure validation
    if any(r.get("code", 0) >= 400 for r in responses):
        test_scripts.append('')
        test_scripts.append('pm.test("Error response has proper structure", function () {')
        test_scripts.append('    if (pm.response.code >= 400) {')
        test_scripts.append('        const jsonData = pm.response.json();')
        test_scripts.append('        pm.expect(jsonData).to.have.property("error");')
        test_scripts.append('    }')
        test_scripts.append('});')
    
    # Test 7: Security headers
    test_scripts.append('')
    test_scripts.append('pm.test("Response has X-Content-Type-Options header", function () {')
    test_scripts.append('    pm.expect(pm.response.headers.get("X-Content-Type-Options")).to.equal("nosniff");')
    test_scripts.append('});')
    
    test_scripts.append('')
    test_scripts.append('pm.test("Response has X-Frame-Options header", function () {')
    test_scripts.append('    pm.expect(pm.response.headers.get("X-Frame-Options")).to.equal("DENY");')
    test_scripts.append('});')
    
    test_scripts.append('')
    test_scripts.append('pm.test("Response has X-XSS-Protection header", function () {')
    test_scripts.append('    pm.expect(pm.response.headers.get("X-XSS-Protection")).to.equal("1; mode=block");')
    test_scripts.append('});')
    
    test_scripts.append('')
    test_scripts.append('pm.test("Response has Content-Security-Policy header", function () {')
    test_scripts.append('    pm.expect(pm.response.headers.get("Content-Security-Policy")).to.exist;')
    test_scripts.append('    pm.expect(pm.response.headers.get("Content-Security-Policy")).to.include("default-src");')
    test_scripts.append('});')
    
    test_scripts.append('')
    test_scripts.append('pm.test("Response has Referrer-Policy header", function () {')
    test_scripts.append('    pm.expect(pm.response.headers.get("Referrer-Policy")).to.equal("strict-origin-when-cross-origin");')
    test_scripts.append('});')
    
    test_scripts.append('')
    test_scripts.append('pm.test("Response has Permissions-Policy header", function () {')
    test_scripts.append('    pm.expect(pm.response.headers.get("Permissions-Policy")).to.exist;')
    test_scripts.append('    pm.expect(pm.response.headers.get("Permissions-Policy")).to.include("geolocation=()");')
    test_scripts.append('});')
    
    # Add bearer token test for authenticated endpoints (skip for public endpoints)
    if not is_public_endpoint:
        test_scripts.append('')
        test_scripts.append('// Authentication test for protected endpoints')
        test_scripts.append('pm.test("Request includes Authorization header", function () {')
        test_scripts.append('    const authHeader = pm.request.headers.get("Authorization");')
        test_scripts.append('    pm.expect(authHeader).to.exist;')
        test_scripts.append('    pm.expect(authHeader).to.include("Bearer");')
        test_scripts.append('});')
    
    # Update the test script
    test_event["script"]["exec"] = test_scripts


def process_collection_items(items):
    """Recursively process collection items and add tests."""
    for item in items:
        if "item" in item:
            # This is a folder, recurse into it
            process_collection_items(item["item"])
        elif "request" in item:
            # This is a request, add tests
            add_tests_to_request(item)


@click.command()
@click.argument('input_file', type=click.Path(exists=True))
@click.option('--output', '-o', type=click.Path(), help='Output file path (defaults to input file)')
def main(input_file, output):
    """Add unit tests to a Postman collection."""
    
    # Read the collection
    with open(input_file, 'r') as f:
        collection = json.load(f)
    
    # Process all items in the collection
    if "item" in collection:
        process_collection_items(collection["item"])
    
    # Determine output file
    if output is None:
        output = input_file
    
    # Write the updated collection
    with open(output, 'w') as f:
        json.dump(collection, f, indent=2)
    
    click.echo(f"Successfully added tests to {input_file}")
    if output != input_file:
        click.echo(f"Output written to {output}")


if __name__ == "__main__":
    main()