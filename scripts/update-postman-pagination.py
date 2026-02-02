#!/usr/bin/env python3
"""
Update Postman collections for new pagination wrapper response format.

List endpoints now return:
{ items_field: [...], total, limit, offset }
instead of just arrays.
"""

import json
import re
import os
from pathlib import Path

# Mapping of endpoint patterns to their array property names
# Each entry is (patterns_to_match, property_name)
# patterns_to_match is a list of strings that should match in the test name
ENDPOINT_MAPPINGS = [
    (["threat model", "threat_model", "threatmodel"], "threat_models"),
    (["threats", "threat "], "threats"),
    (["diagrams", "diagram"], "diagrams"),
    (["documents", "document"], "documents"),
    (["notes", "note "], "notes"),
    (["assets", "asset "], "assets"),
    (["repositories", "repository", "sources", "source "], "repositories"),
    (["webhook subscription", "webhooks"], "subscriptions"),
    (["webhook deliver", "deliveries"], "deliveries"),
    (["credentials", "credential"], "credentials"),
    (["addons", "addon "], "addons"),
    (["invocations", "invocation"], "invocations"),
    (["administrators", "administrator"], "administrators"),
    (["admin user", "admin/users"], "users"),
    (["groups", "group "], "groups"),
    (["members", "member "], "members"),
]

# Test name patterns that should NOT be updated (these endpoints still return raw arrays)
SKIP_PATTERNS = [
    r"metadata",  # metadata endpoints return raw arrays
    r"providers",  # already correctly uses responseData.providers
]


def should_skip_test(test_name):
    """Check if this test should be skipped from updates."""
    for pattern in SKIP_PATTERNS:
        if re.search(pattern, test_name, re.IGNORECASE):
            return True
    return False


def get_array_property(test_name):
    """Determine the array property name based on the test name."""
    test_name_lower = test_name.lower()

    # Check for specific endpoint patterns
    if "threat model" in test_name_lower and "list" not in test_name_lower:
        # Individual threat model, not list
        return None

    for patterns, prop in ENDPOINT_MAPPINGS:
        for pattern in patterns:
            if pattern in test_name_lower:
                return prop

    return None


def update_test_script(exec_lines, test_name):
    """Update test script to use pagination wrapper."""
    if should_skip_test(test_name):
        return exec_lines, False

    array_prop = get_array_property(test_name)
    if not array_prop:
        return exec_lines, False

    updated = False
    new_lines = []

    # Handle both 'responseData' and 'jsonData' variable names
    var_names = ['responseData', 'jsonData', 'response', 'data']

    for line in exec_lines:
        line_updated = False

        for var_name in var_names:
            # Skip if already updated for this array_prop
            if f"{var_name}.{array_prop}" in line:
                continue

            # Pattern 1: pm.expect(var).to.be.an('array')
            if f"pm.expect({var_name}).to.be.an('array')" in line:
                indent = len(line) - len(line.lstrip())
                indent_str = " " * indent
                line = f"{indent_str}pm.expect({var_name}).to.have.property('{array_prop}');\n"
                line += f"{indent_str}pm.expect({var_name}.{array_prop}).to.be.an('array')"
                updated = True
                line_updated = True
                break

            # Pattern 2: var.length -> var.array_prop.length
            if f"{var_name}.length" in line:
                line = line.replace(f"{var_name}.length", f"{var_name}.{array_prop}.length")
                updated = True
                line_updated = True

            # Pattern 3: var[0] -> var.array_prop[0]
            if re.search(rf"{var_name}\[\d+\]", line):
                line = re.sub(rf"{var_name}\[(\d+)\]", f"{var_name}.{array_prop}[\\1]", line)
                updated = True
                line_updated = True

            # Pattern 4: var.forEach -> var.array_prop.forEach
            if f"{var_name}.forEach" in line:
                line = line.replace(f"{var_name}.forEach", f"{var_name}.{array_prop}.forEach")
                updated = True
                line_updated = True

            # Pattern 5: var.find -> var.array_prop.find
            if f"{var_name}.find" in line:
                line = line.replace(f"{var_name}.find", f"{var_name}.{array_prop}.find")
                updated = True
                line_updated = True

            # Pattern 6: var.filter -> var.array_prop.filter
            if f"{var_name}.filter" in line:
                line = line.replace(f"{var_name}.filter", f"{var_name}.{array_prop}.filter")
                updated = True
                line_updated = True

            # Pattern 7: var.map -> var.array_prop.map
            if f"{var_name}.map" in line:
                line = line.replace(f"{var_name}.map", f"{var_name}.{array_prop}.map")
                updated = True
                line_updated = True

        new_lines.append(line)

    return new_lines, updated


def process_item(item, path=""):
    """Recursively process a Postman collection item."""
    changes = 0
    current_path = f"{path}/{item.get('name', 'unknown')}" if path else item.get('name', 'unknown')

    # Process events (pre-request and test scripts)
    if 'event' in item:
        for event in item['event']:
            if event.get('listen') == 'test' and 'script' in event:
                script = event['script']
                if 'exec' in script and isinstance(script['exec'], list):
                    test_name = item.get('name', '')
                    new_exec, updated = update_test_script(script['exec'], test_name)
                    if updated:
                        script['exec'] = new_exec
                        changes += 1
                        print(f"  Updated: {current_path}")

    # Recursively process sub-items
    if 'item' in item:
        for sub_item in item['item']:
            changes += process_item(sub_item, current_path)

    return changes


def process_collection(filepath):
    """Process a single Postman collection file."""
    print(f"\nProcessing: {filepath}")

    with open(filepath, 'r') as f:
        collection = json.load(f)

    total_changes = 0

    # Process all items in the collection
    if 'item' in collection:
        for item in collection['item']:
            total_changes += process_item(item)

    if total_changes > 0:
        # Write updated collection
        with open(filepath, 'w') as f:
            json.dump(collection, f, indent=2)
        print(f"  Total changes: {total_changes}")
    else:
        print("  No changes needed")

    return total_changes


def main():
    postman_dir = Path(__file__).parent.parent / 'test' / 'postman'

    # Process all collection files except result files
    collection_files = [
        f for f in postman_dir.glob('*-collection.json')
        if 'results' not in f.name
    ]

    total_changes = 0
    for filepath in sorted(collection_files):
        total_changes += process_collection(filepath)

    print(f"\n=== Summary ===")
    print(f"Total files processed: {len(collection_files)}")
    print(f"Total changes made: {total_changes}")


if __name__ == '__main__':
    main()
