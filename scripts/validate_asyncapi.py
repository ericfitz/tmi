#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = [
#     "pydantic>=2.12.0",
#     "pyyaml>=6.0",
#     "jsonschema>=4.25.0",
# ]
# ///
"""
AsyncAPI YAML validation script using Pydantic and JSON Schema.

This script validates our TMI AsyncAPI specification against AsyncAPI 3.0.0 schema.
Run with: uv run validate_asyncapi.py tmi-asyncapi.yml
"""

import sys
import yaml
import json
from typing import Dict, Any, List, Optional, Union
from pydantic import BaseModel, Field, field_validator
import jsonschema
from jsonschema import Draft7Validator
import argparse


class AsyncAPIInfo(BaseModel):
    title: str
    version: str
    description: Optional[str] = None


class AsyncAPIServer(BaseModel):
    host: str
    protocol: str
    description: Optional[str] = None
    security: Optional[List[Dict[str, List[str]]]] = None


class AsyncAPIParameter(BaseModel):
    description: Optional[str] = None
    schema_: Dict[str, Any] = Field(..., alias="schema")


class AsyncAPIMessage(BaseModel):
    name: Optional[str] = None
    title: Optional[str] = None
    summary: Optional[str] = None
    description: Optional[str] = None
    payload: Optional[Dict[str, Any]] = None
    examples: Optional[List[Dict[str, Any]]] = None


class AsyncAPIChannel(BaseModel):
    address: str
    description: Optional[str] = None
    parameters: Optional[Dict[str, AsyncAPIParameter]] = None
    messages: Optional[Dict[str, AsyncAPIMessage]] = None


class AsyncAPIOperation(BaseModel):
    action: str  # send or receive
    channel: Dict[str, str]  # reference to channel
    summary: Optional[str] = None
    description: Optional[str] = None
    messages: Optional[List[Dict[str, str]]] = None


class AsyncAPIComponents(BaseModel):
    parameters: Optional[Dict[str, AsyncAPIParameter]] = None
    messages: Optional[Dict[str, AsyncAPIMessage]] = None
    schemas: Optional[Dict[str, Dict[str, Any]]] = None
    securitySchemes: Optional[Dict[str, Dict[str, Any]]] = None


class AsyncAPISpec(BaseModel):
    asyncapi: str = Field(..., pattern=r"^3\.0\.\d+$")
    info: AsyncAPIInfo
    servers: Optional[Dict[str, AsyncAPIServer]] = None
    channels: Optional[Dict[str, AsyncAPIChannel]] = None
    operations: Optional[Dict[str, AsyncAPIOperation]] = None
    components: Optional[AsyncAPIComponents] = None
    
    @field_validator('asyncapi')
    @classmethod
    def validate_asyncapi_version(cls, v):
        if not v.startswith('3.0'):
            raise ValueError('Only AsyncAPI 3.0.x versions are supported')
        return v


def load_yaml_file(filename: str) -> Dict[str, Any]:
    """Load and parse YAML file."""
    try:
        with open(filename, 'r', encoding='utf-8') as file:
            return yaml.safe_load(file)
    except FileNotFoundError:
        print(f"Error: File '{filename}' not found.", file=sys.stderr)
        sys.exit(1)
    except yaml.YAMLError as e:
        print(f"Error parsing YAML file: {e}", file=sys.stderr)
        sys.exit(1)


def validate_json_schemas(spec_data: Dict[str, Any]) -> List[str]:
    """Validate JSON schemas in the components section."""
    issues = []
    
    if 'components' not in spec_data or 'schemas' not in spec_data['components']:
        return issues
    
    schemas = spec_data['components']['schemas']
    
    for schema_name, schema_def in schemas.items():
        try:
            # Validate that each schema is a valid JSON schema
            Draft7Validator.check_schema(schema_def)
        except jsonschema.exceptions.SchemaError as e:
            issues.append(f"Invalid JSON Schema in '{schema_name}': {e.message}")
    
    return issues


