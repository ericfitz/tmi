# Arazzo Workflow Specification Generation

This document describes the TMI Arazzo generation pipeline that automatically creates API workflow specifications from the OpenAPI definition and `api-workflows.json` knowledge base.

## Overview

The Arazzo generation system consists of four stages:

1. **Scaffold Generation** - Redocly CLI creates base Arazzo structure from OpenAPI
2. **Workflow Enhancement** - Python script enriches with TMI-specific workflow patterns
3. **Validation** - Spectral CLI validates against Arazzo v1.0.0 specification
4. **Output** - Both YAML and JSON formats generated

## Quick Start

```bash
# Full pipeline (installs dependencies, generates, validates)
make generate-arazzo

# Individual stages
make arazzo-install    # Install Redocly CLI and Spectral
make arazzo-scaffold   # Generate base scaffold
make arazzo-enhance    # Add TMI workflows
make validate-arazzo   # Validate output
```

## Architecture

### Input Files

- **[docs/reference/apis/tmi-openapi.json](tmi-openapi.json)** (545KB) - Complete OpenAPI 3.0.3 specification
- **[docs/reference/apis/api-workflows.json](api-workflows.json)** - TMI workflow knowledge base containing:
  - OAuth PKCE flow patterns (RFC 7636)
  - Public vs authenticated endpoint classifications
  - Resource hierarchies and prerequisites
  - 7 complete end-to-end workflow sequences

### Output Files

- **[docs/reference/apis/tmi.arazzo.yaml](tmi.arazzo.yaml)** - Human-readable YAML format
- **[docs/reference/apis/tmi.arazzo.json](tmi.arazzo.json)** - Machine-readable JSON format
- **[docs/reference/apis/arazzo/scaffolds/base-scaffold.arazzo.yaml](arazzo/scaffolds/base-scaffold.arazzo.yaml)** - Intermediate scaffold

### Pipeline Components

#### 1. Scaffold Generation ([scripts/generate-arazzo-scaffold.sh](../../../scripts/generate-arazzo-scaffold.sh))

Uses Redocly CLI to generate initial Arazzo structure:

```bash
npx @redocly/cli generate-arazzo docs/reference/apis/tmi-openapi.json \
  --output-file docs/reference/apis/arazzo/scaffolds/base-scaffold.arazzo.yaml
```

**Output**: Base scaffold with all OpenAPI operations mapped to potential workflow steps, but without TMI-specific workflow logic.

#### 2. Workflow Enhancement ([scripts/enhance-arazzo-with-workflows.py](../../../scripts/enhance-arazzo-with-workflows.py))

Python script (500+ lines) that:

- **Loads TMI Knowledge**: Reads `api-workflows.json` patterns
- **OAuth PKCE Integration**: Implements RFC 7636 flow with:
  - `code_verifier` generation (random 128-char string)
  - `code_challenge` calculation (SHA-256 hash, base64url encoded)
  - Three-step workflow: authorization → callback → token exchange
- **Prerequisite Mapping**: Translates TMI prerequisites to Arazzo `dependsOn`:
  ```python
  prereq_map = {
      'oauth_complete': 'oauth_token_exchange',
      'threat_model_create': 'create_threat_model',
      'diagram_create': 'create_diagram',
      # ... 12 more mappings
  }
  ```
- **Complete Sequences**: Adds all 7 end-to-end workflows:
  1. OAuth PKCE Authentication
  2. Threat Model CRUD
  3. Diagram Creation & Collaboration
  4. Threat Management
  5. Document Management
  6. Metadata Operations
  7. Webhooks & Addons
- **Success Criteria**: HTTP-aware validation (GET→200, POST→201, DELETE→204)
- **Sample Payloads**: Generates example request bodies for POST/PUT operations
- **Workflow Outputs**: Defines key outputs for chaining (tokens, resource IDs)

**Dependencies** (UV inline TOML):
```python
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",
# ]
# ///
```

#### 3. Validation ([scripts/validate-arazzo.py](../../../scripts/validate-arazzo.py))

Uses Spectral CLI with Arazzo ruleset:

```bash
npx @stoplight/spectral-cli lint docs/reference/apis/tmi.arazzo.yaml \
  --format stylish
```

