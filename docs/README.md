# TMI Documentation

This directory contains comprehensive documentation for the TMI (Threat Modeling Interface) project, organized by audience and purpose.

## Directory Structure

### 📋 [agent/](agent/) - AI Agent Context Documentation
Documentation primarily intended to give context to AI agents working on the TMI project.

### 🛠️ [developer/](developer/) - Development Documentation  
Everything developers need to build, test, and integrate with the TMI server.

### 🚀 [operator/](operator/) - Operations Documentation
Deployment, operations, and troubleshooting guidance for running TMI in production.

### 📖 [reference/](reference/) - Reference Materials & Architecture
Pure reference materials, specifications, and architectural documentation.

## Getting Started by Role

### For Developers
Start with [developer/setup/development-setup.md](developer/setup/development-setup.md) for local development environment setup.

### For DevOps/SREs
Begin with [operator/deployment/deployment-guide.md](operator/deployment/deployment-guide.md) for production deployment.

### For Integration Teams
Review [developer/integration/client-integration-guide.md](developer/integration/client-integration-guide.md) for client integration patterns.

### For AI Agents
Context and instructions are available in the [agent/](agent/) directory.

## Documentation Conventions

- **File Naming**: All documentation uses `kebab-case.md` naming convention
- **Cross-References**: Links are maintained between related documents
- **Audience-Focused**: Each directory serves a specific audience with clear purpose
- **Comprehensive Coverage**: Every aspect of TMI development and operations is documented

## Quick Reference

### Core Setup Documents
- [Development Environment Setup](developer/setup/development-setup.md)
- [OAuth Integration Guide](developer/setup/oauth-integration.md)
- [Deployment Guide](operator/deployment/deployment-guide.md)

### Testing & Quality
- [Testing Guide](developer/testing/README.md) - Comprehensive testing documentation
- [Coverage Reporting](developer/testing/coverage-reporting.md)
- [WebSocket Testing](developer/testing/websocket-testing.md)
- [CATS Public Endpoints](developer/testing/cats-public-endpoints.md)
- [CATS OAuth False Positives](developer/testing/cats-oauth-false-positives.md)

### Client Integration
- [Client Integration Guide](developer/integration/client-integration-guide.md)
- [OAuth Client Integration](developer/integration/client-oauth-integration.md)
- [WebSocket Integration](developer/integration/client-websocket-integration-guide.md)
- [Webhook Subscriptions](developer/integration/webhook-subscriptions.md)

### Operations & Database
- [Database Operations](operator/database/postgresql-operations.md)
- [Database Schema](operator/database/postgresql-schema.md)
- [Redis Schema](operator/database/redis-schema.md)

## Contributing to Documentation

When adding new documentation:

1. Choose the appropriate directory based on primary audience
2. Use descriptive, hyphenated filenames
3. Include comprehensive README updates
4. Add cross-references to related documents
5. Follow the established directory structure

For questions about documentation organization or to suggest improvements, please create an issue in the project repository.