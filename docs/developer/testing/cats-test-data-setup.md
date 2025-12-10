# CATS Test Data Setup Strategy

**Created:** 2025-12-09
**Purpose:** Eliminate false positives in CATS testing by pre-creating prerequisite objects
**Related:** [API Workflows](../../reference/apis/api-workflows.json), [CATS Remediation Plan](cats-remediation-plan.md)

## Problem Statement

CATS fuzzing currently reports errors when testing endpoints that require prerequisite objects. For example:
- Testing `GET /threat_models/{id}` fails with 404 when no threat model exists
- Testing `GET /threat_models/{id}/threats/{threat_id}` fails when parent threat model doesn't exist
- These are **false positives** - the API is working correctly, but prerequisites aren't met

## Goal

Pre-create a complete object hierarchy before CATS testing so that:
1. Every endpoint can be tested with valid parent objects
2. Path parameters reference actual existing objects
3. CATS can focus on fuzzing request bodies and headers, not object existence
4. We distinguish between real bugs and missing test data

## Dependency Analysis

Based on `api-workflows.json`, here's the complete object hierarchy:

```
threat_model (root)
├── threats
│   └── metadata
├── diagrams
│   ├── metadata
│   └── collaboration (session)
├── documents
│   └── metadata
├── assets
│   └── metadata
├── notes
│   └── metadata
├── repositories
│   └── metadata
└── metadata

addons (independent root)
└── invocations

webhooks (independent root)
└── deliveries

client_credentials (independent root)
```

## Pre-Creation Strategy

### Phase 1: Authentication (Already Implemented)

✅ **Current:** `cats-prepare-database.sh` creates admin user (charlie@test.tmi)

```bash
# Already implemented
./scripts/cats-prepare-database.sh
```

### Phase 2: Create Test Data Scaffold (NEW)

Create a new script: `scripts/cats-create-test-data.sh`

This script should:

1. **Authenticate** as charlie@test.tmi
2. **Create one of each object type** with stable, known IDs
3. **Store IDs** in a reference file for CATS to use
4. **Verify creation** before proceeding

### Test Data Structure

```json
{
  "version": "1.0.0",
  "created_at": "2025-12-09T13:45:00Z",
  "user": {
    "provider_user_id": "charlie",
    "provider": "test",
    "email": "charlie@test.tmi"
  },
  "objects": {
    "threat_model": {
      "id": "<uuid>",
      "name": "CATS Test Threat Model",
      "description": "Created by cats-create-test-data.sh for fuzzing"
    },
    "threat": {
      "id": "<uuid>",
      "threat_model_id": "<threat_model_id>",
      "name": "CATS Test Threat"
    },
    "diagram": {
      "id": "<uuid>",
      "threat_model_id": "<threat_model_id>",
      "diagram_type": "STRIDE"
    },
    "document": {
      "id": "<uuid>",
      "threat_model_id": "<threat_model_id>",
      "name": "CATS Test Document"
    },
    "asset": {
      "id": "<uuid>",
      "threat_model_id": "<threat_model_id>",
      "name": "CATS Test Asset"
    },
    "note": {
      "id": "<uuid>",
      "threat_model_id": "<threat_model_id>",
      "content": "CATS test note"
    },
    "repository": {
      "id": "<uuid>",
      "threat_model_id": "<threat_model_id>",
      "url": "https://github.com/example/test-repo"
    },
    "addon": {
      "id": "<uuid>",
      "name": "CATS Test Addon"
    },
    "webhook": {
      "id": "<uuid>",
      "url": "https://webhook.site/test"
    },
    "client_credential": {
      "id": "<uuid>",
      "name": "CATS Test Credential"
    },
    "metadata_keys": {
      "threat_metadata": "cats-test-key",
      "diagram_metadata": "cats-test-key",
      "document_metadata": "cats-test-key",
      "asset_metadata": "cats-test-key",
      "note_metadata": "cats-test-key",
      "repository_metadata": "cats-test-key",
      "threat_model_metadata": "cats-test-key"
    }
  }
}
```

## Implementation: cats-create-test-data.sh

