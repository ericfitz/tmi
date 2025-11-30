# Heroku Deployment Guide

This guide explains how to deploy the TMI server to Heroku directly from your GitHub repository.

## Overview

The TMI server is deployed to Heroku using:
- **Procfile**: Specifies the command to run the tmiserver binary
- **.godir**: Tells Heroku to build only the tmiserver package (not other binaries)
- **app.json**: Defines the Heroku app configuration, environment variables, and required addons
- **setup-heroku-env.py**: Automated Python configuration script for environment variables (recommended)
- **configure-heroku-env.sh**: Alternative Bash configuration script

## Prerequisites

1. **Heroku Account**: Sign up at [heroku.com](https://heroku.com)
2. **Heroku CLI**: Install from [devcenter.heroku.com/articles/heroku-cli](https://devcenter.heroku.com/articles/heroku-cli)
3. **UV (for automated setup)**: Install from [astral.sh/uv](https://astral.sh/uv) - `curl -LsSf https://astral.sh/uv/install.sh | sh`
4. **GitHub Repository**: Your code must be pushed to a GitHub repository
5. **Git**: Ensure your local repository is connected to GitHub

## Quick Start Deployment

### Recommended: Automated Configuration Script

The fastest way to get started is with the automated configuration script:

```bash
# 1. Create your Heroku apps
heroku create my-tmi-server    # Backend API
heroku create my-tmi-ux        # Frontend (optional)

# 2. Provision addons
heroku addons:create heroku-postgresql:essential-0 --app my-tmi-server
heroku addons:create heroku-redis:mini --app my-tmi-server

# 3. Run automated configuration
make heroku-setup

# 4. Deploy
git push heroku main
```

The script will:
- Automatically extract database and Redis credentials
- Generate secure JWT secrets
- Configure WebSocket CORS origins based on your client app
- Prompt for OAuth provider credentials
- Apply all configuration with confirmation

**See the [Configuration Methods](#configuration-methods) section below for detailed information.**

---

### Alternative: Manual Heroku Dashboard Deployment

### Option 1: Deploy via Heroku Dashboard (For Manual Setup)

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

## Configuration Methods

You have two options for configuring environment variables:

1. **Automated Configuration Script (Recommended)** - Use the interactive `setup-heroku-env.py` script
2. **Manual Configuration** - Set variables individually via Heroku CLI or Dashboard

### Option 1: Automated Configuration (Recommended)

The TMI project includes an automated configuration script that handles most of the setup for you:

```bash
# Interactive mode - prompts for all configuration
make heroku-setup

# Or run directly with uv
uv run scripts/setup-heroku-env.py

# Preview configuration without applying (dry-run)
make heroku-setup-dry-run
```

**What it does automatically:**
- Lists and selects Heroku apps (server + client)
- Detects and extracts PostgreSQL credentials from Heroku Postgres addon
- Detects and extracts Redis credentials from Heroku Redis addon
- Generates secure JWT_SECRET
- Auto-configures OAUTH_CALLBACK_URL from server app URL
- Auto-configures WEBSOCKET_ALLOWED_ORIGINS from client app URL
- Prompts for OAuth provider credentials (Google, GitHub, Microsoft)
- Sets sensible server defaults (logging, TLS, etc.)
- Offers to provision missing addons
- Displays configuration summary before applying

**Script features:**
- `--dry-run` - Preview changes without applying
- `--non-interactive` - Batch mode for automation
- `--skip-oauth` - Skip OAuth configuration
- `--server-app <name>` - Specify server app name
- `--client-app <name>` - Specify client app name
- `--help` - Show all available options

**Prerequisites:**
- Heroku CLI installed and authenticated
- UV installed (`curl -LsSf https://astral.sh/uv/install.sh | sh`)
- Apps created on Heroku (or script can create them)

**Example workflow:**
```bash
# 1. Run the setup script
make heroku-setup

# 2. Select your server app (e.g., tmi-server)
# 3. Select your client app (e.g., tmi-ux)
# 4. Script auto-configures database, Redis, JWT, WebSocket origins
# 5. Enter OAuth credentials for desired providers
# 6. Review and confirm configuration
# 7. Deploy: git push heroku main
```

### Option 2: Manual Configuration

After creating your app, you must configure these environment variables:

#### Database Configuration (Heroku Postgres)

Heroku Postgres addon automatically sets `DATABASE_URL`. You need to extract and set individual variables:

```bash
# After provisioning Heroku Postgres, get the connection details
heroku pg:credentials:url DATABASE --app my-tmi-server

# Set individual environment variables
heroku config:set POSTGRES_HOST=<host> --app my-tmi-server
heroku config:set POSTGRES_PORT=5432 --app my-tmi-server
heroku config:set POSTGRES_USER=<user> --app my-tmi-server
heroku config:set POSTGRES_PASSWORD=<password> --app my-tmi-server
heroku config:set POSTGRES_DATABASE=<database> --app my-tmi-server
heroku config:set POSTGRES_SSL_MODE=require --app my-tmi-server
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
heroku config:set REDIS_HOST=<host> --app my-tmi-server
heroku config:set REDIS_PORT=<port> --app my-tmi-server
heroku config:set REDIS_PASSWORD=<password> --app my-tmi-server
```

#### JWT Configuration

Generate a strong secret for JWT signing:

```bash
# Generate a random secret
openssl rand -base64 32

# Set the JWT secret
heroku config:set JWT_SECRET=<generated-secret> --app my-tmi-server
heroku config:set JWT_EXPIRATION_SECONDS=3600 --app my-tmi-server
heroku config:set JWT_SIGNING_METHOD=HS256 --app my-tmi-server
```

#### Server Configuration

```bash
# Heroku automatically sets PORT, but you can configure interface
heroku config:set SERVER_INTERFACE=0.0.0.0 --app my-tmi-server
heroku config:set LOGGING_LEVEL=info --app my-tmi-server
heroku config:set LOGGING_IS_DEV=false --app my-tmi-server

# TLS is handled by Heroku's load balancer
heroku config:set SERVER_TLS_ENABLED=false --app my-tmi-server
```

### Complete Environment Variables List

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_HOST` | Yes | - | PostgreSQL host from Heroku Postgres |
| `POSTGRES_PORT` | No | 5432 | PostgreSQL port |
| `POSTGRES_USER` | Yes | - | PostgreSQL user |
| `POSTGRES_PASSWORD` | Yes | - | PostgreSQL password |
| `POSTGRES_DATABASE` | Yes | - | PostgreSQL database name |
| `POSTGRES_SSL_MODE` | No | require | SSL mode (use 'require' for Heroku) |
| `REDIS_HOST` | Yes | - | Redis host from Heroku Redis |
| `REDIS_PORT` | No | 6379 | Redis port |
| `REDIS_PASSWORD` | No | - | Redis password |
| `REDIS_DB` | No | 0 | Redis database number |
| `JWT_SECRET` | Yes | - | Strong random secret for JWT signing |
| `JWT_EXPIRATION_SECONDS` | No | 3600 | JWT expiration in seconds |
| `JWT_SIGNING_METHOD` | No | HS256 | JWT signing method |
| `OAUTH_CALLBACK_URL` | No | - | OAuth callback URL |
| `SERVER_PORT` | No | 8080 | Server port (Heroku sets via $PORT) |
| `SERVER_INTERFACE` | No | 0.0.0.0 | Network interface |
| `SERVER_READ_TIMEOUT` | No | 5s | HTTP read timeout |
| `SERVER_WRITE_TIMEOUT` | No | 10s | HTTP write timeout |
| `SERVER_IDLE_TIMEOUT` | No | 60s | HTTP idle timeout |
| `SERVER_TLS_ENABLED` | No | false | TLS (Heroku handles this) |
| `SERVER_TLS_CERT_FILE` | No | - | TLS certificate file path |
| `SERVER_TLS_KEY_FILE` | No | - | TLS key file path |
| `SERVER_TLS_SUBJECT_NAME` | No | hostname | TLS subject name |
| `SERVER_HTTP_TO_HTTPS_REDIRECT` | No | true | Redirect HTTP to HTTPS |
| `LOGGING_LEVEL` | No | info | Log level (debug/info/warn/error) |
| `LOGGING_IS_DEV` | No | false | Development mode |
| `LOGGING_IS_TEST` | No | false | Test mode |
| `LOGGING_LOG_DIR` | No | logs | Log directory path |
| `LOGGING_MAX_AGE_DAYS` | No | 7 | Maximum log age in days |
| `LOGGING_MAX_SIZE_MB` | No | 100 | Maximum log file size in MB |
| `LOGGING_MAX_BACKUPS` | No | 10 | Maximum number of log backups |
| `LOGGING_ALSO_LOG_TO_CONSOLE` | No | true | Also log to console |
| `LOGGING_LOG_API_REQUESTS` | No | false | Log API requests |
| `LOGGING_LOG_API_RESPONSES` | No | false | Log API responses |
| `LOGGING_LOG_WEBSOCKET_MESSAGES` | No | false | Log WebSocket messages |
| `LOGGING_REDACT_AUTH_TOKENS` | No | true | Redact auth tokens in logs |
| `LOGGING_SUPPRESS_UNAUTH_LOGS` | No | true | Suppress unauthenticated logs |
| `WEBSOCKET_ALLOWED_ORIGINS` | No | - | Comma-separated list of allowed WebSocket origins |
| `WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS` | No | 300 | WebSocket inactivity timeout in seconds |

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

**Important:** The TMI server automatically runs database migrations on startup. There is no separate migration binary or step required.

### Automatic Migration Behavior

When the tmiserver binary starts, it:

1. Connects to the PostgreSQL database
2. Automatically runs all pending migrations from `auth/migrations/`
3. Creates or updates the schema as needed
4. Starts the HTTP server after migrations complete

**This means:**

- ✅ No separate `migrate` binary is needed
- ✅ No manual migration commands required
- ✅ Schema is always up-to-date when the server starts
- ✅ Migrations run automatically on every deployment

### Migration Monitoring

To verify migrations ran successfully:

```bash
# Check server startup logs
heroku logs --tail --app my-tmi-server | grep -i migration

# You should see log entries like:
# "Running database migrations from auth/migrations"
# "Successfully applied X migrations"
```

### Manual Database Inspection

If you need to inspect the database schema:

```bash
# Connect to Heroku Postgres
heroku pg:psql --app my-tmi-server

# List all tables
\dt

# Exit
\q
```

## Heroku Build Configuration

The Procfile specifies which binary to run:

```
web: SERVER_PORT=$PORT bin/server
```

Heroku's Go buildpack automatically builds the `cmd/server` package, creating `bin/server` (which is the tmiserver binary).

### Verifying the Build

After deployment, check what binaries were built:

```bash
# SSH into the dyno
heroku run bash --app my-tmi-server

# List binaries
ls -la bin/

# Should see: server (the tmiserver binary)
```

**Note:** The `migrate` and `check-db` binaries are **not needed** for Heroku deployments since migrations run automatically via the tmiserver binary.

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
       host: "${POSTGRES_HOST}"
       port: "${POSTGRES_PORT}"
       user: "${POSTGRES_USER}"
       password: "${POSTGRES_PASSWORD}"
       database: "${POSTGRES_DATABASE}"
       ssl_mode: "require"

   redis:
     host: "${REDIS_HOST}"
     port: "${REDIS_PORT}"
     password: "${REDIS_PASSWORD}"

   auth:
     jwt:
       secret: "${JWT_SECRET}"
       expiration_seconds: 3600
       signing_method: "HS256"
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

#### Quick Start (Automated)

1. **Create Heroku apps** (if not already created):
   ```bash
   heroku create my-tmi-server    # API server
   heroku create my-tmi-ux        # Frontend client (optional)
   ```

2. **Run automated configuration**:
   ```bash
   make heroku-setup
   # Follow the interactive prompts to configure your apps
   ```

3. **Push code and deploy**:
   ```bash
   git push heroku main
   # Database migrations run automatically via release phase
   ```

4. **Test the deployment**:
   ```bash
   curl https://my-tmi-server.herokuapp.com/version
   ```

#### Manual Steps (Alternative)

If you prefer manual configuration:

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

4. **Configure environment variables** (see "Option 2: Manual Configuration" section)

5. **Deploy**:
   ```bash
   git push heroku main
   # Database migrations run automatically via release phase
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
heroku config:set SERVER_PORT=$PORT --app my-tmi-server

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
heroku config:set POSTGRES_HOST=<host> --app my-tmi-server
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

## WebSocket Configuration

The TMI server uses WebSockets for real-time collaboration features. Heroku requires specific configuration for WebSocket support:

### Required Configuration Changes

#### 1. Port Binding (Critical)

The Procfile **must** bind to Heroku's dynamically assigned `$PORT` environment variable:

```
web: SERVER_PORT=$PORT bin/server
```

**Why this matters**: Heroku's HTTP router requires applications to bind to the port specified by the `$PORT` environment variable. WebSocket connections upgrade from HTTP connections, so proper port binding is essential for WebSocket handshakes to succeed.

#### 2. Environment Variable Prefix Support

The server was updated to support both prefixed (`TMI_*`) and non-prefixed environment variables. Use non-prefixed variables in the Procfile for simplicity:

```bash
# Procfile uses non-prefixed variable
SERVER_PORT=$PORT

# app.json and Heroku config can use either:
heroku config:set SERVER_INTERFACE=0.0.0.0 --app my-tmi-server
# or
heroku config:set TMI_SERVER_INTERFACE=0.0.0.0 --app my-tmi-server
```

#### 3. WebSocket Allowed Origins (Required for CORS)

Configure allowed origins for WebSocket connections to permit cross-origin requests from your frontend application:

```bash
# Set allowed origins for WebSocket connections (comma-separated list)
heroku config:set WEBSOCKET_ALLOWED_ORIGINS="https://your-frontend-app.com,https://www.your-frontend-app.com" --app my-tmi-server
```

**Why this matters**: WebSocket connections from browser-based clients require CORS (Cross-Origin Resource Sharing) configuration. The `WEBSOCKET_ALLOWED_ORIGINS` environment variable specifies which origins are permitted to establish WebSocket connections to your server.

**Format**: Comma-separated list of fully qualified origin URLs
- Include protocol (https:// or http://)
- Include all frontend domains that will connect to your API
- No trailing slashes

**Examples**:
```bash
# Single frontend domain
heroku config:set WEBSOCKET_ALLOWED_ORIGINS="https://myapp.example.com" --app my-tmi-server

# Multiple domains (production + staging)
heroku config:set WEBSOCKET_ALLOWED_ORIGINS="https://myapp.com,https://staging.myapp.com" --app my-tmi-server

# Multiple domains with www variants
heroku config:set WEBSOCKET_ALLOWED_ORIGINS="https://myapp.com,https://www.myapp.com,https://staging.myapp.com" --app my-tmi-server
```

**Note**: The server automatically allows connections from:
- `http://localhost:*` (development)
- `http://{server-hostname}` and `https://{server-hostname}` (same-origin)

If you need to allow additional origins (like your frontend application hosted on a different domain), you **must** set `WEBSOCKET_ALLOWED_ORIGINS`.

#### 4. Session Affinity (Optional)

For production deployments with multiple dynos, consider enabling Heroku's session affinity feature:

```bash
# Check session affinity status
heroku features:info http-session-affinity --app my-tmi-server

# Enable session affinity (optional, but recommended for multi-dyno WebSocket deployments)
heroku features:enable http-session-affinity --app my-tmi-server
```

**Benefits**: Session affinity ensures that WebSocket connections from the same client consistently route to the same dyno, which can improve connection stability and reduce reconnection overhead.

**Documentation**: [Heroku Session Affinity](https://devcenter.heroku.com/articles/session-affinity)

### WebSocket Testing

After deployment, test WebSocket connectivity:

```bash
# Test WebSocket endpoint (requires authentication token)
wscat -c "wss://my-tmi-server.herokuapp.com/ws/diagrams/{diagram-id}" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

### WebSocket Troubleshooting

#### Connection Upgrades Failing

**Symptom**: WebSocket connections fail with 503 or timeout errors.

**Cause**: Server not binding to `$PORT` correctly.

**Solution**:
```bash
# Verify Procfile uses $PORT
cat Procfile
# Should show: web: SERVER_PORT=$PORT bin/server

# Check server logs for port binding
heroku logs --tail --app my-tmi-server | grep -i "port\|websocket"
```

#### WebSocket Connection Rejected (CORS)

**Symptom**: WebSocket connections fail with "origin not allowed" or 403 Forbidden errors. Browser console shows CORS-related error messages.

**Cause**: Frontend origin not included in `WEBSOCKET_ALLOWED_ORIGINS`.

**Solution**:
```bash
# Check current allowed origins
heroku config:get WEBSOCKET_ALLOWED_ORIGINS --app my-tmi-server

# Add your frontend origin(s)
heroku config:set WEBSOCKET_ALLOWED_ORIGINS="https://your-frontend.com" --app my-tmi-server

# Check server logs for rejected origins
heroku logs --tail --app my-tmi-server | grep -i "rejected.*origin"
```

**Example log message when origin is rejected**:
```
Rejected WebSocket connection from origin: https://unauthorized-site.com
```

#### WebSocket Disconnects with Multiple Dynos

**Symptom**: WebSocket connections disconnect when scaling to multiple dynos.

**Cause**: Load balancer routing WebSocket traffic to different dynos.

**Solution**:
```bash
# Enable session affinity
heroku features:enable http-session-affinity --app my-tmi-server

# Or scale down to single dyno for testing
heroku ps:scale web=1 --app my-tmi-server
```

#### Idle Connection Timeouts

**Symptom**: WebSocket connections close after 55 seconds of inactivity.

**Cause**: Heroku's HTTP router terminates idle connections after 55 seconds.

**Solution**: The TMI server implements automatic ping/pong heartbeat messages. Ensure `WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS` is set appropriately:

```bash
# Set inactivity timeout (default: 300 seconds)
heroku config:set WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS=300 --app my-tmi-server
```

The server sends ping frames every 30 seconds by default, which keeps connections alive within Heroku's 55-second timeout window.

### WebSocket Monitoring

Monitor WebSocket connections and activity:

```bash
# Enable WebSocket message logging (debugging only)
heroku config:set LOGGING_LOG_WEBSOCKET_MESSAGES=true --app my-tmi-server

# View WebSocket-specific logs
heroku logs --tail --app my-tmi-server | grep -i websocket

# Disable after debugging (reduces log volume)
heroku config:set LOGGING_LOG_WEBSOCKET_MESSAGES=false --app my-tmi-server
```

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

1. **TLS**: Heroku provides TLS termination at the load balancer. Set `SERVER_TLS_ENABLED=false`.

2. **Environment Variables**: Store all secrets in Heroku config vars, never commit them to git.

3. **Database SSL**: Always use `POSTGRES_SSL_MODE=require` for Heroku Postgres.

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

### TMI-Specific Tools

- **Automated Configuration Script**: `scripts/setup-heroku-env.py` - Interactive tool for environment setup
  - Usage: `make heroku-setup` or `uv run scripts/setup-heroku-env.py`
  - Features: Auto-extraction of credentials, JWT generation, WebSocket CORS configuration
  - Dry-run mode: `make heroku-setup-dry-run`

### Heroku Documentation

- [Heroku Go Support](https://devcenter.heroku.com/articles/go-support)
- [Heroku Postgres](https://devcenter.heroku.com/articles/heroku-postgresql)
- [Heroku Redis](https://devcenter.heroku.com/articles/heroku-redis)
- [Heroku CLI Reference](https://devcenter.heroku.com/articles/heroku-cli-commands)
- [Procfile Documentation](https://devcenter.heroku.com/articles/procfile)
- [app.json Schema](https://devcenter.heroku.com/articles/app-json-schema)
- [Session Affinity for WebSockets](https://devcenter.heroku.com/articles/session-affinity)

## Support

For TMI server specific issues, see the main project documentation:
- [Development Setup](../../developer/setup/development-setup.md)
- [Database Operations](../database/database-operations.md)
- [Monitoring](../monitoring/monitoring-guide.md)
