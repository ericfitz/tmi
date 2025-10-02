# Automatic Semantic Versioning

TMI uses automatic semantic versioning to eliminate manual version updates and ensure monotonically increasing version numbers.

## Version Format

Versions follow semantic versioning: `MAJOR.MINOR.PATCH`

- **MAJOR**: Fixed at `0` (initial development phase)
- **MINOR**: Incremented on every commit
- **PATCH**: Incremented on every build, reset to `0` on commits

## How It Works

### Build Workflow

Every time you run `make build-server`, the patch version automatically increments:

```bash
# Starting at 0.9.0
make build-server  # → 0.9.1
make build-server  # → 0.9.2
make build-server  # → 0.9.3
```

### Commit Workflow

Every time you commit, the pre-commit hook automatically:
1. Increments the minor version
2. Resets the patch version to 0
3. Updates version files
4. Stages the changes for the commit

```bash
# Starting at 0.9.3
git commit -m "Add new feature"  # → 0.10.0 (minor++, patch=0)
make build-server                # → 0.10.1
make build-server                # → 0.10.2
git commit -m "Fix bug"          # → 0.11.0 (minor++, patch=0)
```

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
    VersionMajor = "0"
    VersionMinor = "10"
    VersionPatch = "0"
    GitCommit = "development"
    BuildDate = "unknown"
    APIVersion = "v1"
)
```

## Components

### 1. Version Management Script

`scripts/update-version.sh` manages version updates:

- `--build`: Increments patch version (called by Makefile)
- `--commit`: Increments minor version, resets patch (called by git hook)

### 2. Pre-commit Hook

`.git/hooks/pre-commit` automatically runs before each commit to update the version.

### 3. Makefile Integration

The `build-server` target in the Makefile:
1. Calls `update-version.sh --build` to increment the patch
2. Reads the version from `.version`
3. Uses `-ldflags` to inject version info at compile time

## Manual Version Operations

### Check Current Version

```bash
cat .version
```

### Manual Version Update

If you need to manually adjust the version (e.g., for a major version bump):

1. Edit `.version` file directly
2. Run `./scripts/update-version.sh --build` to sync `api/version.go`
3. Commit the changes

### Bypass Version Increment

If you need to build without incrementing the version (not recommended):

```bash
# Build directly with go
go build -o bin/server ./cmd/server
```

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

### Pre-commit Hook Not Running

Ensure the hook is executable:

```bash
chmod +x .git/hooks/pre-commit
```

### Version Not Updating

Check that `jq` is installed (required by the version script):

```bash
which jq
# If not found, install: brew install jq
```

### Version Mismatch

If `.version` and `api/version.go` are out of sync:

```bash
./scripts/update-version.sh --build
```

## Future Enhancements

When TMI reaches production readiness:

1. Update the major version to `1`
2. Consider using git tags for release versions
3. Implement changelog automation based on commits