**Checks**:
- Arazzo v1.0.0 specification compliance
- Workflow description requirements
- Step success criteria presence
- TMI custom rules (OAuth prerequisites, authentication chains)

### Configuration Files

#### [.spectral.yaml](../../../.spectral.yaml)

Spectral linting configuration:

```yaml
extends:
  - "spectral:oas"
  - "spectral:arazzo"

rules:
  arazzo-workflow-description: error
  arazzo-step-successCriteria: error

  tmi-oauth-prerequisite:
    description: "Authenticated workflows should reference OAuth authentication"
    severity: warn
```

#### [package.json](../../../package.json)

NPM dependencies and convenience scripts:

```json
{
  "devDependencies": {
    "@redocly/cli": "^1.25.0",
    "@stoplight/spectral-cli": "^6.11.0"
  },
  "scripts": {
    "arazzo:scaffold": "redocly generate-arazzo docs/reference/apis/tmi-openapi.json --output-file docs/reference/apis/arazzo/scaffolds/base-scaffold.arazzo.yaml",
    "arazzo:lint": "spectral lint docs/reference/apis/tmi.arazzo.yaml"
  }
}
```

## Workflow Patterns

### OAuth PKCE Flow (RFC 7636)

The enhancement script implements complete PKCE support:

```yaml
workflows:
  - workflowId: oauth_pkce_authentication
    summary: OAuth 2.0 Authorization Code Flow with PKCE
    steps:
      - stepId: generate_pkce_parameters
        description: Generate code_verifier and code_challenge
        outputs:
          code_verifier: "random_128_char_string"
          code_challenge: "base64url(sha256(code_verifier))"

      - stepId: authorization_request
        operationPath: /oauth2/authorize
        parameters:
          - name: code_challenge
            in: query
            value: $steps.generate_pkce_parameters.outputs.code_challenge
          - name: code_challenge_method
            in: query
            value: S256

      - stepId: oauth_token_exchange
        operationPath: /oauth2/token
        requestBody:
          code_verifier: $steps.generate_pkce_parameters.outputs.code_verifier
        outputs:
          access_token: $.access_token
          refresh_token: $.refresh_token
```

### Prerequisite Chains

Workflows automatically include dependency relationships:

```yaml
workflows:
  - workflowId: threat_model_crud
    steps:
      - stepId: oauth_token_exchange
        dependsOn: []  # No prerequisites

      - stepId: create_threat_model
        dependsOn: [oauth_token_exchange]

      - stepId: create_diagram
        dependsOn: [create_threat_model]

      - stepId: start_collaboration
        dependsOn: [create_diagram]
```

### Resource Hierarchies

The system understands TMI resource relationships:

```
threat_model (parent)
├── diagram (child)
│   └── collaboration session
├── threat (child)
├── document (child)
└── note (child)
```

Each child resource step automatically depends on parent resource creation.

## Makefile Integration

### Available Targets

| Target | Description |
|--------|-------------|
| `generate-arazzo` | Full pipeline: scaffold → enhance → validate |
| `arazzo-install` | Install Redocly CLI and Spectral (npm dependencies) |
| `arazzo-scaffold` | Generate base scaffold with Redocly |
| `arazzo-enhance` | Enhance scaffold with TMI workflows |
| `validate-arazzo` | Validate generated Arazzo specs |
| `arazzo-all` | Install + generate (complete setup) |

### Execution Flow

```
make generate-arazzo
├── arazzo-scaffold
│   ├── arazzo-install (pnpm install)
│   └── scripts/generate-arazzo-scaffold.sh
│       └── npx @redocly/cli generate-arazzo
├── arazzo-enhance
│   └── uv run scripts/enhance-arazzo-with-workflows.py
└── validate-arazzo
    └── uv run scripts/validate-arazzo.py
```

## Validation Results

The current Arazzo generation pipeline produces specifications that pass validation:

- **Status**: ✅ Passing (0 errors)
- **Workflows**: 150 complete workflows
- **Steps**: 211 total workflow steps
- **Warnings**: 132 warnings (66 YAML + 66 JSON) about preferring operationId over operationPath
  - These warnings are expected and acceptable
  - Both operationPath and operationId are valid per Arazzo v1.0.1 spec
  - operationId is preferred but requires more complex implementation