```bash
#!/bin/bash
# cats-create-test-data.sh
# Creates prerequisite test data for CATS fuzzing

set -euo pipefail

# Configuration
SERVER="${SERVER:-http://localhost:8080}"
USER="${USER:-charlie}"
IDP="${IDP:-test}"
OAUTH_STUB_URL="http://localhost:8079"
OUTPUT_FILE="cats-test-data.json"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >&2
}

error() {
    echo "[ERROR] $1" >&2
    exit 1
}

# 1. Start OAuth stub if not running
if ! pgrep -f "oauth-client-callback-stub" > /dev/null; then
    log "Starting OAuth stub..."
    make start-oauth-stub &
    sleep 3
fi

# 2. Authenticate and get JWT token
log "Authenticating as ${USER}@${IDP}.tmi..."

# Use oauth stub to get token
FLOW_RESPONSE=$(curl -s -X POST "${OAUTH_STUB_URL}/flows/start" \
    -H "Content-Type: application/json" \
    -d "{\"userid\": \"${USER}\", \"idp\": \"${IDP}\", \"server\": \"${SERVER}\"}")

FLOW_ID=$(echo "${FLOW_RESPONSE}" | jq -r '.flow_id')

# Poll for completion
for i in {1..30}; do
    STATUS=$(curl -s "${OAUTH_STUB_URL}/flows/${FLOW_ID}" | jq -r '.status')
    if [ "${STATUS}" = "completed" ]; then
        break
    fi
    sleep 1
done

# Get token
JWT_TOKEN=$(curl -s "${OAUTH_STUB_URL}/flows/${FLOW_ID}" | jq -r '.tokens.access_token')

if [ -z "${JWT_TOKEN}" ] || [ "${JWT_TOKEN}" = "null" ]; then
    error "Failed to get JWT token"
fi

log "Successfully authenticated"

# 3. Create threat model
log "Creating threat model..."
TM_RESPONSE=$(curl -s -X POST "${SERVER}/threat_models" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "CATS Test Threat Model",
        "description": "Created by cats-create-test-data.sh for fuzzing",
        "version": "1.0"
    }')

TM_ID=$(echo "${TM_RESPONSE}" | jq -r '.id')
log "Created threat model: ${TM_ID}"

# 4. Create threat
log "Creating threat..."
THREAT_RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/threats" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "CATS Test Threat",
        "description": "Test threat for CATS fuzzing",
        "severity": "high",
        "status": "identified"
    }')

THREAT_ID=$(echo "${THREAT_RESPONSE}" | jq -r '.id')
log "Created threat: ${THREAT_ID}"

# 5. Create diagram
log "Creating diagram..."
DIAGRAM_RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/diagrams" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "title": "CATS Test Diagram",
        "diagram_type": "STRIDE",
        "version": 1
    }')

DIAGRAM_ID=$(echo "${DIAGRAM_RESPONSE}" | jq -r '.id')
log "Created diagram: ${DIAGRAM_ID}"

# 6. Create document
log "Creating document..."
DOC_RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/documents" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "CATS Test Document",
        "content": "Test document content",
        "content_type": "text/plain"
    }')

DOC_ID=$(echo "${DOC_RESPONSE}" | jq -r '.id')
log "Created document: ${DOC_ID}"

# 7. Create asset
log "Creating asset..."
ASSET_RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/assets" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "CATS Test Asset",
        "description": "Test asset for CATS fuzzing",
        "asset_type": "application"
    }')

ASSET_ID=$(echo "${ASSET_RESPONSE}" | jq -r '.id')
log "Created asset: ${ASSET_ID}"

# 8. Create note
log "Creating note..."
NOTE_RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/notes" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "content": "CATS test note for fuzzing"
    }')

NOTE_ID=$(echo "${NOTE_RESPONSE}" | jq -r '.id')
log "Created note: ${NOTE_ID}"

# 9. Create repository
log "Creating repository..."
REPO_RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/repositories" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "url": "https://github.com/example/cats-test-repo",
        "branch": "main",
        "type": "git"
    }')

REPO_ID=$(echo "${REPO_RESPONSE}" | jq -r '.id')
log "Created repository: ${REPO_ID}"

# 10. Create addon (admin only - charlie has admin privileges)
log "Creating addon..."
ADDON_RESPONSE=$(curl -s -X POST "${SERVER}/addons" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "CATS Test Addon",
        "webhook_id": "'"${WEBHOOK_ID}"'",
        "threat_model_id": "'"${TM_ID}"'"
    }')

ADDON_ID=$(echo "${ADDON_RESPONSE}" | jq -r '.id')
log "Created addon: ${ADDON_ID}"

# 11. Create webhook subscription
log "Creating webhook..."
WEBHOOK_RESPONSE=$(curl -s -X POST "${SERVER}/webhooks/subscriptions" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "url": "https://webhook.site/cats-test",
        "events": ["threat_model.created", "threat.created"],
        "description": "CATS test webhook"
    }')

WEBHOOK_ID=$(echo "${WEBHOOK_RESPONSE}" | jq -r '.id')
log "Created webhook: ${WEBHOOK_ID}"

# 12. Create client credential
log "Creating client credential..."
CLIENT_CRED_RESPONSE=$(curl -s -X POST "${SERVER}/client-credentials" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
        "name": "CATS Test Credential",
        "description": "Test credential for CATS fuzzing"
    }')

CLIENT_CRED_ID=$(echo "${CLIENT_CRED_RESPONSE}" | jq -r '.id')
log "Created client credential: ${CLIENT_CRED_ID}"

# 13. Create metadata entries
log "Creating metadata entries..."

# Threat metadata
curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/threats/${THREAT_ID}/metadata/cats-test-key" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"value": "test-value"}' > /dev/null

# Diagram metadata
curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/diagrams/${DIAGRAM_ID}/metadata/cats-test-key" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"value": "test-value"}' > /dev/null

# Document metadata
curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/documents/${DOC_ID}/metadata/cats-test-key" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"value": "test-value"}' > /dev/null

# Asset metadata
curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/assets/${ASSET_ID}/metadata/cats-test-key" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"value": "test-value"}' > /dev/null

# Note metadata
curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/notes/${NOTE_ID}/metadata/cats-test-key" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"value": "test-value"}' > /dev/null

# Repository metadata
curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/repositories/${REPO_ID}/metadata/cats-test-key" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"value": "test-value"}' > /dev/null

# Threat model metadata
curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/metadata/cats-test-key" \
    -H "Authorization: Bearer ${JWT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"value": "test-value"}' > /dev/null

log "Created all metadata entries"

# 14. Write reference file
log "Writing test data reference to ${OUTPUT_FILE}..."

cat > "${OUTPUT_FILE}" <<EOF
{
  "version": "1.0.0",
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "server": "${SERVER}",
  "user": {
    "provider_user_id": "${USER}",
    "provider": "${IDP}",
    "email": "${USER}@${IDP}.tmi"
  },
  "objects": {
    "threat_model": {
      "id": "${TM_ID}",
      "name": "CATS Test Threat Model"
    },
    "threat": {
      "id": "${THREAT_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "diagram": {
      "id": "${DIAGRAM_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "document": {
      "id": "${DOC_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "asset": {
      "id": "${ASSET_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "note": {
      "id": "${NOTE_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "repository": {
      "id": "${REPO_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "addon": {
      "id": "${ADDON_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "webhook": {
      "id": "${WEBHOOK_ID}"
    },
    "client_credential": {
      "id": "${CLIENT_CRED_ID}"
    },
    "metadata_key": "cats-test-key"
  }
}
EOF

log "✅ Test data creation complete!"
log ""
log "Created objects:"
log "  - Threat Model: ${TM_ID}"
log "  - Threat: ${THREAT_ID}"
log "  - Diagram: ${DIAGRAM_ID}"
log "  - Document: ${DOC_ID}"
log "  - Asset: ${ASSET_ID}"
log "  - Note: ${NOTE_ID}"
log "  - Repository: ${REPO_ID}"
log "  - Addon: ${ADDON_ID}"
log "  - Webhook: ${WEBHOOK_ID}"
log "  - Client Credential: ${CLIENT_CRED_ID}"
log ""
log "Reference file: ${OUTPUT_FILE}"
log ""
log "Next step: Run CATS fuzzing with: make cats-fuzz"
```

