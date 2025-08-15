#!/usr/bin/env python3
# /// script
# dependencies = [
#   "requests",
# ]
# ///
"""
TMI REST API Testing Tool

A simple tool for ad-hoc testing of TMI REST APIs with a human-readable script format.

Usage:
    uv run scripts/api_test.py test_script.txt

Example script:
    # Configure server
    server localhost
    port 8080
    usetls false
    
    # Authenticate user
    auth user1
    
    # Make requests
    request createtm post /threat_models $user1.jwt$ body={"name":"Test Model","description":"Test"}
    request getmodels get /threat_models $user1.jwt$
    
    # Test expectations  
    expect $createtm.status$ == 201
    expect $createtm.body.id$ exists
    expect $getmodels.status$ == 200
"""

import sys
import json
import re
import subprocess
import requests
from dataclasses import dataclass, field
from typing import Dict, Any, Optional, Union
from urllib.parse import urljoin
import os


@dataclass
class AuthUser:
    """Represents an authenticated user"""
    username: str
    jwt: str


@dataclass 
class ApiResponse:
    """Represents an API response"""
    status: int
    statustext: str
    body: Any
    headers: Dict[str, str]


@dataclass
class TestConfig:
    """Test configuration"""
    server: str = "localhost"
    port: int = 8080
    use_tls: bool = False
    base_url: str = field(init=False)
    
    def __post_init__(self):
        protocol = "https" if self.use_tls else "http"
        self.base_url = f"{protocol}://{self.server}:{self.port}"


