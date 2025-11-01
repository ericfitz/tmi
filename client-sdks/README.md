# TMI Client SDKs

## Location

The TMI client SDK source code is located in:

**[docs/reference/apis/sdks-generated/](../docs/reference/apis/sdks-generated/)**

## Available SDKs

All client SDKs are auto-generated from the TMI OpenAPI 3.0 specification using SwaggerHub:

- **[Go SDK](../docs/reference/apis/sdks-generated/go-client-generated/)** - Go client library for TMI API integration
- **[Java SDK](../docs/reference/apis/sdks-generated/java-client-generated/)** - Java client with Maven/Gradle support
- **[JavaScript SDK](../docs/reference/apis/sdks-generated/javascript-client-generated/)** - JavaScript/Node.js client library
- **[Python SDK](../docs/reference/apis/sdks-generated/python-client-generated/)** - Python client with pip packaging

## SDK Features

Each SDK includes:

- **Auto-generated client code** from the OpenAPI specification
- **Authentication helpers** for OAuth 2.0 Implicit Flow + JWT
- **Type-safe request/response models** matching the API schema
- **Documentation and usage examples** for all endpoints
- **Build configuration files** for the respective language/platform

## Documentation

For complete API documentation and SDK usage guidance, see:

- **[API Specifications](../docs/reference/apis/README.md)** - Overview of all API documentation and SDKs
- **[Client Integration Guide](../docs/developer/integration/client-integration-guide.md)** - How to integrate with TMI APIs
- **[OAuth Integration](../docs/developer/setup/oauth-integration.md)** - Authentication setup

## Regeneration

The SDKs are generated from the OpenAPI specification at [docs/reference/apis/tmi-openapi.json](../docs/reference/apis/tmi-openapi.json).

**To regenerate SDKs:**

1. Update the source OpenAPI specification: `docs/reference/apis/tmi-openapi.json`
2. Validate changes: `make validate-openapi`
3. Upload to SwaggerHub and regenerate client SDKs
4. Download and replace the SDK directories in `docs/reference/apis/sdks-generated/`
5. Commit all changes together

## Important Note

**Do not modify the generated SDK code directly.** All SDK code is machine-generated from the OpenAPI specification. Any manual changes will be lost when the SDKs are regenerated.

To make changes to the SDKs:
1. Update the source OpenAPI specification
2. Regenerate the SDKs via SwaggerHub
3. Test the updated SDKs with your application
