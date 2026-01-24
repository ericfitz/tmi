# Reference Materials & Architecture

<!-- Migrated from: docs/reference/README.md on 2025-01-24 -->

This directory contains pure reference materials, specifications, and architectural documentation for the TMI project.

## Purpose

Comprehensive reference documentation including system architecture, API specifications, database schemas, and technical specifications that serve as authoritative sources for TMI system design and implementation.

## Directory Structure

### 🏗️ [architecture/](architecture/) - System Architecture & Design
High-level system design, architectural decisions, and design patterns.

### 📊 [schemas/](schemas/) - Database & Data Schemas
Database schema definitions, data models, and schema evolution documentation.

### 🔌 [apis/](apis/) - API Specifications & Reference
OpenAPI specifications, API documentation, and integration references.

## Files in this Directory

### [collaboration-protocol.md](collaboration-protocol.md)
**Complete WebSocket collaboration protocol specification** for real-time diagram editing.

**Content includes:**
- Role definitions (Host, Participant, Presenter)
- Permission model (Reader, Writer)
- All message types with field definitions
- Session lifecycle (initiation, join, termination)
- Heartbeat and timeout mechanisms
- Deny list functionality
- Conflict detection and resolution
- Undo/redo stack mechanics
- Security considerations
- Message sequence diagrams for common operations

<!-- NEEDS-REVIEW: quota-management.md reference removed - file was previously migrated to docs/migrated/reference/quota-management.md -->

## Reference Categories

### Architecture Documentation
- **System Architecture**: Overall system design and component relationships
- **Service Architecture**: Microservice patterns and communication protocols  
- **Data Architecture**: Data flow and storage architecture
- **Security Architecture**: Authentication, authorization, and security patterns
- **Deployment Architecture**: Infrastructure and deployment patterns

### Schema Documentation
- **Database Schemas**: PostgreSQL table definitions and relationships
- **Cache Schemas**: Redis data structures and key patterns
- **API Schemas**: Request/response data structures and validation rules
- **Message Schemas**: WebSocket message formats and protocols
- **Configuration Schemas**: Configuration file structures and validation

### API Documentation
- **OpenAPI Specifications**: Complete API specifications in OpenAPI 3.0 format
- **REST API Reference**: HTTP endpoint documentation with examples
- **WebSocket API Reference**: Real-time API documentation and message formats
- **Authentication API**: OAuth and JWT authentication specifications
- **Client SDK Documentation**: Client library specifications and examples

## Key Reference Materials

### System Design
- **Component Diagrams**: Visual representations of system components
- **Sequence Diagrams**: Interaction patterns between system components
- **Data Flow Diagrams**: Data movement through the system
- **Architecture Decision Records (ADRs)**: Design decisions and rationale
- **Integration Patterns**: Common integration and communication patterns

### Technical Specifications
- **API Specifications**: Complete OpenAPI 3.0 specifications
- **Database Schema**: Complete PostgreSQL schema with relationships
- **Message Formats**: WebSocket message specifications
- **Configuration Formats**: YAML configuration file specifications
- **Security Specifications**: Authentication and authorization specifications

### Data Models
- **Entity Models**: Business entity definitions and relationships
- **Data Transfer Objects**: API request/response object specifications
- **Database Models**: Database table and column specifications
- **Cache Models**: Redis data structure and key pattern specifications
- **Message Models**: WebSocket message format specifications

## Reference Usage Patterns

### For Developers
- **API Integration**: Use API specifications for client development
- **Database Access**: Reference schema documentation for database operations
- **Architecture Understanding**: Review architectural diagrams and ADRs
- **Message Protocols**: Use WebSocket specifications for real-time integration
- **Configuration**: Reference configuration schemas for deployment setup

### For System Architects
- **Design Review**: Architecture documentation for design decisions
- **Integration Planning**: Reference integration patterns and specifications
- **Scalability Planning**: Architecture patterns for scaling decisions
- **Security Planning**: Security architecture and threat model references
- **Technology Selection**: Architectural context for technology decisions

### For Operations Teams
- **Deployment Planning**: Architecture documentation for infrastructure design
- **Configuration Management**: Configuration schema references for deployment
- **Monitoring Setup**: System architecture for monitoring point identification
- **Troubleshooting**: Architectural context for issue diagnosis
- **Capacity Planning**: Architecture understanding for scaling decisions

## Documentation Standards