def validate_message_references(spec_data: Dict[str, Any]) -> List[str]:
    """Validate that message references point to existing messages."""
    issues = []
    
    # Get all defined messages
    defined_messages = set()
    if 'components' in spec_data and 'messages' in spec_data['components']:
        defined_messages.update(spec_data['components']['messages'].keys())
    
    # Check channel message references
    if 'channels' in spec_data:
        for channel_name, channel_def in spec_data['channels'].items():
            if 'messages' in channel_def:
                for msg_key, msg_ref in channel_def['messages'].items():
                    # Handle both direct messages and $ref references
                    if isinstance(msg_ref, dict) and '$ref' in msg_ref:
                        ref_path = msg_ref['$ref']
                        if ref_path.startswith('#/components/messages/'):
                            msg_name = ref_path.split('/')[-1]
                            if msg_name not in defined_messages:
                                issues.append(f"Channel '{channel_name}' references undefined message '{msg_name}'")
    
    # Check operation message references
    if 'operations' in spec_data:
        for op_name, op_def in spec_data['operations'].items():
            if 'messages' in op_def:
                for msg_ref in op_def['messages']:
                    if isinstance(msg_ref, dict) and '$ref' in msg_ref:
                        ref_path = msg_ref['$ref']
                        if ref_path.startswith('#/components/messages/'):
                            msg_name = ref_path.split('/')[-1]
                            if msg_name not in defined_messages:
                                issues.append(f"Operation '{op_name}' references undefined message '{msg_name}'")
    
    return issues


def validate_security_schemes(spec_data: Dict[str, Any]) -> List[str]:
    """Validate security scheme definitions and references."""
    issues = []
    
    # Get defined security schemes
    defined_schemes = set()
    if 'components' in spec_data and 'securitySchemes' in spec_data['components']:
        defined_schemes.update(spec_data['components']['securitySchemes'].keys())
    
    # Check server security references
    if 'servers' in spec_data:
        for server_name, server_def in spec_data['servers'].items():
            if 'security' in server_def:
                for security_req in server_def['security']:
                    for scheme_name in security_req.keys():
                        if scheme_name not in defined_schemes:
                            issues.append(f"Server '{server_name}' references undefined security scheme '{scheme_name}'")
    
    return issues


def validate_asyncapi_spec(filename: str) -> bool:
    """Validate AsyncAPI specification file."""
    print(f"Validating AsyncAPI specification: {filename}")
    
    # Load the YAML file
    spec_data = load_yaml_file(filename)
    
    # Validate basic structure with Pydantic
    validation_errors = []
    try:
        spec = AsyncAPISpec.model_validate(spec_data)
        print("✓ Basic AsyncAPI structure is valid")
    except Exception as e:
        validation_errors.append(f"Pydantic validation failed: {e}")
    
    # Additional validations
    schema_issues = validate_json_schemas(spec_data)
    if schema_issues:
        validation_errors.extend(schema_issues)
    else:
        print("✓ JSON schemas are valid")
    
    message_issues = validate_message_references(spec_data)
    if message_issues:
        validation_errors.extend(message_issues)
    else:
        print("✓ Message references are valid")
    
    security_issues = validate_security_schemes(spec_data)
    if security_issues:
        validation_errors.extend(security_issues)
    else:
        print("✓ Security scheme references are valid")
    
    # Check for required sections
    required_sections = ['info', 'channels', 'operations', 'components']
    missing_sections = []
    for section in required_sections:
        if section not in spec_data:
            missing_sections.append(section)
    
    if missing_sections:
        validation_errors.append(f"Missing required sections: {', '.join(missing_sections)}")
    else:
        print("✓ All required sections present")
    
    # Report results
    if validation_errors:
        print("\n❌ Validation Issues:")
        for error in validation_errors:
            print(f"  - {error}")
        return False
    else:
        print("\n✅ AsyncAPI specification is valid!")
        return True


def main():
    parser = argparse.ArgumentParser(description='Validate AsyncAPI YAML specification')
    parser.add_argument('file', help='Path to AsyncAPI YAML file')
    parser.add_argument('--verbose', '-v', action='store_true', help='Verbose output')
    
    args = parser.parse_args()
    
    success = validate_asyncapi_spec(args.file)
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()