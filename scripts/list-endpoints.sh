#!/bin/zsh
jq -r '.paths | to_entries[] | "\n## \(.key)\n" + (.value | to_entries[] | "- \(.key | ascii_upcase): " + (.value.responses | to_entries | map("\(.key)") | join(", ")))' shared/api-specs/tmi-openapi.json
