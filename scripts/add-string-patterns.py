#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""
Add pattern constraints to string fields in OpenAPI spec response schemas.
Addresses security scanner requirements for string validation.
"""

import json
import sys
from pathlib import Path
from datetime import datetime

SPEC_FILE = Path("docs/reference/apis/tmi-openapi.json")
DEFAULT_PATTERN = r"^[\u0020-\uFFFF]*$"  # Printable Unicode

def add_patterns_to_object(obj, path=""):
    """Recursively add patterns to string fields without them."""
    if isinstance(obj, dict):
        # Check if this is a string schema without a pattern
        if obj.get("type") == "string" and "pattern" not in obj and "enum" not in obj:
            # Determine appropriate pattern based on format/context
            if obj.get("format") == "uuid":
                obj["pattern"] = r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
            elif obj.get("format") == "date-time":
                obj["pattern"] = r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$"
            elif obj.get("format") == "uri":
                obj["pattern"] = r"^https?://[^\s/$.?#].[^\s]*$"
            elif obj.get("format") == "email":
                obj["pattern"] = r"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$"
            else:
                # General text field
                obj["pattern"] = DEFAULT_PATTERN

        # Recurse into nested objects
        for key, value in obj.items():
            add_patterns_to_object(value, f"{path}.{key}")

    elif isinstance(obj, list):
        for i, item in enumerate(obj):
            add_patterns_to_object(item, f"{path}[{i}]")

def main():
    # Create backup
    backup_file = SPEC_FILE.with_suffix(f".json.backup.{datetime.now().strftime('%Y%m%d_%H%M%S')}")
    backup_file.write_text(SPEC_FILE.read_text())
    print(f"Created backup: {backup_file}")

    # Load spec
    with open(SPEC_FILE) as f:
        spec = json.load(f)

    # Add patterns to schemas
    if "components" in spec and "schemas" in spec["components"]:
        add_patterns_to_object(spec["components"]["schemas"])

    # Write back with proper formatting
    with open(SPEC_FILE, 'w') as f:
        json.dump(spec, f, indent=2, ensure_ascii=False)

    print(f"Added patterns to string fields in {SPEC_FILE}")
    return 0

if __name__ == "__main__":
    sys.exit(main())
