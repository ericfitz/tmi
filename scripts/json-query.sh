#!/bin/bash

# Function to display usage
usage() {
    echo "Usage: $0 <depth> <input_file>"
    echo "  depth: Maximum depth to traverse (integer)"
    echo "  input_file: Path to JSON file"
    echo ""
    echo "Example: $0 3 data.json"
    exit 1
}

# Check if correct number of arguments provided
if [ $# -ne 2 ]; then
    echo "Error: Wrong number of arguments"
    usage
fi

DEPTH="$1"
INPUT_FILE="$2"

# Validate depth is a positive integer
if ! [[ "$DEPTH" =~ ^[0-9]+$ ]]; then
    echo "Error: Depth must be a positive integer"
    exit 1
fi

# Check if input file exists
if [ ! -f "$INPUT_FILE" ]; then
    echo "Error: Input file '$INPUT_FILE' not found"
    exit 1
fi

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not installed. Please install jq first."
    exit 1
fi

echo "Extracting keys up to depth $DEPTH from $INPUT_FILE:"
echo "========================================"

# Execute the jq command with proper indentation using shell variable substitution
jq -r '
  def walk_with_indent($d; $indent):
    if $d <= 0 then empty
    elif type == "object" then
      keys_unsorted[] as $k |
        ($indent + $k),
        (getpath([$k]) | walk_with_indent($d - 1; $indent + "  "))
    else empty end;

  walk_with_indent('"$DEPTH"' ; "")
' "$INPUT_FILE"

echo "========================================"
echo "Done."

