# TMI Development Setup

<!-- Migrated to wiki: Getting-Started-with-Development.md on 2025-01-24 -->
<!-- This file is maintained for reference. See wiki for current documentation. -->

## Quick Start

```bash
# Start development environment
make start-dev
```

This will:

1. Start PostgreSQL & Redis containers via Docker
2. Wait for database to be ready
3. Run database migrations
4. Start the TMI server with development configuration on port 8080

## Configuration

TMI uses YAML configuration files with environment variable overrides.

**Configuration files:**

- `config-development.yml` - Development configuration (copy from `config-example.yml` on first setup)
- `config-production.yml` - Production configuration template
- `config-example.yml` - Example configuration with all options documented

**Note:** Configuration files are not auto-generated. Copy `config-example.yml` to `config-development.yml` and customize as needed.

**OAuth credentials are stored directly in `config-development.yml` for development convenience.**

## OAuth Setup

For authentication, you can use the built-in TMI test provider (no configuration required) or configure external OAuth providers:

### TMI Test Provider (Development)

The built-in TMI OAuth provider is enabled by default in development builds. No configuration required:

```bash
# Use TMI test provider with random user
curl "http://localhost:8080/oauth2/authorize?idp=tmi"

# Use TMI test provider with specific user (login_hint)
curl "http://localhost:8080/oauth2/authorize?idp=tmi&login_hint=alice"
```

### Google OAuth (Optional)

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create OAuth 2.0 credentials
3. Add redirect URI: `http://localhost:8080/oauth2/callback`
4. Update the OAuth credentials in `config-development.yml`

### GitHub OAuth (Optional)

1. Go to GitHub Settings -> Developer settings -> OAuth Apps
2. Create new OAuth App with callback: `http://localhost:8080/oauth2/callback`
3. Update the OAuth credentials in `config-development.yml`

## Database Containers

Development uses Docker containers:

```bash
make start-database    # Start PostgreSQL only
make start-redis       # Start Redis only
```

**Connection details:**

- PostgreSQL: `localhost:5432`, user: `tmi_dev`, password: `dev123`, database: `tmi_dev`
- Redis: `localhost:6379`, no password

## Available Commands

```bash
make start-dev          # Start development server
make build-server       # Build production binary
make test-unit          # Run unit tests
make test-integration   # Run integration tests
make lint               # Run linter
make status             # Check status of all services
make clean-everything   # Clean up all containers and processes
```

## Production Deployment

For production, use:

```bash
# Build optimized binary
make build-server

# Deploy with production config
./bin/tmiserver --config=config-production.yml
```

Set production environment variables:

- `JWT_SECRET` - Secure random key (32+ characters, cryptographically random)
- `POSTGRES_PASSWORD` - Database password
- OAuth client credentials for your production domain

See [Deployment Guide](../../operator/deployment/deployment-guide.md) for complete production setup instructions.
