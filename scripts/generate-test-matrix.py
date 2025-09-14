#!/usr/bin/env python3
"""
Generate test matrix from Newman API test results.

This script processes Newman JSON output and creates a matrix showing:
- Endpoints (rows)
- Test cases by return code (columns)
- Pass/Fail/Skip counts in each cell

Usage:
    python3 generate-test-matrix.py <newman-results.json>
"""

import json
import sys
from collections import defaultdict
from urllib.parse import urlparse
import re

def extract_endpoint(method, url_obj):
    """Extract a clean endpoint identifier from method and URL object."""
    try:
        # Handle both string URLs and Newman URL objects
        if isinstance(url_obj, str):
            parsed = urlparse(url_obj)
            path = parsed.path
        else:
            # Newman URL object with path array
            path_parts = url_obj.get('path', [])
            if not path_parts or path_parts == [""]:
                path = "/"
            else:
                path = "/" + "/".join(path_parts)
        
        # Handle special cases first
        if path == "/":
            return f"{method} /"
        elif path == "/oauth2/userinfo":
            return f"{method} /oauth2/userinfo"
        elif path == "/oauth2/providers":
            return f"{method} /oauth2/providers"
        elif path.startswith("/oauth2/authorize"):
            return f"{method} /oauth2/authorize"
        elif path == "/.well-known/openid-configuration":
            return f"{method} /.well-known/openid-configuration"
        elif path == "/.well-known/jwks.json":
            return f"{method} /.well-known/jwks.json"
        elif "/creds" in path:
            return f"{method} /creds"
        
        # Normalize path by replacing UUIDs and IDs with placeholders
        normalized_path = re.sub(r'/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', '/{id}', path)
        normalized_path = re.sub(r'/\d+', '/{id}', normalized_path)
        
        return f"{method} {normalized_path}"
    except:
        return f"{method} {str(url_obj)}"

def extract_test_case_type(test_name):
    """Extract the expected return code from test case name."""
    # Look for patterns like "Success (200)", "Not Found (404)", etc.
    match = re.search(r'\((\d{3})\)', test_name)
    if match:
        return int(match.group(1))
    
    # Map common test patterns to expected codes
    name_lower = test_name.lower()
    if 'success' in name_lower:
        return 200
    elif 'created' in name_lower:
        return 201
    elif 'unauthorized' in name_lower or 'auth' in name_lower:
        return 401
    elif 'forbidden' in name_lower:
        return 403
    elif 'not found' in name_lower:
        return 404
    elif 'bad request' in name_lower or 'invalid' in name_lower:
        return 400
    else:
        return 'Unknown'

def analyze_assertions(assertions):
    """Analyze assertions to count passed, failed, and skipped."""
    passed = 0
    failed = 0
    skipped = 0
    
    for assertion in assertions or []:
        if assertion.get('skipped', False):
            skipped += 1
        elif assertion.get('error'):
            failed += 1
        else:
            passed += 1
    
    return passed, failed, skipped

def main():
    if len(sys.argv) != 2:
        print("Usage: python3 generate-test-matrix.py <newman-results.json>")
        sys.exit(1)
    
    json_file = sys.argv[1]
    
    try:
        with open(json_file, 'r') as f:
            data = json.load(f)
    except FileNotFoundError:
        print(f"Error: File {json_file} not found")
        sys.exit(1)
    except json.JSONDecodeError as e:
        print(f"Error: Invalid JSON in {json_file}: {e}")
        sys.exit(1)
    
    # Extract execution data
    executions = data.get('run', {}).get('executions', [])
    
    # Matrix: endpoint -> expected_code -> {passed, failed, skipped}
    matrix = defaultdict(lambda: defaultdict(lambda: {'passed': 0, 'failed': 0, 'skipped': 0}))
    
    # Track all status codes and endpoints
    all_status_codes = set()
    all_endpoints = set()
    
    for execution in executions:
        # Skip executions without responses (pre-request scripts, etc.)
        if not execution.get('response'):
            continue
            
        # Extract endpoint info
        request = execution.get('request', {})
        method = request.get('method', 'UNKNOWN')
        url_obj = request.get('url', {})
        endpoint = extract_endpoint(method, url_obj)
        
        # Extract test info
        test_name = execution.get('item', {}).get('name', '')
        expected_code = extract_test_case_type(test_name)
        actual_code = execution.get('response', {}).get('code', 0)
        
        # Analyze assertions
        assertions = execution.get('assertions', [])
        passed, failed, skipped = analyze_assertions(assertions)
        
        # Update matrix
        matrix[endpoint][expected_code]['passed'] += passed
        matrix[endpoint][expected_code]['failed'] += failed
        matrix[endpoint][expected_code]['skipped'] += skipped
        
        # Track all codes and endpoints
        all_status_codes.add(expected_code)
        all_endpoints.add(endpoint)
    
    # Sort for consistent output
    sorted_endpoints = sorted(all_endpoints)
    sorted_codes = sorted([c for c in all_status_codes if isinstance(c, int)]) + \
                   sorted([c for c in all_status_codes if not isinstance(c, int)])
    
    # Generate matrix output
    print("# TMI API Test Results Matrix")
    print()
    print("**Test Results by Endpoint and Expected Status Code**")
    print()
    print("Format: Pass/Fail/Skip counts")
    print()
    
    # Header row
    header = "| Endpoint |"
    for code in sorted_codes:
        header += f" {code} |"
    print(header)
    
    # Separator row
    separator = "|" + ("-" * (max(len(endpoint) for endpoint in sorted_endpoints) + 2)) + "|"
    for _ in sorted_codes:
        separator += "---------|"
    print(separator)
    
    # Data rows
    for endpoint in sorted_endpoints:
        row = f"| {endpoint:<{max(len(e) for e in sorted_endpoints)}} |"
        for code in sorted_codes:
            stats = matrix[endpoint][code]
            cell = f" {stats['passed']}/{stats['failed']}/{stats['skipped']} |"
            row += cell
        print(row)
    
    print()
    print("## Summary")
    print()
    
    # Calculate totals
    total_passed = sum(stats['passed'] for endpoint_data in matrix.values() 
                      for stats in endpoint_data.values())
    total_failed = sum(stats['failed'] for endpoint_data in matrix.values() 
                      for stats in endpoint_data.values())
    total_skipped = sum(stats['skipped'] for endpoint_data in matrix.values() 
                       for stats in endpoint_data.values())
    total_tests = total_passed + total_failed + total_skipped
    
    print(f"- **Total Tests**: {total_tests}")
    print(f"- **Passed**: {total_passed} ({total_passed/total_tests*100:.1f}%)")
    print(f"- **Failed**: {total_failed} ({total_failed/total_tests*100:.1f}%)")
    print(f"- **Skipped**: {total_skipped} ({total_skipped/total_tests*100:.1f}%)")
    
    # Identify problematic endpoints
    problem_endpoints = []
    for endpoint, codes in matrix.items():
        total_endpoint_failed = sum(stats['failed'] for stats in codes.values())
        if total_endpoint_failed > 0:
            problem_endpoints.append((endpoint, total_endpoint_failed))
    
    if problem_endpoints:
        print()
        print("## Endpoints with Failures")
        print()
        problem_endpoints.sort(key=lambda x: x[1], reverse=True)
        for endpoint, failures in problem_endpoints:
            print(f"- **{endpoint}**: {failures} failed tests")

if __name__ == "__main__":
    main()