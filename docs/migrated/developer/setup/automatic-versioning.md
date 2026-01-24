# Automatic Semantic Versioning

TMI uses automatic semantic versioning based on conventional commit types to eliminate manual version updates and ensure monotonically increasing version numbers.

## Version Format

Versions follow semantic versioning: `MAJOR.MINOR.PATCH`

- **MAJOR**: Fixed at `0` (initial development phase)
- **MINOR**: Incremented on feature commits (`feat:`)
- **PATCH**: Incremented on all other commits (`fix:`, `refactor:`, `docs:`, etc.)

## How It Works

### Commit-Based Versioning

Every time you commit, the post-commit hook automatically:
1. Reads the commit message
2. Parses the conventional commit type
3. Updates version based on commit type:
   - `feat:` commits → increment MINOR, reset PATCH to 0
   - All other commits → increment PATCH
4. Updates version files
5. Amends the commit with the updated version files

```bash
# Starting at 0.9.0
git commit -m "fix(api): correct JWT validation"     # → 0.9.1 (patch++)
git commit -m "refactor(auth): simplify login flow"  # → 0.9.2 (patch++)
git commit -m "feat(api): add user deletion endpoint" # → 0.10.0 (minor++, patch=0)
git commit -m "docs: update API documentation"       # → 0.10.1 (patch++)
git commit -m "feat(websocket): add heartbeat"       # → 0.11.0 (minor++, patch=0)
```

### Conventional Commit Format

The version script recognizes these conventional commit patterns:
- `feat:` - New feature (increments MINOR)
- `feat(scope):` - New feature with scope (increments MINOR)
- `feat!:` - Breaking change feature (increments MINOR)
- `feat(scope)!:` - Breaking change feature with scope (increments MINOR)
- All other types (`fix:`, `refactor:`, `docs:`, `style:`, `test:`, `chore:`, etc.) increment PATCH

## Version Storage

### .version File

The `.version` file at the project root tracks the current version state:

```json
{
  "major": 0,
  "minor": 10,
  "patch": 0
}
```

This file is tracked in git and updated automatically.

### api/version.go

The `api/version.go` file contains the version variables that are set at build time using `-ldflags`:

```go
var (
    // Major version number
    VersionMajor = "0"
    // Minor version number
    VersionMinor = "..."  // Updated automatically by post-commit hook
    // Patch version number
    VersionPatch = "..."  // Updated automatically by post-commit hook
    // GitCommit is the git commit hash from build
    GitCommit = "development"
    // BuildDate is the build timestamp
    BuildDate = "unknown"
    // APIVersion is the API version string
    APIVersion = "v1"
)
```

Note: The actual values of `VersionMinor` and `VersionPatch` are maintained automatically by the post-commit hook and will reflect the current version state.

## Components

### 1. Version Management Script

`scripts/update-version.sh` manages version updates:

- `--commit`: Parses commit type and increments version accordingly (called by post-commit hook)
  - `feat:` commits → increment MINOR, reset PATCH
  - Other commits → increment PATCH

### 2. Post-commit Hook

`.git/hooks/post-commit` automatically runs after each commit to:
1. Call `update-version.sh --commit` to update version based on commit type
2. Stage the updated version files
3. Amend the commit with version changes (using `--no-verify` to prevent infinite loop)

### 3. Makefile Integration

The `build-server` target in the Makefile:
1. Reads the version from `.version`
2. Uses `-ldflags` to inject version info at compile time

## Manual Version Operations

### Check Current Version

```bash
cat .version
```

### Manual Version Update

If you need to manually adjust the version (e.g., for a major version bump):

1. Edit `.version` file directly (change `major`, `minor`, or `patch` fields)
2. Update `api/version.go` to match (or let the next commit sync it automatically)
3. Commit the changes

Note: Version syncing happens automatically on the next commit via the post-commit hook.

## Version Information at Runtime

The server exposes version information through the API at the root endpoint (`/`):

```bash
curl http://localhost:8080/ | jq '.service.build'
# Output: "0.10.1-abc1234"
```

The version includes:
- Major.Minor.Patch numbers
- Git commit hash (short)

## Troubleshooting

### Post-commit Hook Not Running

Ensure the hook is executable:

```bash
chmod +x .git/hooks/post-commit
```

### Version Not Updating

Check that `jq` is installed (required by the version script):

```bash
which jq
# If not found, install: brew install jq
```

### Version Mismatch

If `.version` and `api/version.go` are out of sync, make any commit to trigger the post-commit hook:

```bash
# The post-commit hook will automatically sync the files
git commit --allow-empty -m "chore: sync version files"
```

## Future Enhancements

When TMI reaches production readiness:

1. Update the major version to `1`
2. Consider using git tags for release versions
3. Implement changelog automation based on commits