## Integration with CATS

### Option 1: CATS Reference Data (Recommended)

CATS supports loading reference data from a file to use in path parameters.

**Update `run-cats-fuzz.sh`:**

```bash
# After authentication, create test data
log "Creating test data scaffold..."
./scripts/cats-create-test-data.sh

# Pass reference data to CATS
CATS_REFDATA_FILE="cats-test-data.json"

cats --contract="${OPENAPI_SPEC}" \
     --server="${SERVER}" \
     --headers="Authorization=Bearer ${JWT_TOKEN}" \
     --refData="${CATS_REFDATA_FILE}" \
     --httpMethods="${HTTP_METHODS}" \
     ${PATH_RESTRICTION}
```

CATS will then use IDs from the reference file when testing endpoints like:
- `GET /threat_models/{threat_model_id}` → uses `threat_model.id` from refData
- `GET /threat_models/{threat_model_id}/threats/{threat_id}` → uses both IDs from refData

### Option 2: CATS Custom Fuzzer

Create a custom fuzzer that injects real IDs before testing.

### Option 3: CATS Path Parameter Overrides

Use CATS `--params` flag to override specific path parameters:

```bash
cats --contract="${OPENAPI_SPEC}" \
     --server="${SERVER}" \
     --headers="Authorization=Bearer ${JWT_TOKEN}" \
     --params="threat_model_id=${TM_ID},threat_id=${THREAT_ID},diagram_id=${DIAGRAM_ID}"
```

