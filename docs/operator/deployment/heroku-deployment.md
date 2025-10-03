# Heroku Deployment Guide

This guide explains how to deploy the TMI server to Heroku directly from your GitHub repository.

## Overview

The TMI server is deployed to Heroku using:
- **Procfile**: Specifies the command to run the tmiserver binary
- **.godir**: Tells Heroku to build only the tmiserver package (not other binaries)
- **app.json**: Defines the Heroku app configuration, environment variables, and required addons

## Prerequisites

1. **Heroku Account**: Sign up at [heroku.com](https://heroku.com)
2. **Heroku CLI**: Install from [devcenter.heroku.com/articles/heroku-cli](https://devcenter.heroku.com/articles/heroku-cli)
3. **GitHub Repository**: Your code must be pushed to a GitHub repository
4. **Git**: Ensure your local repository is connected to GitHub

## Quick Start Deployment

### Option 1: Deploy via Heroku Dashboard (Recommended for First Deployment)

1. **Login to Heroku Dashboard**
   - Navigate to [dashboard.heroku.com](https://dashboard.heroku.com)
   - Click "New" → "Create new app"

2. **Configure App**
   - **App name**: Choose a unique name (e.g., `my-tmi-server`)
   - **Region**: Choose US or Europe
   - Click "Create app"

3. **Connect to GitHub**
   - Go to the "Deploy" tab
   - Under "Deployment method", select "GitHub"
   - Click "Connect to GitHub" and authorize Heroku
   - Search for your repository and click "Connect"

4. **Enable Automatic Deploys** (Optional)
   - Under "Automatic deploys", select your branch (usually `main`)
   - Click "Enable Automatic Deploys"
   - This will automatically deploy when you push to the branch

5. **Manual Deploy**
   - Under "Manual deploy", select your branch
   - Click "Deploy Branch"
   - Wait for the build to complete

### Option 2: Deploy via Heroku CLI

1. **Login to Heroku**
   ```bash
   heroku login
   ```

2. **Create Heroku App**
   ```bash
   heroku create my-tmi-server
   ```

3. **Set Heroku Git Remote**
   ```bash
   heroku git:remote -a my-tmi-server
   ```

4. **Deploy**
   ```bash
   git push heroku main
   ```

### Option 3: Deploy Button (One-Click Deploy)

Add this button to your README.md:

```markdown
[![Deploy](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy)
```

Users can click this button to deploy your app directly to Heroku.

## Required Configuration

### Environment Variables

After creating your app, you must configure these environment variables:

#### Database Configuration (Heroku Postgres)

Heroku Postgres addon automatically sets `DATABASE_URL`. You need to extract and set individual variables:

```bash
# After provisioning Heroku Postgres, get the connection details
heroku pg:credentials:url DATABASE --app my-tmi-server

# Set individual environment variables
heroku config:set TMI_POSTGRES_HOST=<host> --app my-tmi-server
heroku config:set TMI_POSTGRES_PORT=5432 --app my-tmi-server
heroku config:set TMI_POSTGRES_USER=<user> --app my-tmi-server
heroku config:set TMI_POSTGRES_PASSWORD=<password> --app my-tmi-server
heroku config:set TMI_POSTGRES_DATABASE=<database> --app my-tmi-server
heroku config:set TMI_POSTGRES_SSL_MODE=require --app my-tmi-server
```

**OR** use the Heroku Dashboard:
1. Go to your app's "Settings" tab
2. Click "Reveal Config Vars"
3. Add each variable manually

#### Redis Configuration (Heroku Redis)

Similar to Postgres, extract Redis connection details:

```bash
# After provisioning Heroku Redis
heroku redis:credentials --app my-tmi-server

# Set individual environment variables
heroku config:set TMI_REDIS_HOST=<host> --app my-tmi-server
heroku config:set TMI_REDIS_PORT=<port> --app my-tmi-server
heroku config:set TMI_REDIS_PASSWORD=<password> --app my-tmi-server
```

#### JWT Configuration

Generate a strong secret for JWT signing:

```bash
# Generate a random secret
openssl rand -base64 32

# Set the JWT secret
heroku config:set TMI_AUTH_JWT_SECRET=<generated-secret> --app my-tmi-server
heroku config:set TMI_AUTH_JWT_ISSUER=tmi-server --app my-tmi-server
heroku config:set TMI_AUTH_JWT_ACCESS_TOKEN_TTL=60 --app my-tmi-server
heroku config:set TMI_AUTH_JWT_REFRESH_TOKEN_TTL=10080 --app my-tmi-server
```

#### Server Configuration

```bash
# Heroku automatically sets PORT, but you can configure interface
heroku config:set TMI_SERVER_INTERFACE=0.0.0.0 --app my-tmi-server
heroku config:set TMI_LOGGING_LEVEL=info --app my-tmi-server
heroku config:set TMI_LOGGING_IS_DEV=false --app my-tmi-server

# TLS is handled by Heroku's load balancer
heroku config:set TMI_SERVER_TLS_ENABLED=false --app my-tmi-server
```

### Complete Environment Variables List

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TMI_POSTGRES_HOST` | Yes | - | PostgreSQL host from Heroku Postgres |
| `TMI_POSTGRES_PORT` | No | 5432 | PostgreSQL port |
| `TMI_POSTGRES_USER` | Yes | - | PostgreSQL user |
| `TMI_POSTGRES_PASSWORD` | Yes | - | PostgreSQL password |
| `TMI_POSTGRES_DATABASE` | Yes | - | PostgreSQL database name |
| `TMI_POSTGRES_SSL_MODE` | No | require | SSL mode (use 'require' for Heroku) |
| `TMI_REDIS_HOST` | Yes | - | Redis host from Heroku Redis |
| `TMI_REDIS_PORT` | No | 6379 | Redis port |
| `TMI_REDIS_PASSWORD` | No | - | Redis password |
| `TMI_AUTH_JWT_SECRET` | Yes | - | Strong random secret for JWT signing |
| `TMI_AUTH_JWT_ISSUER` | No | tmi-server | JWT issuer claim |
| `TMI_AUTH_JWT_ACCESS_TOKEN_TTL` | No | 60 | Access token TTL (minutes) |
| `TMI_AUTH_JWT_REFRESH_TOKEN_TTL` | No | 10080 | Refresh token TTL (minutes) |
| `TMI_SERVER_PORT` | No | 8080 | Server port (Heroku sets via $PORT) |
| `TMI_SERVER_INTERFACE` | No | 0.0.0.0 | Network interface |
| `TMI_LOGGING_LEVEL` | No | info | Log level (debug/info/warn/error) |
| `TMI_LOGGING_IS_DEV` | No | false | Development mode |
| `TMI_SERVER_TLS_ENABLED` | No | false | TLS (Heroku handles this) |

## Required Addons

The `app.json` file automatically provisions these addons:

### 1. Heroku Postgres (Database)

```bash
# Provision manually if needed
heroku addons:create heroku-postgresql:essential-0 --app my-tmi-server

# Check status
heroku pg:info --app my-tmi-server

# View credentials
heroku pg:credentials:url DATABASE --app my-tmi-server
```

**Plans**:
- `essential-0`: $5/month, 10M rows
- `essential-1`: $50/month, 10M rows
- `standard-0`: $50/month, 64GB storage

### 2. Heroku Redis (Cache & Sessions)

```bash
# Provision manually if needed
heroku addons:create heroku-redis:mini --app my-tmi-server

# Check status
heroku redis:info --app my-tmi-server

# View credentials
heroku redis:credentials --app my-tmi-server
```

**Plans**:
- `mini`: $3/month, 25MB
- `premium-0`: $15/month, 100MB

## Database Migrations

The TMI server uses golang-migrate for database schema management. You need to run migrations after deployment:

### Option 1: Run Migrations via Heroku CLI

```bash
# Use one-off dyno to run migrations
heroku run bin/migrate up --app my-tmi-server

# Check migration status
heroku run bin/migrate version --app my-tmi-server
```

**Note**: This requires the `migrate` binary to be built, which isn't configured by default. See "Building Multiple Binaries" section.

### Option 2: Run Migrations from Local Machine

```bash
# Connect to Heroku Postgres from local machine
heroku pg:credentials:url DATABASE --app my-tmi-server

# Set environment variables locally
export TMI_POSTGRES_HOST=<host>
export TMI_POSTGRES_PORT=5432
export TMI_POSTGRES_USER=<user>
export TMI_POSTGRES_PASSWORD=<password>
export TMI_POSTGRES_DATABASE=<database>
export TMI_POSTGRES_SSL_MODE=require

# Run migrations locally against Heroku database
make build-migrate
./bin/migrate up
```

### Option 3: Automatic Migrations (Recommended for Production)

Modify your Procfile to run migrations on startup:

```
release: bin/migrate up
web: bin/tmiserver --config=config-production.yml
```

**Note**: This requires building both `migrate` and `tmiserver` binaries. See "Building Multiple Binaries" section.

## Building Only tmiserver (Current Configuration)

The `.godir` file contains `tmiserver`, which tells Heroku to build only:

```
github.com/ericfitz/tmi/cmd/server
```

This prevents Heroku from building the `migrate` and `check-db` binaries, which speeds up builds and reduces slug size.

### Verifying the Build

After deployment, check what binaries were built:

```bash
# SSH into the dyno
heroku run bash --app my-tmi-server

# List binaries
ls -la bin/

# Should only see: tmiserver
```

## Building Multiple Binaries

If you need to build multiple binaries (e.g., for migrations), you have two options:

### Option 1: Multi-Binary Procfile (Recommended)

1. **Remove `.godir` file**:
   ```bash
   rm .godir
   ```

2. **Update `app.json`** to build multiple packages:
   ```json
   "env": {
     "GO_INSTALL_PACKAGE_SPEC": {
       "value": "github.com/ericfitz/tmi/cmd/server github.com/ericfitz/tmi/cmd/migrate",
       "required": true
     }
   }
   ```

3. **Update Procfile** to run migrations on release:
   ```
   release: bin/migrate up
   web: bin/tmiserver --config=config-production.yml
   ```

### Option 2: Separate Migration App

Create a separate Heroku app for running migrations:

```bash
# Create migration app
heroku create my-tmi-migrations --app my-tmi-server

# Configure to build only migrate binary
heroku config:set GO_INSTALL_PACKAGE_SPEC=github.com/ericfitz/tmi/cmd/migrate --app my-tmi-migrations

# Deploy
git push heroku main:main --app my-tmi-migrations

# Run migrations
heroku run bin/migrate up --app my-tmi-migrations
```

## Configuration File

The server expects a configuration file. You have several options:

### Option 1: Use Environment Variables (Recommended for Heroku)

The TMI server reads configuration from environment variables when they're set. This is the recommended approach for Heroku.

**No config file needed** - just set environment variables as documented above.

### Option 2: Commit config-production.yml to Repository

1. **Create config-production.yml**:
   ```yaml
   server:
     port: "8080"
     interface: "0.0.0.0"
     tls_enabled: false

   logging:
     level: "info"
     is_dev: false
     log_dir: "/tmp/logs"

   database:
     postgres:
       host: "${TMI_POSTGRES_HOST}"
       port: "${TMI_POSTGRES_PORT}"
       user: "${TMI_POSTGRES_USER}"
       password: "${TMI_POSTGRES_PASSWORD}"
       database: "${TMI_POSTGRES_DATABASE}"
       ssl_mode: "require"

   redis:
     host: "${TMI_REDIS_HOST}"
     port: "${TMI_REDIS_PORT}"
     password: "${TMI_REDIS_PASSWORD}"

   auth:
     jwt:
       secret: "${TMI_AUTH_JWT_SECRET}"
       issuer: "tmi-server"
       access_token_ttl: 60
       refresh_token_ttl: 10080
   ```

2. **Update .gitignore** to allow config-production.yml:
   ```
   # .gitignore
   config-development.yml  # Keep ignoring this
   # Remove config-production.yml from ignore list
   ```

3. **Commit and deploy**:
   ```bash
   git add config-production.yml
   git commit -m "Add production configuration"
   git push heroku main
   ```

### Option 3: Generate Config at Runtime

Modify the Procfile to generate config from environment variables:

```
web: ./bin/tmiserver --generate-config && ./bin/tmiserver --config=config-production.yml
```

## Deployment Workflow

### Initial Deployment

1. **Push code to GitHub**:
   ```bash
   git add .
   git commit -m "Prepare for Heroku deployment"
   git push origin main
   ```

2. **Create Heroku app and deploy**:
   ```bash
   heroku create my-tmi-server
   git push heroku main
   ```

3. **Provision addons**:
   ```bash
   heroku addons:create heroku-postgresql:essential-0
   heroku addons:create heroku-redis:mini
   ```

4. **Configure environment variables** (see "Required Configuration" section)

5. **Run database migrations**:
   ```bash
   heroku run bin/migrate up
   ```

6. **Test the deployment**:
   ```bash
   heroku open
   curl https://my-tmi-server.herokuapp.com/version
   ```

### Subsequent Deployments

With automatic deploys enabled:

```bash
# Make changes locally
git add .
git commit -m "Update feature"
git push origin main

# Heroku automatically deploys from GitHub
# Monitor deployment
heroku logs --tail
```

Manual deployment:

```bash
git push heroku main
```

## Monitoring and Troubleshooting

### View Logs

```bash
# Stream live logs
heroku logs --tail --app my-tmi-server

# View recent logs
heroku logs --app my-tmi-server

# View specific number of lines
heroku logs -n 500 --app my-tmi-server
```

### Check Dyno Status

```bash
# View running dynos
heroku ps --app my-tmi-server

# Restart all dynos
heroku restart --app my-tmi-server

# Restart specific dyno
heroku restart web.1 --app my-tmi-server
```

### Check Addon Status

```bash
# PostgreSQL
heroku pg:info --app my-tmi-server
heroku pg:diagnose --app my-tmi-server

# Redis
heroku redis:info --app my-tmi-server
```

### Common Issues

#### 1. Application Error (H10)

**Symptom**: "Application error" page when accessing the app.

**Cause**: Server failed to bind to the PORT provided by Heroku.

**Solution**:
```bash
# Heroku sets PORT automatically
# Ensure your server listens on $PORT
heroku config:set TMI_SERVER_PORT=$PORT --app my-tmi-server

# Or use 0.0.0.0:$PORT in your code
```

#### 2. Database Connection Failed

**Symptom**: Logs show "failed to connect to database".

**Cause**: Missing or incorrect database credentials.

**Solution**:
```bash
# Get correct credentials
heroku pg:credentials:url DATABASE --app my-tmi-server

# Update environment variables
heroku config:set TMI_POSTGRES_HOST=<host> --app my-tmi-server
# ... set other variables
```

#### 3. Wrong Binary Built

**Symptom**: Logs show "bash: bin/tmiserver: command not found".

**Cause**: Heroku built the wrong binary or no binary at all.

**Solution**:
```bash
# Check .godir file
cat .godir

# Should contain: tmiserver

# Or check GO_INSTALL_PACKAGE_SPEC
heroku config:get GO_INSTALL_PACKAGE_SPEC --app my-tmi-server

# Should be: github.com/ericfitz/tmi/cmd/server
```

#### 4. Build Timeout

**Symptom**: Build fails with "Build exceeded maximum time".

**Cause**: Go module downloads or builds taking too long.

**Solution**:
```bash
# Use Go module cache
heroku config:set GOMODCACHE=/app/.go/pkg/mod --app my-tmi-server

# Reduce build parallelism
heroku config:set GOMAXPROCS=2 --app my-tmi-server
```

## Scaling

### Vertical Scaling (Dyno Size)

```bash
# List available dyno types
heroku ps:types --app my-tmi-server

# Scale up to Standard-1X ($25/month)
heroku ps:resize web=standard-1x --app my-tmi-server

# Scale up to Standard-2X ($50/month)
heroku ps:resize web=standard-2x --app my-tmi-server
```

### Horizontal Scaling (More Dynos)

```bash
# Scale to 2 web dynos
heroku ps:scale web=2 --app my-tmi-server

# Scale to 4 web dynos
heroku ps:scale web=4 --app my-tmi-server
```

**Note**: WebSocket connections are sticky by default, so multiple dynos work well with the TMI server's WebSocket hub.

## Cost Estimation

### Minimal Setup

- **Eco Dyno**: $5/month (sleeps after 30 min of inactivity)
- **Heroku Postgres (essential-0)**: $5/month
- **Heroku Redis (mini)**: $3/month
- **Total**: ~$13/month

### Production Setup

- **Basic Dyno**: $7/month (no sleeping)
- **Heroku Postgres (standard-0)**: $50/month
- **Heroku Redis (premium-0)**: $15/month
- **Total**: ~$72/month

### High-Availability Setup

- **Standard-2X Dynos (x2)**: $100/month
- **Heroku Postgres (standard-2)**: $200/month
- **Heroku Redis (premium-2)**: $60/month
- **Total**: ~$360/month

## Security Considerations

1. **TLS**: Heroku provides TLS termination at the load balancer. Set `TMI_SERVER_TLS_ENABLED=false`.

2. **Environment Variables**: Store all secrets in Heroku config vars, never commit them to git.

3. **Database SSL**: Always use `TMI_POSTGRES_SSL_MODE=require` for Heroku Postgres.

4. **JWT Secret**: Generate a strong random secret:
   ```bash
   openssl rand -base64 32
   ```

5. **Access Control**: Use Heroku's access control to limit who can modify the app:
   ```bash
   heroku access --app my-tmi-server
   heroku access:add user@example.com --app my-tmi-server
   ```

## CI/CD Integration

### Automatic Deploys from GitHub

1. **Enable in Dashboard**:
   - Go to "Deploy" tab
   - Select "GitHub" as deployment method
   - Enable "Wait for CI to pass before deploy"
   - Enable "Automatic deploys"

2. **Configure GitHub Actions** (optional):
   ```yaml
   # .github/workflows/deploy.yml
   name: Deploy to Heroku

   on:
     push:
       branches: [main]

   jobs:
     deploy:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v2
         - uses: akhileshns/heroku-deploy@v3.12.12
           with:
             heroku_api_key: ${{secrets.HEROKU_API_KEY}}
             heroku_app_name: "my-tmi-server"
             heroku_email: "your-email@example.com"
   ```

## Additional Resources

- [Heroku Go Support](https://devcenter.heroku.com/articles/go-support)
- [Heroku Postgres](https://devcenter.heroku.com/articles/heroku-postgresql)
- [Heroku Redis](https://devcenter.heroku.com/articles/heroku-redis)
- [Heroku CLI Reference](https://devcenter.heroku.com/articles/heroku-cli-commands)
- [Procfile Documentation](https://devcenter.heroku.com/articles/procfile)
- [app.json Schema](https://devcenter.heroku.com/articles/app-json-schema)

## Support

For TMI server specific issues, see the main project documentation:
- [Development Setup](../../developer/setup/development-setup.md)
- [Database Operations](../database/database-operations.md)
- [Monitoring](../monitoring/monitoring-guide.md)