class ApiTester:
    """Main API testing class"""
    
    def __init__(self):
        self.config = TestConfig()
        self.users: Dict[str, AuthUser] = {}
        self.responses: Dict[str, ApiResponse] = {}
        self.variables: Dict[str, Any] = {}
        
    def parse_and_run(self, script_path: str) -> bool:
        """Parse and run a test script. Returns True if all tests pass."""
        try:
            with open(script_path, 'r') as f:
                lines = f.readlines()
            
            print(f"Running test script: {script_path}")
            print("=" * 50)
            
            passed = 0
            failed = 0
            
            for line_num, line in enumerate(lines, 1):
                line = line.strip()
                
                # Skip empty lines and comments
                if not line or line.startswith('#'):
                    continue
                    
                try:
                    if self._process_line(line):
                        continue  # Configuration or setup command
                    else:
                        # This was an expect statement
                        passed += 1
                        print(f"✅ Line {line_num}: PASS")
                        
                except AssertionError as e:
                    failed += 1
                    print(f"❌ Line {line_num}: FAIL - {e}")
                    
                except Exception as e:
                    failed += 1
                    print(f"💥 Line {line_num}: ERROR - {e}")
                    
            print("=" * 50)
            print(f"Results: {passed} passed, {failed} failed")
            return failed == 0
            
        except FileNotFoundError:
            print(f"Error: Script file '{script_path}' not found")
            return False
        except Exception as e:
            print(f"Error running script: {e}")
            return False
    
    def _process_line(self, line: str) -> bool:
        """Process a single line. Returns True if it's a config/action, False if it's an expect."""
        parts = line.split(None, 1)
        if not parts:
            return True
            
        command = parts[0].lower()
        args = parts[1] if len(parts) > 1 else ""
        
        if command == "server":
            self.config.server = args.strip()
            self._update_base_url()
            
        elif command == "port":
            self.config.port = int(args.strip())
            self._update_base_url()
            
        elif command == "usetls":
            self.config.use_tls = args.strip().lower() in ('true', '1', 'yes')
            self._update_base_url()
            
        elif command == "auth":
            self._authenticate_user(args.strip())
            
        elif command == "request":
            self._make_request(args)
            
        elif command == "expect":
            self._check_expectation(args)
            return False  # This was a test
            
        else:
            raise ValueError(f"Unknown command: {command}")
            
        return True
    
    def _update_base_url(self):
        """Update base URL after configuration changes"""
        protocol = "https" if self.config.use_tls else "http"
        self.config.base_url = f"{protocol}://{self.config.server}:{self.config.port}"
    
    def _authenticate_user(self, username: str):
        """Authenticate a user using make test-api auth=only"""
        print(f"🔐 Authenticating user: {username}")
        
        try:
            # Run make test-api auth=only to get JWT token
            result = subprocess.run(
                ["make", "test-api", "auth=only"], 
                capture_output=True, 
                text=True,
                cwd=os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
            )
            
            if result.returncode != 0:
                raise Exception(f"Authentication failed: {result.stderr}")
                
            # Extract token from output (look for lines after "✅ Token:")
            lines = result.stdout.split('\n')
            token = None
            found_token_marker = False
            for line in lines:
                line = line.strip()
                if "✅ Token:" in line:
                    found_token_marker = True
                    continue
                elif found_token_marker and line.startswith('eyJ'):  # JWT tokens start with eyJ
                    token = line
                    break
                    
            if not token:
                raise Exception("No JWT token found in authentication output")
                
            # Extract username from token payload (for display purposes)
            # This is just for user feedback, not security
            import base64
            try:
                payload = json.loads(base64.b64decode(token.split('.')[1] + '=='))
                actual_username = payload.get('email', payload.get('sub', username))
            except:
                actual_username = username
                
            self.users[username] = AuthUser(username=actual_username, jwt=token)
            print(f"✅ Authenticated {username} -> {actual_username}")
            
        except Exception as e:
            raise Exception(f"Failed to authenticate user {username}: {e}")
    
    def _make_request(self, args: str):
        """Make an HTTP request"""
        # Parse: reqname method url [jwt] [body=...]
        # Use regex to properly handle JSON body
        import re
        match = re.match(r'^(\S+)\s+(\S+)\s+(\S+)\s*(.*?)(?:\s+body=(.+?))?$', args.strip())
        if not match:
            raise ValueError("Request format: reqname method url [jwt] [body=...]")
            
        req_name = match.group(1)
        method = match.group(2).upper()
        url = match.group(3)
        middle_args = match.group(4).strip() if match.group(4) else ""
        body_str = match.group(5) if match.group(5) else None
        
        # Handle optional JWT and body
        jwt_token = None
        body = None
        
        # Parse JWT token from middle args
        if middle_args:
            jwt_token = self._resolve_variable(middle_args)
        
        # Parse body if present
        if body_str:
            try:
                body = json.loads(body_str)
            except json.JSONDecodeError:
                raise ValueError(f"Invalid JSON in body: {body_str}")
        
        # Resolve variables in URL
        original_url = url
        url = self._resolve_variable(url)
        if '$' in original_url and original_url != url:
            print(f"   URL resolved: {original_url} → {url}")
        
        # Build full URL
        if not url.startswith('http'):
            full_url = urljoin(self.config.base_url, url.lstrip('/'))
        else:
            full_url = url
            
        # Prepare headers
        headers = {'Content-Type': 'application/json'}
        if jwt_token:
            headers['Authorization'] = f'Bearer {jwt_token}'
            
        print(f"🌐 {method} {full_url}")
        
        try:
            # Make request
            print(f"   Debug: Making {method} request to {full_url}")
            print(f"   Debug: Headers = {headers}")
            if body:
                print(f"   Debug: Body = {body}")
            
            response = requests.request(
                method=method,
                url=full_url,
                headers=headers,
                json=body,
                timeout=30
            )
            
            print(f"   Debug: Raw response status = {response.status_code}")
            print(f"   Debug: Raw response headers = {dict(response.headers)}")
            
            # Parse response body
            try:
                response_body = response.json()
            except json.JSONDecodeError:
                response_body = response.text
                
            # Store response
            api_response = ApiResponse(
                status=response.status_code,
                statustext=response.reason,
                body=response_body,
                headers=dict(response.headers)
            )
            
            self.responses[req_name] = api_response
            print(f"   → {response.status_code} {response.reason}")
            if response.status_code >= 400:
                print(f"   Response body: {response_body}")
            
        except requests.RequestException as e:
            raise Exception(f"Request failed: {e}")
    
    def _check_expectation(self, args: str):
        """Check an expectation"""
        # Parse: expression operator value
        parts = args.split(None, 2)
        if len(parts) < 2:
            raise ValueError("Expect format: expression operator [value]")
            
        expression = parts[0]
        operator = parts[1]
        expected_value = parts[2] if len(parts) > 2 else None
        
        # Resolve the expression
        actual_value = self._resolve_variable(expression)
        
        # Check the expectation
        if operator == "exists":
            if actual_value is None:
                raise AssertionError(f"{expression} does not exist")
                
        elif operator == "==":
            if expected_value is None:
                raise ValueError("== operator requires a value")
            expected = self._resolve_variable(expected_value)
            if str(actual_value) != str(expected):
                raise AssertionError(f"{expression} = {actual_value}, expected {expected}")
                
        elif operator == "!=":
            if expected_value is None:
                raise ValueError("!= operator requires a value")
            expected = self._resolve_variable(expected_value)
            if str(actual_value) == str(expected):
                raise AssertionError(f"{expression} = {actual_value}, should not equal {expected}")
                
        elif operator == ">":
            if expected_value is None:
                raise ValueError("> operator requires a value")
            expected = float(self._resolve_variable(expected_value))
            actual = float(actual_value)
            if actual <= expected:
                raise AssertionError(f"{expression} = {actual}, expected > {expected}")
                
        elif operator == "<":
            if expected_value is None:
                raise ValueError("< operator requires a value")
            expected = float(self._resolve_variable(expected_value))
            actual = float(actual_value)  
            if actual >= expected:
                raise AssertionError(f"{expression} = {actual}, expected < {expected}")
                
        else:
            raise ValueError(f"Unknown operator: {operator}")
    
    def _resolve_variable(self, expression: str) -> Any:
        """Resolve variables in expressions like $user1.jwt$ or $createtm.body.id$"""
        # If it's a string that might contain variables, resolve them
        if isinstance(expression, str) and '$' in expression:
            import re
            def replace_var(match):
                var_expr = match.group(0)  # Full match including $...$
                return str(self._resolve_single_variable(var_expr))
            
            # Replace all $variable$ patterns in the string
            return re.sub(r'\$[^$]+\$', replace_var, expression)
        
        # For simple expressions that are just a single variable
        if expression.startswith('$') and expression.endswith('$'):
            return self._resolve_single_variable(expression)
        
        # Try to parse as JSON/number/boolean, otherwise return as string
        if expression.lower() in ('true', 'false'):
            return expression.lower() == 'true'
        try:
            return json.loads(expression)
        except (json.JSONDecodeError, ValueError):
            try:
                return float(expression)
            except ValueError:
                return expression

    def _resolve_single_variable(self, expression: str) -> Any:
        """Resolve a single variable like $user1.jwt$ or $createtm.body.id$"""
        if not expression.startswith('$') or not expression.endswith('$'):
            return expression
        
        # Remove $ markers
        var_path = expression[1:-1]
        
        # Split on dots to navigate object hierarchy
        parts = var_path.split('.')
        
        # Get root object
        root_name = parts[0]
        if root_name in self.users:
            current = self.users[root_name]
        elif root_name in self.responses:
            current = self.responses[root_name]
        else:
            raise ValueError(f"Unknown variable: {root_name}")
        
        # Navigate through the path
        for part in parts[1:]:
            if hasattr(current, part):
                current = getattr(current, part)
            elif isinstance(current, dict) and part in current:
                current = current[part]
            else:
                return None  # Path doesn't exist
                
        return current


def main():
    if len(sys.argv) != 2:
        print("Usage: python3 scripts/api_test.py <test_script>")
        print()
        print("Example script format:")
        print("    server localhost")
        print("    port 8080")  
        print("    auth user1")
        print("    request createtm post /threat_models $user1.jwt$ body={\"name\":\"Test\"}")
        print("    expect $createtm.status$ == 201")
        sys.exit(1)
    
    script_path = sys.argv[1]
    tester = ApiTester()
    
    success = tester.parse_and_run(script_path)
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()