### Validation Command

```bash
make validate-arazzo
```

This runs Spectral CLI validation against both YAML and JSON outputs using the ruleset defined in [.spectral.yaml](.spectral.yaml).

## Troubleshooting

### Common Issues

#### 1. Network Timeouts (PyPI/NPM)

**Symptom**: `error: Failed to fetch: https://pypi.org/simple/pyyaml/`

**Cause**: Network connectivity issues or firewall blocking PyPI/NPM

**Solutions**:
- Check network connectivity: `curl -I https://pypi.org/`
- Configure proxy if behind corporate firewall
- Wait and retry if PyPI is experiencing downtime
- Use cached dependencies if available

#### 2. Missing Dependencies

**Symptom**: `ModuleNotFoundError: No module named 'yaml'`

**Cause**: Running script directly with `python3` instead of `uv run`

**Solution**: Always use `uv run scripts/enhance-arazzo-with-workflows.py` to automatically manage dependencies

#### 3. Scaffold Not Found

**Symptom**: `FileNotFoundError: docs/reference/apis/arazzo/scaffolds/base-scaffold.arazzo.yaml`

**Solution**: Run `make arazzo-scaffold` first to generate the base scaffold

#### 4. Duplicate stepId Errors

**Symptom**: `arazzo-workflow-stepId-unique` errors about duplicate stepIds

**Cause**: Multiple workflows using the same step identifier

**Solution**: Fixed in enhancement script v2 by adding workflow-specific prefixes to stepIds

#### 5. Invalid Runtime Expression Errors

**Symptom**: Runtime expression validation failures

**Cause**: Incorrect syntax for Arazzo runtime expressions (e.g., using curly braces)

**Solution**: Fixed in enhancement script v2 - runtime expressions use format `$steps.stepId.outputs.field` without curly braces

#### 6. Validation Errors (Legacy)

**Note**: The following issues have been resolved in the current version:

**Symptom**: Spectral reports errors in generated Arazzo

**Previous Issues** (now fixed):
- ~~Missing `description` fields~~ → Auto-generated by `_sanitize_scaffold()`
- ~~Invalid `operationPath`~~ → Fixed by removing inline parameters/requestBody
- ~~Missing `successCriteria`~~ → Added automatically by enhancement script
- ~~Duplicate stepIds~~ → Fixed with workflow-specific prefixing
- ~~Invalid runtime expressions~~ → Fixed with correct Arazzo v1.0.1 syntax

## Future Enhancements

### Planned Features

1. **Itarazzo Integration**: Workflow execution testing
   - Automated workflow playback
   - Response validation
   - Integration test generation

2. **Additional Workflows**: Expand coverage
   - Admin operations (user management, system config)
   - Reporting workflows (analytics, export)
   - Batch operations

3. **Enhanced Validation**:
   - Runtime parameter validation
   - Circular dependency detection
   - Coverage analysis (% of OpenAPI operations used)

4. **Documentation Generation**:
   - Mermaid sequence diagrams from workflows
   - API client code generation
   - Postman collection export

## References

- [Arazzo Specification v1.0.0](https://www.openapis.org/arazzo-specification)
- [RFC 7636 - PKCE](https://datatracker.ietf.org/doc/html/rfc7636)
- [Redocly CLI Documentation](https://redocly.com/docs/cli/)
- [Spectral Documentation](https://stoplight.io/open-source/spectral)
- [TMI OpenAPI Specification](tmi-openapi.json)
- [TMI API Workflows](api-workflows.json)

## Maintenance

### Updating Workflows

When adding new API endpoints:

1. Update `docs/reference/apis/tmi-openapi.json`
2. Update `docs/reference/apis/api-workflows.json` with new patterns
3. Run `make generate-arazzo` to regenerate specifications
4. Commit both OpenAPI and generated Arazzo files

### Version Control

Generated Arazzo files are committed to the repository to provide:
- Workflow documentation without requiring generation tools
- Diff visibility when API workflows change
- CI/CD integration for workflow testing (future)

**Recommended**: Regenerate after any OpenAPI changes to keep workflows synchronized.
