# TMI Documentation

This directory contains comprehensive documentation for the TMI (Threat Modeling Interface) project, organized by audience and purpose.

## Directory Structure

### [agent/](agent/) - AI Agent Context Documentation
Documentation primarily intended to give context to AI agents working on the TMI project.
<!-- NEEDS-REVIEW: agent/ directory exists but contains only .DS_Store - no actual documentation files -->

### [developer/](developer/) - Development Documentation
Everything developers need to build, test, and integrate with the TMI server.

### [operator/](operator/) - Operations Documentation
Deployment, operations, and troubleshooting guidance for running TMI in production.

### [reference/](reference/) - Reference Materials & Architecture
Pure reference materials, specifications, and architectural documentation.

## Getting Started by Role

### For Developers
<!-- NEEDS-REVIEW: developer/setup/development-setup.md does not exist - see wiki Getting-Started-with-Development instead -->
Start with [developer/setup/README.md](developer/setup/README.md) for local development environment setup, or see the [wiki documentation](https://github.com/ericfitz/tmi/wiki/Getting-Started-with-Development).

### For DevOps/SREs
Begin with [operator/deployment/deployment-guide.md](operator/deployment/deployment-guide.md) for production deployment.

### For Integration Teams
<!-- NEEDS-REVIEW: developer/integration/client-integration-guide.md does not exist -->
Review [developer/integration/README.md](developer/integration/README.md) for client integration patterns, or see the [wiki API Integration guide](https://github.com/ericfitz/tmi/wiki/API-Integration).

### For AI Agents
<!-- NEEDS-REVIEW: agent/ directory has no content - AI agent context is in CLAUDE.md at project root -->
Context and instructions are available in the project root `CLAUDE.md` file.

## Documentation Conventions

- **File Naming**: All documentation uses `kebab-case.md` naming convention
- **Cross-References**: Links are maintained between related documents
- **Audience-Focused**: Each directory serves a specific audience with clear purpose
- **Comprehensive Coverage**: Every aspect of TMI development and operations is documented

## Quick Reference

### Core Setup Documents
<!-- NEEDS-REVIEW: developer/setup/development-setup.md does not exist -->
<!-- NEEDS-REVIEW: developer/setup/oauth-integration.md does not exist -->
- [Developer Setup README](developer/setup/README.md)
- [Deployment Guide](operator/deployment/deployment-guide.md)

### Testing & Quality
- [Testing Guide](developer/testing/README.md) - Comprehensive testing documentation
<!-- NEEDS-REVIEW: developer/testing/coverage-reporting.md does not exist -->
- [WebSocket Testing](developer/testing/websocket-testing.md)
- [CATS Public Endpoints](developer/testing/cats-public-endpoints.md)
- [CATS OAuth False Positives](developer/testing/cats-oauth-false-positives.md)

### Client Integration
<!-- NEEDS-REVIEW: developer/integration/client-integration-guide.md does not exist -->
- [OAuth Client Integration](developer/integration/client-oauth-integration.md)
- [WebSocket Integration](developer/integration/client-websocket-integration-guide.md)
- [Webhook Subscriptions](developer/integration/webhook-subscriptions.md)

### Operations & Database
- [Database Operations](operator/database/postgresql-operations.md)
<!-- NEEDS-REVIEW: operator/database/postgresql-schema.md does not exist - see wiki Database-Schema-Reference -->
<!-- NEEDS-REVIEW: operator/database/redis-schema.md does not exist - see wiki Configuration-Reference for Redis config -->

## Contributing to Documentation

When adding new documentation:

1. Choose the appropriate directory based on primary audience
2. Use descriptive, hyphenated filenames
3. Include comprehensive README updates
4. Add cross-references to related documents
5. Follow the established directory structure

For questions about documentation organization or to suggest improvements, please create an issue in the project repository.

---

## Verification Summary

**Verified on**: 2026-01-24

**Verified Items:**
- Directory structure: All 4 directories exist (agent, developer, operator, reference)
- Deployment guide: `operator/deployment/deployment-guide.md` - EXISTS
- Testing README: `developer/testing/README.md` - EXISTS
- WebSocket testing: `developer/testing/websocket-testing.md` - EXISTS
- CATS docs: Both `cats-public-endpoints.md` and `cats-oauth-false-positives.md` - EXIST
- OAuth integration: `developer/integration/client-oauth-integration.md` - EXISTS
- WebSocket integration: `developer/integration/client-websocket-integration-guide.md` - EXISTS
- Webhook subscriptions: `developer/integration/webhook-subscriptions.md` - EXISTS
- PostgreSQL operations: `operator/database/postgresql-operations.md` - EXISTS

**Broken References (marked with NEEDS-REVIEW):**
- `developer/setup/development-setup.md` - Does not exist (content in wiki)
- `developer/integration/client-integration-guide.md` - Does not exist (content in wiki)
- `developer/setup/oauth-integration.md` - Does not exist
- `developer/testing/coverage-reporting.md` - Does not exist
- `operator/database/postgresql-schema.md` - Does not exist (content in wiki)
- `operator/database/redis-schema.md` - Does not exist
- `agent/` directory - Empty (AI context is in CLAUDE.md)

**Migration Note**: Primary documentation is now in the GitHub wiki. This file serves as a local index pointing to remaining docs in this directory.
