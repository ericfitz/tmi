#!/usr/bin/env python3
"""
JSON Patcher Utility

A tool to patch JSON files by replacing content at specific JSON paths.
Useful for making precise modifications to large JSON files like OpenAPI specifications.

Usage:
    python patch-json.py --sourcefile input.json --jsonpath "$.components.schemas.ThreatModel" --patchfile patch.json

Features:
- Backs up the original file before modification
- Supports JSONPath navigation to target specific elements
- Preserves formatting and structure of the source file
- Validates JSON syntax before writing
"""

import json
import argparse
import sys
import os
from pathlib import Path
from typing import Any, Dict, List, Union
import shutil


def parse_json_path(path: str) -> List[str]:
    """
    Parse a simple JSON path like "$.components.schemas.ThreatModel" 
    into a list of keys: ["components", "schemas", "ThreatModel"]
    
    Currently supports simple dot notation paths starting with $.
    """
    if not path.startswith('$.'):
        raise ValueError("JSON path must start with '$.'")
    
    # Remove the '$.' prefix and split by dots
    path_parts = path[2:].split('.')
    return [part for part in path_parts if part]  # Remove empty parts


def navigate_to_parent(data: Dict[str, Any], path_parts: List[str]) -> tuple[Dict[str, Any], str]:
    """
    Navigate to the parent of the target element and return the parent dict and target key.
    
    Args:
        data: The root JSON data
        path_parts: List of path components
        
    Returns:
        Tuple of (parent_dict, target_key)
        
    Raises:
        KeyError: If the path doesn't exist
    """
    current = data
    
    # Navigate to the parent (all parts except the last)
    for part in path_parts[:-1]:
        if not isinstance(current, dict):
            raise KeyError(f"Cannot navigate through non-dict at path component '{part}'")
        if part not in current:
            raise KeyError(f"Path component '{part}' not found")
        current = current[part]
    
    # The last part is the target key
    target_key = path_parts[-1]
    
    return current, target_key


def patch_json(source_data: Dict[str, Any], json_path: str, patch_data: Any) -> Dict[str, Any]:
    """
    Patch the source JSON data at the specified path with the patch data.
    
    Args:
        source_data: The source JSON data
        json_path: JSONPath string to the target element
        patch_data: The data to replace at the target path
        
    Returns:
        The modified JSON data
    """
    path_parts = parse_json_path(json_path)
    
    if not path_parts:
        raise ValueError("Empty JSON path")
    
    # Navigate to the parent and get the target key
    parent, target_key = navigate_to_parent(source_data, path_parts)
    
    # Verify the target exists
    if target_key not in parent:
        raise KeyError(f"Target key '{target_key}' not found at path '{json_path}'")
    
    # Replace the target with the patch data
    parent[target_key] = patch_data
    
    return source_data


def backup_file(file_path: Path) -> Path:
    """
    Create a backup of the source file.
    
    Args:
        file_path: Path to the file to backup
        
    Returns:
        Path to the backup file
    """
    backup_path = file_path.with_suffix(f"{file_path.suffix}.backup")
    
    # If backup already exists, create numbered backups
    counter = 1
    while backup_path.exists():
        backup_path = file_path.with_suffix(f"{file_path.suffix}.backup.{counter}")
        counter += 1
    
    shutil.copy2(file_path, backup_path)
    print(f"✅ Created backup: {backup_path}")
    return backup_path


def load_json_file(file_path: Path) -> Dict[str, Any]:
    """Load and parse a JSON file."""
    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            return json.load(f)
    except json.JSONDecodeError as e:
        raise ValueError(f"Invalid JSON in {file_path}: {e}")
    except FileNotFoundError:
        raise FileNotFoundError(f"File not found: {file_path}")


def save_json_file(file_path: Path, data: Dict[str, Any], indent: int = 2) -> None:
    """Save JSON data to a file with formatting."""
    try:
        with open(file_path, 'w', encoding='utf-8') as f:
            json.dump(data, f, indent=indent, ensure_ascii=False)
        print(f"✅ Updated file: {file_path}")
    except Exception as e:
        raise ValueError(f"Failed to write JSON to {file_path}: {e}")


def validate_args(args) -> None:
    """Validate command line arguments."""
    source_path = Path(args.sourcefile)
    patch_path = Path(args.patchfile)
    
    if not source_path.exists():
        raise FileNotFoundError(f"Source file not found: {source_path}")
    
    if not patch_path.exists():
        raise FileNotFoundError(f"Patch file not found: {patch_path}")
    
    if not args.jsonpath.startswith('$.'):
        raise ValueError("JSON path must start with '$.' (e.g., '$.components.schemas.ThreatModel')")


def main():
    parser = argparse.ArgumentParser(
        description="Patch JSON files by replacing content at specific JSON paths",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Replace a schema definition
  python patch-json.py --sourcefile api.json --jsonpath "$.components.schemas.ThreatModel" --patchfile new_threat_model.json
  
  # Replace a specific endpoint
  python patch-json.py --sourcefile api.json --jsonpath "$.paths./threat_models.post" --patchfile new_endpoint.json
  
  # Add Input/Output schema separation
  python patch-json.py --sourcefile api.json --jsonpath "$.components.schemas" --patchfile updated_schemas.json
        """
    )
    
    parser.add_argument(
        '--sourcefile', '-s',
        required=True,
        help="Path to the source JSON file to patch"
    )
    
    parser.add_argument(
        '--jsonpath', '-p',
        required=True,
        help="JSON path to the element to replace (e.g., '$.components.schemas.ThreatModel')"
    )
    
    parser.add_argument(
        '--patchfile', '-f',
        required=True,
        help="Path to the JSON file containing the replacement data"
    )
    
    parser.add_argument(
        '--no-backup',
        action='store_true',
        help="Skip creating a backup of the source file"
    )
    
    parser.add_argument(
        '--indent',
        type=int,
        default=2,
        help="JSON indentation level (default: 2)"
    )
    
    args = parser.parse_args()
    
    try:
        # Validate arguments
        validate_args(args)
        
        source_path = Path(args.sourcefile)
        patch_path = Path(args.patchfile)
        
        print(f"🔧 Patching JSON file...")
        print(f"   Source: {source_path}")
        print(f"   Path: {args.jsonpath}")
        print(f"   Patch: {patch_path}")
        
        # Load the source and patch files
        print("📖 Loading source file...")
        source_data = load_json_file(source_path)
        
        print("📖 Loading patch file...")
        patch_data = load_json_file(patch_path)
        
        # Create backup unless disabled
        if not args.no_backup:
            backup_path = backup_file(source_path)
        
        # Apply the patch
        print("🔄 Applying patch...")
        patched_data = patch_json(source_data, args.jsonpath, patch_data)
        
        # Save the result
        print("💾 Saving patched file...")
        save_json_file(source_path, patched_data, indent=args.indent)
        
        print("✅ JSON patching completed successfully!")
        
    except Exception as e:
        print(f"❌ Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()