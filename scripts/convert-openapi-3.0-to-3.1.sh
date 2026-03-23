#!/usr/bin/env bash
# Convert OpenAPI 3.0.3 spec to 3.1.0
# Usage: ./scripts/convert-openapi-3.0-to-3.1.sh api-schema/tmi-openapi.json api-schema/tmi-openapi-3.1.json

set -euo pipefail

INPUT="${1:?Usage: $0 <input.json> <output.json>}"
OUTPUT="${2:?Usage: $0 <input.json> <output.json>}"

if [ ! -f "$INPUT" ]; then
  echo "Error: Input file not found: $INPUT" >&2
  exit 1
fi

echo "Converting $INPUT -> $OUTPUT"
echo "  OpenAPI 3.0.3 -> 3.1.0"

jq '
  # 1. Bump version
  .openapi = "3.1.0" |

  # 2. Add JSON Schema dialect
  .jsonSchemaDialect = "https://json-schema.org/draft/2020-12/schema" |

  # 3. Convert nullable: true to type arrays
  # Walk all objects: if nullable==true and type is a string, convert to array with null
  (.. | objects | select(.nullable == true and (.type | type) == "string")) |=
    ((.type = [.type, "null"]) | del(.nullable)) |

  # 4. Convert nullable: true without explicit type (just remove nullable flag)
  (.. | objects | select(.nullable == true and .type == null)) |=
    del(.nullable) |

  # 5. Convert exclusiveMinimum/exclusiveMaximum boolean style to numeric style
  # (TMI has 0 instances, but included for correctness)
  (.. | objects | select(.exclusiveMinimum == true)) |=
    ((.exclusiveMinimum = .minimum) | del(.minimum)) |
  (.. | objects | select(.exclusiveMaximum == true)) |=
    ((.exclusiveMaximum = .maximum) | del(.maximum))
' "$INPUT" > "$OUTPUT"

# Verify output is valid JSON
if jq empty "$OUTPUT" 2>/dev/null; then
  echo "  Output is valid JSON"
else
  echo "Error: Output is not valid JSON" >&2
  exit 1
fi

# Report conversion stats
NULLABLE_REMAINING=$(jq '[.. | objects | select(.nullable != null)] | length' "$OUTPUT")
VERSION=$(jq -r '.openapi' "$OUTPUT")
echo "  Output version: $VERSION"
echo "  Remaining nullable fields: $NULLABLE_REMAINING (should be 0)"

echo "Done."
