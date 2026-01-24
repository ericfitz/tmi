# API Specifications & Reference

<!-- Migrated from: docs/reference/apis/README.md on 2025-01-24 -->

This directory contains the TMI API specifications and workflow definitions.

## Purpose

This directory serves as the single source of truth for TMI API specifications, including REST APIs, WebSocket protocols, and workflow orchestration patterns.

## Files

### REST API Specification

- **`tmi-openapi.json`** - The authoritative OpenAPI 3.0.3 specification for the TMI REST API
  - Comprehensive REST API documentation
  - OAuth 2.0 authentication flows
  - Request/response schemas
  - Error handling patterns
  - Public endpoint markers for OAuth/OIDC/SAML endpoints

### WebSocket API Specification

- **`tmi-asyncapi.yml`** - AsyncAPI specification for the WebSocket collaboration protocol
  - Real-time diagram collaboration
  - Message schemas for session management
  - Event-driven communication patterns

### Workflow Specifications

- **`api-workflows.json`** - API workflow definitions and integration patterns
  - End-to-end workflow sequences
  - Prerequisite relationships between API calls
  - OAuth PKCE flow patterns
  - CRUD operation sequences

### Arazzo Workflow Specifications

- **`tmi.arazzo.yaml`** - Arazzo v1.0.0 workflow specification (human-readable YAML)
- **`tmi.arazzo.json`** - Arazzo v1.0.0 workflow specification (machine-readable JSON)
  - Generated from OpenAPI + workflow knowledge base
  - PKCE OAuth flow with code_verifier and code_challenge
  - 7 complete end-to-end workflows
  - Prerequisite mapping via dependsOn relationships
  - See `arazzo-generation.md` for details

## Using the Specifications

### For API Consumers

1. **REST API Reference**: Read `tmi-openapi.json` for endpoint details, schemas, and authentication requirements
2. **WebSocket Integration**: Consult `tmi-asyncapi.yml` for real-time collaboration protocol
3. **Workflow Patterns**: Review `api-workflows.json` or Arazzo specifications for complete integration sequences
4. **Client Development**: Use specifications to generate client SDKs or implement custom integrations

### For API Developers

1. **Source of Truth**: Always edit the source specification files directly
2. **Validation**: Use `make validate-openapi` and `make validate-arazzo` to validate changes
3. **Code Generation**: Run `make generate-api` to regenerate Go server code from OpenAPI
4. **Workflow Updates**: Run `make generate-arazzo` to regenerate Arazzo specifications from workflows

## Related Documentation

### Implementation Guidance

- [Client Integration Guide](../../developer/integration/client-integration-guide.md) - Using these APIs
- [OAuth Integration](../../developer/integration/client-oauth-integration.md) - Authentication setup
- [Arazzo Generation](arazzo-generation.md) - Workflow specification generation

### Testing and Quality

- [Integration Testing](../../developer/testing/integration-test-plan.md) - API testing procedures
- [Postman Testing](../../developer/testing/postman-test-implementation-tracker.md) - Postman collection usage
- [CATS Public Endpoints](../../developer/testing/cats-public-endpoints.md) - Public endpoint handling

### Operations and Deployment

- [Deployment Guide](../../operator/deployment/deployment-guide.md) - API deployment
- [Database Schema](../schemas/) - Data structures used by APIs

## Workflow

When making changes to the TMI API:

1. Update the appropriate source specification:
   - REST API: `tmi-openapi.json`
   - WebSocket: `tmi-asyncapi.yml`
   - Workflows: `api-workflows.json`
2. Validate the changes:
   - `make validate-openapi` (OpenAPI)
   - `make validate-arazzo` (Arazzo)
3. Regenerate derived artifacts:
   - `make generate-api` (Go server code)
   - `make generate-arazzo` (Arazzo workflows)
4. Test the changes with integration tests
5. Commit all changes together to maintain consistency
6. Update server implementation if the API contract changed

For detailed API endpoint documentation, request/response formats, and integration examples, consult the OpenAPI specification directly or use tools like Swagger UI or Redoc to render interactive documentation.

---

## Verification Summary

**Verified on 2025-01-24:**

### Files (All Verified)
- `tmi-openapi.json` - EXISTS
- `tmi-asyncapi.yml` - EXISTS
- `api-workflows.json` - EXISTS
- `tmi.arazzo.yaml` - EXISTS
- `tmi.arazzo.json` - EXISTS
- `arazzo-generation.md` - EXISTS

### Make Targets (All Verified in Makefile)
- `make validate-openapi` - VERIFIED (line 1380)
- `make validate-arazzo` - VERIFIED (line 1357)
- `make generate-api` - VERIFIED (line 209)
- `make generate-arazzo` - VERIFIED (line 1364)

### Related Documentation Links (All Verified)
- `client-integration-guide.md` - EXISTS at `docs/developer/integration/`
- `client-oauth-integration.md` - EXISTS at `docs/developer/integration/` (corrected from non-existent oauth-integration.md)
- `integration-test-plan.md` - EXISTS at `docs/developer/testing/` (corrected from non-existent api-integration-tests.md)
- `postman-test-implementation-tracker.md` - EXISTS at `docs/developer/testing/` (corrected from non-existent postman-comprehensive-testing.md)
- `cats-public-endpoints.md` - EXISTS at `docs/developer/testing/`
- `deployment-guide.md` - EXISTS at `docs/operator/deployment/`
- `schemas/` directory - EXISTS at `docs/reference/schemas/`

### Corrections Made
1. Changed `oauth-integration.md` to `client-oauth-integration.md` (file did not exist at original path)
2. Changed `api-integration-tests.md` to `integration-test-plan.md` (file did not exist)
3. Changed `postman-comprehensive-testing.md` to `postman-test-implementation-tracker.md` (file did not exist)
