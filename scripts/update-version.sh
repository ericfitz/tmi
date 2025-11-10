#!/bin/bash
# update-version.sh - Automatic version management for TMI
#
# Usage:
#   ./update-version.sh --commit  # Increment version based on commit type
#
# Versioning Rules:
#   - feat: commits increment MINOR version, reset PATCH to 0
#   - All other commits (fix, refactor, docs, etc.) increment PATCH version

set -e

VERSION_FILE=".version"
VERSION_GO_FILE="api/version.go"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
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

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Check if .version file exists
if [ ! -f "$VERSION_FILE" ]; then
    log_error "Version file $VERSION_FILE not found"
    exit 1
fi

# Read current version
MAJOR=$(jq -r '.major' "$VERSION_FILE")
MINOR=$(jq -r '.minor' "$VERSION_FILE")
PATCH=$(jq -r '.patch' "$VERSION_FILE")

log_info "Current version: $MAJOR.$MINOR.$PATCH"

# Determine action
if [ "$1" == "--commit" ]; then
    # Get the commit message (most recent commit)
    COMMIT_MSG=$(git log -1 --format='%s' 2>/dev/null || echo "")

    if [ -z "$COMMIT_MSG" ]; then
        log_warning "Could not read commit message, defaulting to PATCH increment"
        PATCH=$((PATCH + 1))
    else
        # Extract commit type from conventional commit format: type(scope): description
        # Matches: feat, feat(scope), feat!, feat(scope)!
        if echo "$COMMIT_MSG" | grep -qE '^feat(\(.+\))?(!)?:'; then
            # Feature commit: increment MINOR, reset PATCH
            MINOR=$((MINOR + 1))
            PATCH=0
            log_info "Feature commit detected: incrementing MINOR version, resetting PATCH"
        else
            # All other commits: increment PATCH
            PATCH=$((PATCH + 1))
            log_info "Non-feature commit detected: incrementing PATCH version"
        fi
    fi

else
    log_error "Invalid argument. Use --commit"
    echo "Usage:"
    echo "  $0 --commit  # Increment version based on commit type"
    exit 1
fi

NEW_VERSION="$MAJOR.$MINOR.$PATCH"
log_success "New version: $NEW_VERSION"

# Update .version file
cat > "$VERSION_FILE" <<EOF
{
  "major": $MAJOR,
  "minor": $MINOR,
  "patch": $PATCH
}
EOF

log_success "Updated $VERSION_FILE"

# Update api/version.go
if [ -f "$VERSION_GO_FILE" ]; then
    # Update the version variables in version.go
    sed -i.bak "s/VersionMajor = \"[0-9]*\"/VersionMajor = \"$MAJOR\"/" "$VERSION_GO_FILE"
    sed -i.bak "s/VersionMinor = \"[0-9]*\"/VersionMinor = \"$MINOR\"/" "$VERSION_GO_FILE"
    sed -i.bak "s/VersionPatch = \"[0-9]*\"/VersionPatch = \"$PATCH\"/" "$VERSION_GO_FILE"
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
