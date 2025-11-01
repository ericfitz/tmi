# API Specifications & Reference

This directory contains the TMI OpenAPI 3.0 specification and all machine-generated artifacts derived from it using SwaggerHub.

## Purpose

This directory serves as the single source of truth for the TMI API specification and houses all generated documentation, client SDKs, and specification variants produced by SwaggerHub.

## Directory Structure

### Source Specification

- **`tmi-openapi.json`** - The authoritative OpenAPI 3.0.3 specification (version 1.0.0) for the TMI REST API
- **`tmi-asyncapi.yml`** - AsyncAPI specification for the WebSocket collaboration protocol
- **`api-workflows.json`** - API workflow definitions and integration patterns

### Generated Content

All subdirectories contain machine-generated artifacts from SwaggerHub based on the `tmi-openapi.json` v1.0.0 specification:

#### Documentation Artifacts

- **`dynamic-html-documentation-generated/`** - Interactive HTML documentation with dynamic examples
- **`html-documentation-generated/`** - Static HTML documentation (format variant 1)
- **`html2-documentation-generated/`** - Static HTML documentation (format variant 2)

#### Client SDKs

**`sdks-generated/`** contains client libraries for multiple programming languages:

- **`go-client-generated/`** - Go SDK for TMI API integration
- **`java-client-generated/`** - Java SDK with Maven/Gradle support
- **`javascript-client-generated/`** - JavaScript/Node.js SDK
- **`python-client-generated/`** - Python SDK with pip packaging

Each SDK includes:
- Auto-generated client code from the OpenAPI specification
- Authentication helpers (OAuth 2.0 Implicit Flow + JWT)
- Type-safe request/response models
- Documentation and usage examples
- Build configuration files

#### OpenAPI Specification Variants

**`openapi-specifications-generated/`** contains processed versions of the source specification:

- **`tmi-openapi-specification-1.0.0-resolved.json`** - All `$ref` references inlined (JSON format)
- **`tmi-openapi-specification-1.0.0-resolved.yaml`** - All `$ref` references inlined (YAML format)
- **`tmi-openapi-specification-1.0.0-unresolved.json`** - Original structure with `$ref` references (JSON format)
- **`tmi-openapi-specification-1.0.0-unresolved.yaml`** - Original structure with `$ref` references (YAML format)

**Resolved vs Unresolved:**
- **Resolved** specifications inline all references, making them self-contained and easier for tools that don't handle `$ref` resolution
- **Unresolved** specifications preserve the modular structure with `$ref` pointers, maintaining readability and reusability

## Using the Generated Content

### For API Consumers

1. **Quick Reference**: Open any of the HTML documentation directories in a browser for human-readable API docs
2. **Client Development**: Use the appropriate SDK from `sdks-generated/` for your programming language
3. **Tool Integration**: Use resolved specifications for tools that need self-contained schemas
4. **Validation**: Reference the source `tmi-openapi.json` for schema validation against the server

### For API Developers

1. **Source of Truth**: Always edit `tmi-openapi.json` - never modify generated artifacts
2. **Regeneration**: After updating the source specification, regenerate all artifacts via SwaggerHub
3. **Validation**: Use `make validate-openapi` to validate specification changes before committing
4. **Version Control**: Commit both source specifications and generated artifacts to track API evolution

## Related Documentation

### Implementation Guidance

- [Client Integration Guide](../../developer/integration/client-integration-guide.md) - Using these APIs
- [OAuth Integration](../../developer/setup/oauth-integration.md) - Authentication setup

### Testing and Quality

- [API Testing](../../developer/testing/api-integration-tests.md) - API testing procedures
- [Postman Testing](../../developer/testing/postman-comprehensive-testing.md) - Postman collection usage

### Operations and Deployment

- [Deployment Guide](../../operator/deployment/deployment-guide.md) - API deployment
- [Database Schema](../schemas/) - Data structures used by APIs

## Workflow

When making changes to the TMI API:

1. Update the source `tmi-openapi.json` specification
2. Validate the changes: `make validate-openapi`
3. Upload to SwaggerHub and regenerate all artifacts
4. Download and replace the contents of the generated directories
5. Commit all changes together to maintain consistency
6. Update the server implementation if the API contract changed

For detailed API endpoint documentation, request/response formats, and integration examples, consult the generated HTML documentation or the source OpenAPI specification.
