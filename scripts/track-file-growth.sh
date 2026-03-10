#!/bin/sh
# Track a file's size growth over time in a git repository.
# Handles renames by searching all known paths the file has had.
# Usage: track-file-growth.sh [file-path]
# Default: api-schema/tmi-openapi.json

set -e

FILE="${1:-api-schema/tmi-openapi.json}"
BASENAME=$(basename "$FILE")

# Verify we're in a git repo
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "Error: not inside a git repository" >&2
    exit 1
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Phase 1: Discover all paths this file has lived at
echo "Discovering file paths..." >&2
git log --all --diff-filter=ACMRD --name-only --format="" \
    -- "**/$BASENAME" "*/$BASENAME" "$BASENAME" 2>/dev/null \
    | sort -u > "$TMPDIR/paths"

if [ ! -s "$TMPDIR/paths" ]; then
    echo "No history found for: $BASENAME" >&2
    exit 1
fi

echo "Found paths:" >&2
cat "$TMPDIR/paths" >&2

# Phase 2: Get all commits touching any of these paths, deduplicated and sorted by date
# Build the git log arguments from discovered paths
paths_args=""
while IFS= read -r p; do
    paths_args="$paths_args $p"
done < "$TMPDIR/paths"

# Get unique commits sorted by author date (oldest first)
# shellcheck disable=SC2086
git log --all --reverse --format="%aI %H %s" -- $paths_args \
    | sort -t' ' -k1,1 | awk '!seen[$2]++' > "$TMPDIR/commits"

total=$(wc -l < "$TMPDIR/commits" | tr -d ' ')
echo "Processing $total commits..." >&2

# Phase 3: For each commit, find the file and get its size
# We need to figure out which path the file was at for each commit
count=0
while IFS= read -r line; do
    ts=$(echo "$line" | cut -d' ' -f1)
    hash=$(echo "$line" | cut -d' ' -f2)
    subject=$(echo "$line" | cut -d' ' -f3-)
    short_hash=$(echo "$hash" | cut -c1-10)

    # Try each known path to find the file at this commit
    size=""
    while IFS= read -r p; do
        s=$(git cat-file -s "$hash:$p" 2>/dev/null) && {
            size="$s"
            break
        }
    done < "$TMPDIR/paths"

    if [ -z "$size" ]; then
        size="0"
    fi

    # Format size with commas
    formatted=$(printf "%'d" "$size" 2>/dev/null || echo "$size")

    echo "$ts|$short_hash|$formatted|$subject" >> "$TMPDIR/output"

    count=$((count + 1))
    if [ $((count % 50)) -eq 0 ]; then
        echo "  $count / $total..." >&2
    fi
done < "$TMPDIR/commits"

echo "Done." >&2

# Phase 4: Determine column widths and print
max_size_len=4
while IFS='|' read -r _ _ formatted _; do
    len=${#formatted}
    if [ "$len" -gt "$max_size_len" ]; then
        max_size_len=$len
    fi
done < "$TMPDIR/output"

# Print header
printf "%-25s  %-10s  %${max_size_len}s  %s\n" \
    "TIMESTAMP" "COMMIT" "SIZE" "MESSAGE"
dashes=$(printf '%*s' "$max_size_len" '' | tr ' ' '-')
printf "%-25s  %-10s  %${max_size_len}s  %s\n" \
    "-------------------------" "----------" "$dashes" "-------"

# Print data
while IFS='|' read -r ts short_hash formatted subject; do
    printf "%-25s  %-10s  %${max_size_len}s  %s\n" \
        "$ts" "$short_hash" "$formatted" "$subject"
done < "$TMPDIR/output"