## Makefile Integration

Add new make targets:

```makefile
.PHONY: cats-create-test-data cats-fuzz-with-data

## Create test data for CATS fuzzing
cats-create-test-data:
	@./scripts/cats-create-test-data.sh

## Run CATS fuzzing with pre-created test data
cats-fuzz-with-data: cats-create-test-data
	@make cats-fuzz
```

## Updated Workflow

```bash
# 1. Start server
make start-dev

# 2. Grant admin privileges (one-time)
./scripts/cats-prepare-database.sh

# 3. Create test data scaffold
./scripts/cats-create-test-data.sh

# 4. Run CATS fuzzing (now with valid objects)
make cats-fuzz

# 5. Parse and analyze results
make analyze-cats-results
```

## Expected Impact

**Before (current state):**
- ~231 404 errors from missing parent objects
- False positives on Happy Path tests
- Can't distinguish between "endpoint broken" vs "no test data"

**After (with test data):**
- ✅ All path parameters reference real objects
- ✅ 404 errors only indicate actual bugs (broken queries, wrong IDs)
- ✅ Happy Path tests validate actual functionality
- ✅ CATS focuses on fuzzing request bodies and headers

**Estimated reduction:** ~200-230 false positive errors eliminated

## Maintenance

**When to re-create test data:**
- After `make clean-everything` (database reset)
- Before each CATS run (safest approach)
- When adding new endpoints that require new object types

**Idempotency:**
- Script should be idempotent (safe to run multiple times)
- Check if objects exist before creating
- Update reference file with existing IDs if found

## Next Steps

1. ✅ Review this document for accuracy
2. ⏳ Implement `scripts/cats-create-test-data.sh`
3. ⏳ Update `scripts/run-cats-fuzz.sh` to use reference data
4. ⏳ Add Makefile targets
5. ⏳ Test with `make cats-fuzz-with-data`
6. ⏳ Compare results to baseline (should see ~200 fewer false positives)
7. ⏳ Document in CATS remediation plan

## References

- [API Workflows](../../reference/apis/api-workflows.json) - Complete dependency tree
- [CATS Documentation](https://github.com/Endava/cats) - Reference data format
- [cats-prepare-database.sh](../../../scripts/cats-prepare-database.sh) - Admin setup
- [run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) - CATS execution script
