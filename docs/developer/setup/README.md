# Development Setup

This directory contains essential setup guides for TMI development environment.

## Files in this Directory

### [development-setup.md](development-setup.md)
**Primary setup guide** for local TMI development environment.

**Content includes:**
- Quick start commands (`make start-dev`)
- Database container setup (PostgreSQL & Redis)
- Configuration file generation
- Environment variables and secrets management
- Development workflow and available commands
- Production deployment preparation

**Key Features:**
- Automated Docker container management
- Hot reloading development server
- Database migration handling
- OAuth provider configuration

### [oauth-integration.md](oauth-integration.md)
**Comprehensive OAuth setup guide** for authentication integration.

**Content includes:**
- OAuth 2.0 implementation details
- Multi-provider support (Google, GitHub, Microsoft)
- Development vs production OAuth configuration
- OAuth application setup instructions
- Test provider configuration
- Authentication flow troubleshooting
- JWT token management
- Security best practices

**Supported Providers:**
- Google OAuth 2.0
- GitHub OAuth Apps
- Microsoft Azure AD
- Built-in test provider for development

### [automatic-versioning.md](automatic-versioning.md)
**Automatic semantic versioning system** for TMI releases.

**Content includes:**
- Conventional commit-based version incrementing
- Post-commit hook automation
- Version file format and management
- Major/minor/patch version rules

### [promtail-container.md](promtail-container.md)
**Promtail log shipping container setup** for centralized logging.

**Content includes:**
- Promtail container configuration
- Loki integration setup
- Log collection pipeline
- Docker Compose configuration
- Troubleshooting guide

## Getting Started

1. **Start with**: [development-setup.md](development-setup.md) for basic environment
2. **Then configure**: [oauth-integration.md](oauth-integration.md) for authentication

## Essential Commands

```bash
# Complete development environment
make start-dev

# Individual services  
make start-database     # PostgreSQL only
make start-redis       # Redis only

# Configuration
make generate-config   # Create config templates
```

## Prerequisites

### Required Software
- Go 1.25.3 or later
- Docker Desktop (for database containers)
- Make (for build automation)
- Git

### Development Dependencies
- PostgreSQL client tools (for database access)
- Redis CLI (for cache debugging)
- Web browser (for OAuth testing)

## Environment Overview

TMI development environment consists of:

- **TMI Server**: Go HTTP server with WebSocket support
- **PostgreSQL**: Primary data storage
- **Redis**: Session storage and caching
- **OAuth Providers**: Authentication services

## Configuration Management

### Configuration Files
- `config-development.yml` - Local development settings
- `config-production.yml` - Production template
- `.env.dev` - Environment-specific overrides

### Environment Variables
- `POSTGRES_PASSWORD` - Database password
- OAuth client credentials for each provider
- JWT signing secrets

## Quick Setup Validation

After setup, verify with:

```bash
# Check database connectivity
make test-integration name=TestDatabaseIntegration

# Verify OAuth configuration
curl http://localhost:8080/oauth2/providers

# Test full server functionality
make test-unit && make test-integration
```

## Next Steps

After completing setup:

1. Review [Testing Documentation](../testing/) for development testing
2. Explore [Integration Guides](../integration/) for client development
3. Consult [Operations Documentation](../../operator/) for deployment

## Troubleshooting

Common setup issues:

- **Docker containers fail**: Ensure Docker Desktop is running
- **Database connection errors**: Check container status with `docker ps`
- **OAuth errors**: Verify provider configuration and callback URLs
- **Port conflicts**: Default port 8080, configure alternatives if needed

For detailed troubleshooting, see the individual setup documents.