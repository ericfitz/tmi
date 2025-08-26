# TMI Shared Resources Subtree Integration Guide

This guide explains how to integrate TMI's shared API resources into your repository using Git subtree.

## Overview

TMI provides shared resources through a Git subtree that includes:
- OpenAPI and AsyncAPI specifications
- Client integration documentation
- OAuth setup guides
- Reference SDK implementations

These resources are maintained in the TMI repository's `shared/` directory and published to a separate `shared` branch for easy consumption.

## Prerequisites

- Git version 1.7.11 or later (for subtree support)
- Access to the TMI repository (public or with appropriate permissions)
- Understanding of your project's directory structure

## Initial Setup

### Step 1: Choose Your Target Directory

Decide where in your repository you want the TMI shared resources to appear. Common choices:
- `shared-api/` - For API-focused integration
- `external/tmi/` - For vendor/external resources
- `tmi-shared/` - For clear TMI identification

### Step 2: Add the Subtree

Add the TMI shared resources to your repository:

```bash
# Replace [TARGET_DIR] with your chosen directory name
# Replace [TMI_REPO_URL] with the actual TMI repository URL

git subtree add --prefix=[TARGET_DIR] [TMI_REPO_URL] shared --squash
```

Example:
```bash
git subtree add --prefix=shared-api https://github.com/yourusername/tmi.git shared --squash
```

This command:
- Creates a new directory in your repository
- Pulls the `shared` branch from TMI
- `--squash` condenses the history into a single commit

### Step 3: Verify the Integration

After the subtree is added, verify the contents:

```bash
ls -la [TARGET_DIR]/
```

You should see:
```
shared-api/
├── README.md
├── api-specs/
│   ├── tmi-openapi.json
│   └── tmi-asyncapi.yml
├── docs/
│   ├── CLIENT_INTEGRATION_GUIDE.md
│   ├── TMI-API-v1_0.md
│   └── ...
└── sdk-examples/
    └── python-sdk/
```

## Pulling Updates

To update your copy of the TMI shared resources:

```bash
git subtree pull --prefix=[TARGET_DIR] [TMI_REPO_URL] shared --squash
```

Example:
```bash
git subtree pull --prefix=shared-api https://github.com/yourusername/tmi.git shared --squash
```

This will:
- Fetch the latest changes from TMI's `shared` branch
- Merge them into your repository
- Maintain a clean commit history with `--squash`

## Best Practices

### 1. Create Helper Scripts

Add these commands to your `Makefile` or create shell scripts:

```makefile
# Makefile example
update-tmi-shared:
	git subtree pull --prefix=shared-api https://github.com/yourusername/tmi.git shared --squash

.PHONY: update-tmi-shared
```

Or as a shell script (`scripts/update-tmi.sh`):
```bash
#!/bin/bash
echo "Updating TMI shared resources..."
git subtree pull --prefix=shared-api https://github.com/yourusername/tmi.git shared --squash
echo "✅ TMI shared resources updated"
```

### 2. Document the Integration

Add to your project's README:

```markdown
## TMI API Integration

This project uses TMI's shared API resources via Git subtree. The resources are located in `shared-api/`.

To update the API specifications:
```bash
make update-tmi-shared
```
```

### 3. Avoid Direct Modifications

- **Don't modify** files in the subtree directory directly
- If you need customizations, copy files to another location
- Submit changes to the upstream TMI repository instead

### 4. Regular Updates

- Schedule regular updates (e.g., weekly or with each sprint)
- Review TMI's changelog before updating
- Test your integration after updates

## Common Issues and Solutions

### Issue: Merge Conflicts

If you encounter merge conflicts during `subtree pull`:

1. Resolve conflicts in the affected files
2. Complete the merge:
   ```bash
   git add .
   git commit -m "Merge TMI shared resources update"
   ```

### Issue: Wrong Directory Structure

If files appear in the wrong location:
1. Remove the subtree directory: `rm -rf [TARGET_DIR]`
2. Remove from git: `git rm -r [TARGET_DIR]`
3. Commit the removal
4. Re-add the subtree with the correct prefix

### Issue: Authentication Required

For private repositories:
```bash
# Use SSH URL
git subtree add --prefix=shared-api git@github.com:yourusername/tmi.git shared --squash

# Or use personal access token
git subtree add --prefix=shared-api https://YOUR_TOKEN@github.com/yourusername/tmi.git shared --squash
```

## Integration Examples

### TypeScript/JavaScript Project

```typescript
// Import OpenAPI spec for type generation
import tmiSpec from './shared-api/api-specs/tmi-openapi.json';

// Use with OpenAPI generator
// npm run generate-types -- -i ./shared-api/api-specs/tmi-openapi.json
```

### Python Project

```python
# Reference the OpenAPI spec
from pathlib import Path
import json

spec_path = Path(__file__).parent / "shared-api" / "api-specs" / "tmi-openapi.json"
with open(spec_path) as f:
    tmi_spec = json.load(f)
```

### CI/CD Integration

```yaml
# GitHub Actions example
- name: Update TMI Shared Resources
  run: |
    git config user.name "GitHub Actions"
    git config user.email "actions@github.com"
    git subtree pull --prefix=shared-api https://github.com/yourusername/tmi.git shared --squash
```

## Removing the Subtree

If you need to remove the subtree integration:

```bash
# Remove the directory
git rm -r [TARGET_DIR]
git commit -m "Remove TMI shared subtree"
```

Note: This only removes the files; the subtree merge history remains in your repository.

## Getting Help

- Check the TMI repository's issues for known problems
- Review the `shared/README.md` for content-specific documentation
- Contact the TMI team for integration support

## Summary

Git subtree provides a simple way to include TMI's shared resources in your repository while maintaining a clean history. The key commands are:

- **Add**: `git subtree add --prefix=[DIR] [URL] shared --squash`
- **Update**: `git subtree pull --prefix=[DIR] [URL] shared --squash`

Remember to document your integration and create automation for regular updates.