### Architecture Documentation
- **Visual Diagrams**: Standardized diagrams using consistent notation
- **ADR Format**: Structured architecture decision records
- **Component Specifications**: Detailed component interface specifications
- **Integration Patterns**: Reusable integration pattern documentation
- **Design Principles**: Documented design principles and guidelines

### Schema Documentation
- **Schema Evolution**: Documented schema migration and versioning
- **Relationship Diagrams**: Visual entity relationship diagrams
- **Constraint Documentation**: Business rule and constraint specifications
- **Index Documentation**: Performance optimization through indexing strategy
- **Data Dictionary**: Complete field and table documentation

### API Documentation
- **OpenAPI Standards**: Complete OpenAPI 3.0 specification compliance
- **Example Requests**: Working examples for all API endpoints
- **Error Documentation**: Complete error response specifications
- **Authentication Documentation**: Detailed authentication flow specifications
- **Versioning Strategy**: API versioning and compatibility documentation

## Reference Quality Standards

### Accuracy Requirements
- **Up-to-date**: Documentation synchronized with implementation
- **Comprehensive**: Complete coverage of system components and interfaces
- **Validated**: Documentation validated against implementation
- **Versioned**: Version-controlled with clear change tracking
- **Reviewed**: Technical review process for accuracy and completeness

### Usability Requirements
- **Searchable**: Easy to search and navigate
- **Cross-referenced**: Linked to related documentation and implementation
- **Examples**: Working examples and usage patterns
- **Clear Structure**: Logical organization and clear hierarchy
- **Multiple Formats**: Available in multiple formats for different use cases

## Maintenance and Updates

### Documentation Lifecycle
- **Creation**: New reference documentation for new system components
- **Updates**: Regular updates to reflect system changes
- **Review**: Periodic review for accuracy and completeness
- **Archival**: Proper archival of obsolete documentation
- **Migration**: Documentation migration for system evolution

### Change Management
- **Version Control**: All reference documentation under version control
- **Change Tracking**: Clear tracking of changes and updates
- **Impact Analysis**: Assessment of documentation changes on system users
- **Notification**: Communication of significant documentation changes
- **Rollback**: Capability to rollback documentation changes if needed

## Related Documentation

### Implementation Guidance
- [Developer Documentation](../developer/) - Implementation guides using reference materials
- [Operations Documentation](../operator/) - Operational procedures based on architecture

### Context and Background
- [Integration Guides](../developer/integration/) - Client integration using API specifications

<!-- NEEDS-REVIEW: Agent Documentation link removed - docs/agent/ directory is empty (no .md files) -->

## Contributing to Reference Documentation

### Adding New Reference Material
1. **Identify Need**: Determine need for new reference documentation
2. **Select Category**: Choose appropriate directory (architecture/schemas/apis)
3. **Follow Standards**: Adhere to established documentation standards
4. **Include Examples**: Provide working examples and usage patterns
5. **Cross-Reference**: Link to related documentation and implementation
6. **Review Process**: Submit for technical review and validation

### Updating Existing Reference Material
1. **Assess Impact**: Understand impact of changes on system users
2. **Update Documentation**: Make accurate and complete updates
3. **Validate Changes**: Verify changes against current implementation
4. **Update Cross-References**: Update related documentation links
5. **Communicate Changes**: Notify stakeholders of significant updates

For questions about reference documentation or to contribute improvements, please create an issue in the project repository with clear specifications and use cases.

---

## Verification Summary (2025-01-24)

**Verified items:**
- `architecture/` directory exists with README.md and oauth-flow-diagrams.md
- `schemas/` directory exists with README.md
- `apis/` directory exists with tmi-openapi.json, tmi-asyncapi.yml, arazzo files, api-workflows.json, and more
- `collaboration-protocol.md` exists (40,837 bytes, detailed WebSocket protocol spec)
- `docs/developer/` directory exists with setup, testing, integration, and planning subdirectories
- `docs/operator/` directory exists with database and deployment documentation
- `docs/developer/integration/` directory exists with client-oauth-integration.md and client-websocket-integration-guide.md

**Issues corrected:**
1. Removed reference to `quota-management.md` - file was previously migrated to docs/migrated/reference/
2. Removed reference to `docs/agent/` - directory exists but contains no .md documentation files

**Wiki pages updated:**
- `/Users/efitz/Projects/tmi.wiki/Architecture-and-Design.md` - Added "Source Repository Reference Materials" section with directory structure and key reference files

**Migration details:**
- Original location: `docs/reference/README.md`
- New location: `docs/migrated/reference/README.md`
- Content integrated into wiki: Architecture-and-Design page