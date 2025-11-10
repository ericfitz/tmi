#!/bin/bash
# update-version.sh - Automatic version management for TMI
#
# Usage:
#   ./update-version.sh --commit  # Increment build number (for commits, will amend)

set -e

VERSION_FILE=".version"
VERSION_GO_FILE="api/version.go"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if .version file exists
if [ ! -f "$VERSION_FILE" ]; then
    log_error "Version file $VERSION_FILE not found"
    exit 1
fi

# Read current version
MAJOR=$(jq -r '.major' "$VERSION_FILE")
MINOR=$(jq -r '.minor' "$VERSION_FILE")
BUILD=$(jq -r '.build' "$VERSION_FILE")

log_info "Current version: $MAJOR.$MINOR.$BUILD"

# Determine action
if [ "$1" == "--commit" ]; then
    # Increment build number
    BUILD=$((BUILD + 1))
    log_info "Incrementing build number for commit"

else
    log_error "Invalid argument. Use --commit"
    echo "Usage:"
    echo "  $0 --commit  # Increment build number (for commits)"
    exit 1
fi

NEW_VERSION="$MAJOR.$MINOR.$BUILD"
log_success "New version: $NEW_VERSION"

# Update .version file
cat > "$VERSION_FILE" <<EOF
{
  "major": $MAJOR,
  "minor": $MINOR,
  "build": $BUILD
}
EOF

log_success "Updated $VERSION_FILE"

# Update api/version.go
if [ -f "$VERSION_GO_FILE" ]; then
    # Update the version variables in version.go
    sed -i.bak "s/VersionMajor = \"[0-9]*\"/VersionMajor = \"$MAJOR\"/" "$VERSION_GO_FILE"
    sed -i.bak "s/VersionMinor = \"[0-9]*\"/VersionMinor = \"$MINOR\"/" "$VERSION_GO_FILE"
    sed -i.bak "s/VersionPatch = \"[0-9]*\"/VersionPatch = \"$BUILD\"/" "$VERSION_GO_FILE"
    rm -f "${VERSION_GO_FILE}.bak"
    log_success "Updated $VERSION_GO_FILE"
else
    log_error "$VERSION_GO_FILE not found"
    exit 1
fi

# Stage the version files for git
if git rev-parse --git-dir > /dev/null 2>&1; then
    git add "$VERSION_FILE" "$VERSION_GO_FILE"
    log_success "Staged version files for amend"
fi

log_success "Version update complete: $NEW_VERSION"
