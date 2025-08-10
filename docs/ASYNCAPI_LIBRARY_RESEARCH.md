# Go AsyncAPI Library Research

## Overview
This research evaluates Go libraries for AsyncAPI v3.0 support and code generation capabilities for the TMI collaborative editing implementation.

## Libraries Evaluated

### 1. asyncapi-codegen (github.com/lerenn/asyncapi-codegen)
**Status**: ✅ **RECOMMENDED**

**AsyncAPI v3.0 Support**: ✅ **Full support**
- Contains `pkg/asyncapi/v3` package 
- Examples directory with v3 implementations
- Actively supports AsyncAPI 3.0.0 specification

**Features**:
- Complete code generation from AsyncAPI specs
- Generates AppController and UserController structures
- Message type definitions and validation
- Middleware support for message processing
- Built-in logging for operations and messages
- Key format conversion (snake, camel, kebab)
- Custom Go type extensions

**Integration**:
- Simple CLI: `asyncapi-codegen -i ./asyncapi.yaml -p <package> -o ./output.go`
- Clean integration with existing Go projects
- Supports WebSocket message handling

**Maintenance**: ✅ Actively maintained with recent updates

### 2. Watermill (github.com/ThreeDotsLabs/watermill)
**Status**: ⚠️ **Limited AsyncAPI Integration**

**AsyncAPI v3.0 Support**: ⚠️ **Template-based only**
- Has AsyncAPI template `@asyncapi/go-watermill-template` 
- Template works with AsyncAPI Generator
- No direct AsyncAPI v3.0 parsing/generation

**Features**:
- High-performance message streaming (hundreds of thousands msgs/sec)
- Multiple pub/sub protocols (Kafka, HTTP, Google Cloud Pub/Sub, NATS, RabbitMQ, etc.)
- Universal abstractions for event-driven architectures
- Middleware and plugin system

**WebSocket Support**: ⚠️ **HTTP support available, WebSocket unclear**
- Has HTTP pub/sub implementation
- WebSocket support not explicitly documented

**Use Case**: Better for runtime message handling than code generation

### 3. ogen (github.com/ogen-go/ogen) 
**Status**: ❌ **Not suitable**

**AsyncAPI v3.0 Support**: ❌ **OpenAPI only**
- Specifically designed for OpenAPI v3, not AsyncAPI
- No AsyncAPI parsing or generation capabilities

**WebSocket Support**: ❌ **Planned but not implemented**
- WebSocket support is on roadmap
- Currently REST/HTTP only

### 4. swaggo/swag
**Status**: ❌ **Not suitable**

**AsyncAPI v3.0 Support**: ❌ **Swagger/OpenAPI 2.0 only**
- Generates Swagger 2.0 documentation from Go annotations
- No AsyncAPI specification support
- Can convert to OpenAPI 3.0 but not AsyncAPI

## Recommendation

**Use asyncapi-codegen for Phase 0 implementation:**

1. **Full AsyncAPI v3.0 Support**: Only library with complete AsyncAPI v3.0 parsing and code generation
2. **Message Type Generation**: Automatically generates Go structs for all our message types
3. **Validation Support**: Built-in message validation based on schemas
4. **Clean Integration**: Simple CLI tool that integrates well with existing codebase
5. **Active Maintenance**: Recent updates and good community support

## Implementation Approach

### Phase 0 Integration Steps:
1. Install asyncapi-codegen: `go install github.com/lerenn/asyncapi-codegen/cmd/asyncapi-codegen@latest`
2. Generate Go types: `asyncapi-codegen -i tmi-asyncapi.yaml -p asyncapi -o api/asyncapi.gen.go`
3. Use generated types in WebSocket message handling
4. Leverage generated validation functions

### Benefits for TMI:
- **Type Safety**: All WebSocket message types are strongly typed
- **Validation**: Automatic message validation against AsyncAPI schema
- **Consistency**: Generated types match AsyncAPI specification exactly
- **Maintenance**: Schema changes automatically propagate to Go code
- **Documentation**: Code and schema stay synchronized

### Integration with Existing Code:
- Generated types can be used alongside existing Gin/Gorilla WebSocket setup
- Validation functions can be integrated into message processing pipeline
- No breaking changes to current architecture required

## Alternative: Manual Implementation

If code generation proves complex:
- **Hybrid approach**: Use AsyncAPI for documentation, manual Go structs for implementation
- **Schema validation**: Use AsyncAPI spec for runtime validation without code generation
- **Future migration**: Can always add code generation later without breaking changes

## Conclusion

asyncapi-codegen provides the best balance of AsyncAPI v3.0 support, code generation capabilities, and integration simplicity for the TMI collaborative editing implementation. The tool will significantly reduce boilerplate code and ensure type safety for all WebSocket message handling.