# TMI Development Setup

## Quick Start

```bash
# Start development environment
make start-dev
```

This will:
1. Start PostgreSQL & Redis containers via Docker
2. Generate `config-development.yaml` if it doesn't exist
3. Start the TMI server with development configuration

## Configuration

TMI uses YAML configuration files with environment variable overrides.

**Generated files:**
- `config-development.yaml` - Development configuration
- `config-production.yaml` - Production template  
- `docker-compose.env` - Container environment variables

**Environment variables for secrets:**
```bash
TMI_DATABASE_POSTGRES_PASSWORD         # Database password (automatically set to 'postgres' for dev)
```

**OAuth credentials are stored directly in `config-development.yaml` for development convenience.**

## OAuth Setup

For authentication, configure OAuth applications:

### Google OAuth
1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create OAuth 2.0 credentials
3. Add redirect URI: `http://localhost:8080/auth/callback`
4. Update the OAuth credentials in `config-development.yaml`

### GitHub OAuth (Optional)
1. Go to GitHub Settings → Developer settings → OAuth Apps
2. Create new OAuth App with callback: `http://localhost:8080/auth/callback`
3. Set `TMI_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_ID` and `TMI_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET`

## Database Containers

Development uses Docker containers:

```bash
make start-dev-db      # Start PostgreSQL only
make start-dev-redis   # Start Redis only
```

**Connection details:**
- PostgreSQL: `localhost:5432`, user: `postgres`, password: `postgres`, database: `tmi`
- Redis: `localhost:6379`, no password

## Available Commands

```bash
make start-dev          # Start development server
make build-server       # Build production binary
make run-tests          # Run tests
make run-lint           # Run linter
make generate-config    # Generate config templates
```

## Production Deployment

For production, use:

```bash
# Build optimized binary
make build-server

# Deploy with production config
./bin/server --config=config-production.yaml
```

Set production environment variables:
- `TMI_AUTH_JWT_SECRET` - Secure random key
- `TMI_DATABASE_POSTGRES_PASSWORD` - Database password  
- OAuth client credentials for your production domain

See [DEPLOYMENT.md](DEPLOYMENT.md) for complete production setup instructions.