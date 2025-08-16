#!/usr/bin/env python3
# /// script
# dependencies = ["pyyaml"]
# ///
"""
YAML to Makefile Variable Converter

This script converts YAML configuration files to Makefile variable definitions.
It handles nested structures, lists, and various data types.

Usage: uv run scripts/yaml-to-make.py config/target.yml
"""

import yaml
import sys
import os
from typing import Any, Dict, List, Union

def safe_makefile_value(value: Any) -> str:
    """Convert a Python value to a safe Makefile variable value."""
    if isinstance(value, bool):
        return "true" if value else "false"
    elif isinstance(value, (int, float)):
        return str(value)
    elif isinstance(value, str):
        # Escape special characters for Makefile
        escaped = value.replace('\\', '\\\\').replace('"', '\\"').replace('$', '$$')
        return escaped
    else:
        return str(value)

def yaml_to_makefile_vars(data: Union[Dict, List, Any], prefix: str = "") -> List[str]:
    """
    Convert YAML data structure to Makefile variable definitions.
    
    Args:
        data: The YAML data (dict, list, or scalar)
        prefix: Variable name prefix for nested structures
        
    Returns:
        List of Makefile variable definitions
    """
    result = []
    
    if isinstance(data, dict):
        for key, value in data.items():
            # Convert key to valid Makefile variable name
            safe_key = key.upper().replace('-', '_').replace('.', '_').replace(' ', '_')
            var_name = f"{prefix}{safe_key}" if prefix else safe_key
            
            if isinstance(value, dict):
                # Recursively handle nested dictionaries
                result.extend(yaml_to_makefile_vars(value, f"{var_name}_"))
            elif isinstance(value, list):
                # Handle lists by joining elements with spaces
                if value:
                    list_value = " ".join(safe_makefile_value(item) for item in value)
                    result.append(f"{var_name} := {list_value}")
                else:
                    result.append(f"{var_name} :=")
            else:
                # Handle scalar values
                result.append(f"{var_name} := {safe_makefile_value(value)}")
    
    elif isinstance(data, list):
        # If the root is a list, create indexed variables
        for i, item in enumerate(data):
            result.extend(yaml_to_makefile_vars(item, f"{prefix}{i}_"))
    
    else:
        # Scalar value at root level
        if prefix:
            result.append(f"{prefix.rstrip('_')} := {safe_makefile_value(data)}")
    
    return result

def load_yaml_config(config_file: str) -> Dict:
    """Load and parse YAML configuration file."""
    if not os.path.exists(config_file):
        print(f"# Error: Configuration file not found: {config_file}", file=sys.stderr)
        sys.exit(1)
    
    try:
        with open(config_file, 'r') as f:
            config = yaml.safe_load(f)
            if config is None:
                return {}
            return config
    except yaml.YAMLError as e:
        print(f"# Error parsing YAML file {config_file}: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"# Error reading file {config_file}: {e}", file=sys.stderr)
        sys.exit(1)

def main():
    """Main entry point."""
    if len(sys.argv) != 2:
        print("Usage: python3 scripts/yaml-to-make.py config/target.yml", file=sys.stderr)
        sys.exit(1)
    
    config_file = sys.argv[1]
    
    # Load YAML configuration
    config = load_yaml_config(config_file)
    
    # Generate Makefile variables
    makefile_vars = yaml_to_makefile_vars(config)
    
    # Output variables (these will be evaluated by Make)
    print(f"# Generated from {config_file}")
    for var in makefile_vars:
        print(var)
    
    # Add some derived convenience variables
    if 'infrastructure' in config:
        infra = config['infrastructure']
        if 'postgres' in infra:
            pg = infra['postgres']
            print(f"POSTGRES_URL := postgresql://{pg.get('user', 'postgres')}:{pg.get('password', '')}@localhost:{pg.get('port', 5432)}/{pg.get('database', 'postgres')}")
        
        if 'redis' in infra:
            redis = infra['redis']
            print(f"REDIS_URL := redis://localhost:{redis.get('port', 6379)}/0")

if __name__ == "__main__":
    main()