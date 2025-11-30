#!/bin/bash
# Generate base Arazzo scaffold from OpenAPI using Redocly CLI
# This creates the initial structure which is then enhanced with TMI workflow knowledge

set -e

OPENAPI_SPEC="docs/reference/apis/tmi-openapi.json"
SCAFFOLD_OUTPUT="docs/reference/apis/arazzo/scaffolds/base-scaffold.arazzo.yaml"

echo "🔧 Generating Arazzo scaffold with Redocly CLI..."
echo "   Source: ${OPENAPI_SPEC}"
echo "   Output: ${SCAFFOLD_OUTPUT}"

# Generate scaffold using Redocly
npx @redocly/cli generate-arazzo "${OPENAPI_SPEC}" \
  --output-file "${SCAFFOLD_OUTPUT}"

if [ $? -eq 0 ]; then
    echo "✅ Base scaffold generated successfully"

    # Show file size
    FILE_SIZE=$(stat -f%z "${SCAFFOLD_OUTPUT}" 2>/dev/null || stat -c%s "${SCAFFOLD_OUTPUT}" 2>/dev/null)
    echo "   File size: ${FILE_SIZE} bytes"

    # Quick validation with Spectral
    echo ""
    echo "🔍 Running quick validation..."
    npx @stoplight/spectral-cli lint "${SCAFFOLD_OUTPUT}" \
      --format stylish \
      --quiet || true

    echo ""
    echo "✅ Scaffold generation complete"
else
    echo "❌ Scaffold generation failed"
    exit 1
fi
