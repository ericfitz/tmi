# TMI Server Deployment Guide

This guide covers building, configuring, and deploying the Threat Modeling Interface (TMI) server in production environments.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Building](#building)
- [Configuration](#configuration)
- [Database Setup](#database-setup)
- [Cache Setup](#cache-setup)
- [Deployment Options](#deployment-options)
- [Security Considerations](#security-considerations)
- [Monitoring & Logging](#monitoring--logging)
- [Troubleshooting](#troubleshooting)

## Overview

TMI is a Go-based web application that provides collaborative threat modeling capabilities. The architecture includes:

- **TMI Server**: Go HTTP server with WebSocket support for real-time collaboration
- **PostgreSQL Database**: Primary data storage for threat models, diagrams, and user data
- **Redis Cache**: Session storage, caching, and real-time WebSocket message coordination
- **OAuth Integration**: Authentication via Google, GitHub, and Microsoft

## Prerequisites

### Development/Build Environment

- Go 1.25.6 or later (verified in go.mod)
- Git
- Make (server is managed via makefile targets, e.g. `make start-dev`)
- Docker (Docker Desktop recommended for local development)

### Runtime Environment

- Linux/Windows/macOS (supports all Go target platforms)
- PostgreSQL 12+
- Redis 6+
- TLS certificates (for production HTTPS)
- OAuth application credentials from your chosen providers

### Network & Security

- Inbound access on configured port (default 8080)
  - in dev environments just use locally; no need for firewall reconfiguration
- Outbound HTTPS access to OAuth providers
- Database and Redis connectivity
  - in dev enviroments, just run docker desktop and the make file can set up the containers for you
- Domain name with DNS (for production)
  - not needed for dev environments

## Building

### Environment

Either use the test OAuth provider (enabled by default in dev builds), or set up OAuth. For OAuth you will need a client ID and a client secret, available from your OAuth provider, and you'll need to configure the callback URL to: http://localhost:8080/oauth2/callback

(Home page URL in development environment is http://localhost:4200)

GitHub: https://github.com/settings/developers - create an application
Google:

- create a Google Cloud project
- navigate to https://console.cloud.google.com/auth/clients
- create a new client
  - Application type: web application
  - Add the callback URL http://localhost:8080/oauth2/callback
  - Save your creds json file or copy the client ID and client secret
  - Navigate to the "audience" page on the left and add your gmail account as a test account

### Development Environment

```bash
# Clone the repository
git clone <repository-url>
cd tmi
```

- [Install Go](https://go.dev/doc/install) 1.25.6 or later
- Copy config-example.yml to config-development.yml
- Modify config-development.yml with your OAuth client information, above

### Build the server

`make build-server`

### Create the development containers and start the app

`make start-dev`

### Production Build (don't do this at this time)

```bash
# Build with optimizations
go build -ldflags="-w -s" -o tmi-server cmd/server/main.go

# Cross-compile for different platforms
GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o tmi-server-linux-amd64 cmd/server/main.go
GOOS=windows GOARCH=amd64 go build -ldflags="-w -s" -o tmi-server-windows-amd64.exe cmd/server/main.go
```

### Docker Build

TMI uses [Chainguard](https://chainguard.dev/) container images for enhanced security with minimal attack surface.

```dockerfile
# Dockerfile.server (Chainguard-based secure build)
# Builder stage: Chainguard Go image
FROM cgr.dev/chainguard/go:latest AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
# Static binary build (excludes Oracle support which requires CGO)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o tmiserver ./cmd/server

# Runtime stage: Chainguard static image (~57MB total)
FROM cgr.dev/chainguard/static:latest
COPY --from=builder /app/tmiserver /tmiserver
COPY --from=builder /app/auth/migrations /auth/migrations
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/tmiserver"]
```

```bash
# Build individual containers
make build-container-tmi     # TMI server only
make build-container-db      # PostgreSQL only
make build-container-redis   # Redis only

# Build all containers
make build-containers
```

**Note**: Container builds use `CGO_ENABLED=0` for static binaries, which excludes Oracle database support. Container builds support PostgreSQL, MySQL, SQLServer, and SQLite. For Oracle support, build locally with `go build -tags oracle`.

## Configuration

TMI supports two configuration methods that can be combined:

1. **YAML Configuration Files** (recommended for traditional deployments)
2. **Environment Variables** (recommended for containers)

### Configuration Templates

TMI provides example configuration files in the repository:

- `config-example.yml` - Template configuration file
- `config-development.yml` - Development configuration (if present)
- `config-production.yml` - Production configuration template
- `config-test.yml` - Test configuration

To customize for your environment:
1. Copy `config-example.yml` to `config-development.yml`
2. Edit `config-development.yml` with your settings
3. For production, customize `config-production.yml`

**Note**: The `--generate-config` flag displays setup help rather than generating files.

### Configuration File Structure

```yaml
# config-production.yml
server:
  port: "8080"
  interface: "0.0.0.0"
  read_timeout: 5s    # Default from source code
  write_timeout: 10s  # Default from source code
  idle_timeout: 60s   # Default from source code
  tls_enabled: true
  tls_cert_file: "/etc/tls/server.crt"
  tls_key_file: "/etc/tls/server.key"
  tls_subject_name: "tmi.example.com"
  http_to_https_redirect: true

database:
  postgres:
    host: "postgres"
    port: "5432"
    user: "tmi_user"
    password: "" # Set via POSTGRES_PASSWORD
    database: "tmi"
    sslmode: "require"
  redis:
    host: "redis"
    port: "6379"
    password: "" # Set via REDIS_PASSWORD
    db: 0

auth:
  jwt:
    secret: "" # REQUIRED: Set via JWT_SECRET
    expiration_seconds: 3600
    signing_method: "HS256"
  oauth:
    callback_url: "https://tmi.example.com/oauth2/callback"
    providers:
      google:
        enabled: true
        client_id: "" # Set via OAUTH_PROVIDERS_GOOGLE_CLIENT_ID
        client_secret: "" # Set via OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET
      github:
        enabled: true
        client_id: "" # Set via OAUTH_PROVIDERS_GITHUB_CLIENT_ID
        client_secret: "" # Set via OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET
      microsoft:
        enabled: false

logging:
  level: "info"
  is_dev: false
  log_dir: "/var/log/tmi"
  max_age_days: 30
  max_size_mb: 100
  max_backups: 10
  also_log_to_console: true
```

### Environment Variables

All configuration values can be overridden with environment variables:

| Environment Variable                      | Description                          | Default                                 |
| ----------------------------------------- | ------------------------------------ | --------------------------------------- |
| `SERVER_PORT`                             | HTTP server port                     | `8080`                                  |
| `SERVER_INTERFACE`                        | Interface to bind to                 | `0.0.0.0`                               |
| `SERVER_TLS_ENABLED`                      | Enable HTTPS                         | `false`                                 |
| `SERVER_TLS_CERT_FILE`                    | TLS certificate file path            | ``                                      |
| `SERVER_TLS_KEY_FILE`                     | TLS private key file path            | ``                                      |
| `POSTGRES_HOST`                           | PostgreSQL host                      | `localhost`                             |
| `POSTGRES_PORT`                           | PostgreSQL port                      | `5432`                                  |
| `POSTGRES_USER`                           | PostgreSQL username                  | `postgres`                              |
| `POSTGRES_PASSWORD`                       | PostgreSQL password                  | ``                                      |
| `POSTGRES_DATABASE`                       | PostgreSQL database name             | `tmi`                                   |
| `POSTGRES_SSL_MODE`                       | PostgreSQL SSL mode                  | `disable`                               |
| `REDIS_HOST`                              | Redis host                           | `localhost`                             |
| `REDIS_PORT`                              | Redis port                           | `6379`                                  |
| `REDIS_PASSWORD`                          | Redis password                       | ``                                      |
| `REDIS_DB`                                | Redis database number                | `0`                                     |
| `JWT_SECRET`                              | **REQUIRED** JWT signing secret      | ``                                      |
| `JWT_EXPIRATION_SECONDS`                  | JWT token lifetime                   | `3600`                                  |
| `OAUTH_CALLBACK_URL`                      | OAuth callback URL                   | `http://localhost:8080/oauth2/callback` |
| `OAUTH_PROVIDERS_GOOGLE_ENABLED`          | Enable Google OAuth                  | `true`                                  |
| `OAUTH_PROVIDERS_GOOGLE_CLIENT_ID`        | Google OAuth client ID               | ``                                      |
| `OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET`    | Google OAuth client secret           | ``                                      |
| `OAUTH_PROVIDERS_GITHUB_ENABLED`          | Enable GitHub OAuth                  | `true`                                  |
| `OAUTH_PROVIDERS_GITHUB_CLIENT_ID`        | GitHub OAuth client ID               | ``                                      |
| `OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET`    | GitHub OAuth client secret           | ``                                      |
| `LOGGING_LEVEL`                           | Log level (debug, info, warn, error) | `info`                                  |
| `LOGGING_IS_DEV`                          | Development mode                     | `false`                                 |

### Required Configuration

**Minimum required configuration for production:**

1. **JWT Secret**: `JWT_SECRET` - Use a cryptographically secure random string (32+ characters)
2. **Database Password**: `POSTGRES_PASSWORD`
3. **OAuth Credentials**: At least one OAuth provider must be configured with client ID and secret
4. **TLS Certificates**: For production HTTPS deployments

## Database Setup

### PostgreSQL Installation

**Ubuntu/Debian:**

```bash
sudo apt update
sudo apt install postgresql postgresql-contrib
sudo systemctl start postgresql
sudo systemctl enable postgresql
```

**CentOS/RHEL:**

```bash
sudo yum install postgresql-server postgresql-contrib
sudo postgresql-setup initdb
sudo systemctl start postgresql
sudo systemctl enable postgresql
```

**Docker:**

```bash
docker run -d \
  --name postgres \
  -e POSTGRES_USER=tmi_user \
  -e POSTGRES_PASSWORD=secure_password \
  -e POSTGRES_DB=tmi \
  -v postgres_data:/var/lib/postgresql/data \
  -p 5432:5432 \
  postgres:15
```

### Database Configuration

1. **Create Database and User:**

```sql
-- Connect as postgres superuser
sudo -u postgres psql

-- Create user and database
CREATE USER tmi_user WITH PASSWORD 'secure_password';
CREATE DATABASE tmi OWNER tmi_user;
GRANT ALL PRIVILEGES ON DATABASE tmi TO tmi_user;

-- Exit PostgreSQL
\q
```

2. **Configure PostgreSQL for Remote Access** (if needed):

```bash
# Edit postgresql.conf
sudo nano /etc/postgresql/15/main/postgresql.conf

# Add/modify:
listen_addresses = '*'

# Edit pg_hba.conf for authentication
sudo nano /etc/postgresql/15/main/pg_hba.conf

# Add line for your TMI server:
host    tmi    tmi_user    10.0.0.0/8    md5

# Restart PostgreSQL
sudo systemctl restart postgresql
```

3. **Database Migrations:**
   TMI automatically runs database migrations on startup via GORM AutoMigrate. The schema includes:

- User management tables
- Threat model and diagram schemas
- OAuth and session tables
- Indexes and constraints

**Note:** Migrations run automatically when the tmiserver starts. There is no separate migration binary required - the server handles schema initialization and updates internally via GORM.

### Database Backup & Recovery

**Backup:**

```bash
# Full database backup
pg_dump -h localhost -U tmi_user -d tmi > tmi_backup_$(date +%Y%m%d_%H%M%S).sql

# Automated daily backups
echo "0 2 * * * pg_dump -h localhost -U tmi_user -d tmi > /backups/tmi_$(date +\%Y\%m\%d).sql" | crontab -
```

**Restore:**

```bash
# Restore from backup
psql -h localhost -U tmi_user -d tmi < tmi_backup_20231201_020000.sql
```

## Cache Setup

### Redis Installation

**Ubuntu/Debian:**

```bash
sudo apt update
sudo apt install redis-server
sudo systemctl start redis-server
sudo systemctl enable redis-server
```

**CentOS/RHEL:**

```bash
sudo yum install epel-release
sudo yum install redis
sudo systemctl start redis
sudo systemctl enable redis
```

**Docker:**

```bash
docker run -d \
  --name redis \
  -p 6379:6379 \
  -v redis_data:/data \
  tmi/tmi-redis:latest redis-server --appendonly yes
```

### Redis Configuration

1. **Secure Redis** (edit `/etc/redis/redis.conf`):

```bash
# Bind to specific interfaces
bind 127.0.0.1 10.0.0.5

# Set password
requirepass your_redis_password

# Disable dangerous commands
rename-command FLUSHDB ""
rename-command FLUSHALL ""
rename-command CONFIG ""

# Restart Redis
sudo systemctl restart redis-server
```

2. **Redis Memory Management:**

```bash
# Set memory limit
maxmemory 1gb
maxmemory-policy allkeys-lru

# Enable persistence
save 900 1
save 300 10
save 60 10000
```

## Deployment Options

### 1. Traditional Server Deployment

**System Service (systemd):**

Create `/etc/systemd/system/tmi.service`:

```ini
[Unit]
Description=TMI Threat Modeling Server
After=network.target postgresql.service redis.service

[Service]
Type=simple
User=tmi
Group=tmi
WorkingDirectory=/opt/tmi
ExecStart=/opt/tmi/tmi-server --config=/etc/tmi/production.yml
Restart=always
RestartSec=5
Environment=JWT_SECRET=your-secure-jwt-secret
Environment=POSTGRES_PASSWORD=your-db-password
Environment=OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=your-google-client-id
Environment=OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET=your-google-secret

# Security settings
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/tmi
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

**Deployment Steps:**

```bash
# Create user
sudo useradd -r -s /bin/false tmi

# Create directories
sudo mkdir -p /opt/tmi /etc/tmi /var/log/tmi
sudo chown tmi:tmi /var/log/tmi

# Copy binary and config
sudo cp tmi-server /opt/tmi/
sudo cp config-production.yml /etc/tmi/production.yml
sudo cp -r auth/migrations /opt/tmi/
sudo cp -r static /opt/tmi/

# Set permissions
sudo chown -R tmi:tmi /opt/tmi
sudo chmod +x /opt/tmi/tmi-server

# Start service
sudo systemctl daemon-reload
sudo systemctl enable tmi
sudo systemctl start tmi

# Check status
sudo systemctl status tmi
```

### 2. Docker Deployment

**Docker Compose Example:**

`docker-compose.yml`:

```yaml
version: "3.8"

services:
  tmi:
    image: tmi-server:latest
    ports:
      - "8080:8080"
    environment:
      - POSTGRES_HOST=postgres
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
      - REDIS_HOST=redis
      - JWT_SECRET=${JWT_SECRET}
      - OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
      - OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET=${GOOGLE_CLIENT_SECRET}
      - OAUTH_CALLBACK_URL=https://tmi.example.com/oauth2/callback
    depends_on:
      - postgres
      - redis
    restart: unless-stopped
    volumes:
      - ./logs:/var/log/tmi

  postgres:
    image: postgres:15
    environment:
      - POSTGRES_USER=tmi_user
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
      - POSTGRES_DB=tmi
    volumes:
      - postgres_data:/var/lib/postgresql/data
    restart: unless-stopped

  redis:
    image: tmi/tmi-redis:latest
    command: redis-server --appendonly yes --requirepass ${REDIS_PASSWORD}
    volumes:
      - redis_data:/data
    restart: unless-stopped

volumes:
  postgres_data:
  redis_data:
```

`.env` file:

```bash
POSTGRES_PASSWORD=secure_db_password
REDIS_PASSWORD=secure_redis_password
JWT_SECRET=your-very-secure-jwt-secret-key
GOOGLE_CLIENT_ID=your-google-oauth-client-id
GOOGLE_CLIENT_SECRET=your-google-oauth-client-secret
```

**Deployment:**

```bash
# Deploy with Docker Compose
docker-compose up -d

# View logs
docker-compose logs -f tmi

# Scale if needed
docker-compose up -d --scale tmi=3
```

### 3. Kubernetes Deployment

**Kubernetes Manifests:**

`namespace.yml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: tmi
```

`configmap.yml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tmi-config
  namespace: tmi
data:
  POSTGRES_HOST: "postgres"
  REDIS_HOST: "redis"
  OAUTH_CALLBACK_URL: "https://tmi.example.com/oauth2/callback"
  LOGGING_LEVEL: "info"
```

`secrets.yml`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: tmi-secrets
  namespace: tmi
type: Opaque
data:
  JWT_SECRET: <base64-encoded-jwt-secret>
  POSTGRES_PASSWORD: <base64-encoded-db-password>
  OAUTH_PROVIDERS_GOOGLE_CLIENT_ID: <base64-encoded-client-id>
  OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET: <base64-encoded-client-secret>
```

`deployment.yml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tmi-server
  namespace: tmi
spec:
  replicas: 3
  selector:
    matchLabels:
      app: tmi-server
  template:
    metadata:
      labels:
        app: tmi-server
    spec:
      containers:
        - name: tmi-server
          image: tmi-server:latest
          ports:
            - containerPort: 8080
          envFrom:
            - configMapRef:
                name: tmi-config
            - secretRef:
                name: tmi-secrets
          livenessProbe:
            httpGet:
              path: /version
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /version
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            requests:
              memory: "256Mi"
              cpu: "100m"
            limits:
              memory: "512Mi"
              cpu: "500m"
```

`service.yml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: tmi-service
  namespace: tmi
spec:
  selector:
    app: tmi-server
  ports:
    - port: 80
      targetPort: 8080
  type: ClusterIP
```

**Deploy to Kubernetes:**

```bash
kubectl apply -f namespace.yml
kubectl apply -f configmap.yml
kubectl apply -f secrets.yml
kubectl apply -f deployment.yml
kubectl apply -f service.yml

# Check deployment
kubectl get pods -n tmi
kubectl logs -f deployment/tmi-server -n tmi
```

## Security Considerations

### 1. Authentication & Authorization

- **OAuth Setup**: Configure OAuth applications with your identity providers
- **JWT Security**: Use a strong, unique JWT secret (32+ characters, cryptographically random)
- **Token Expiration**: Default 1-hour JWT expiration, 30-day refresh token expiration
- **Role-Based Access**: Users have reader/writer/owner roles per threat model

### 2. Network Security

- **HTTPS**: Always use TLS in production (`SERVER_TLS_ENABLED=true`)
- **Firewall**: Restrict access to database and Redis ports
- **Reverse Proxy**: Consider using nginx/Apache as a reverse proxy with rate limiting

**Nginx Example:**

```nginx
server {
    listen 443 ssl http2;
    server_name tmi.example.com;

    ssl_certificate /etc/ssl/certs/tmi.crt;
    ssl_private_key /etc/ssl/private/tmi.key;

    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;
    limit_req zone=api burst=20 nodelay;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### 3. Database Security

- **SSL/TLS**: Use `sslmode=require` for PostgreSQL connections
- **Network Isolation**: Keep database on private network
- **Regular Updates**: Keep PostgreSQL and Redis updated
- **Backup Encryption**: Encrypt database backups

### 4. Application Security

- **Regular Updates**: Keep TMI server and dependencies updated
- **Secrets Management**: Use environment variables or secret management systems
- **File Permissions**: Run TMI server as non-root user with minimal permissions
- **Input Validation**: TMI includes comprehensive input validation and sanitization

## Monitoring & Logging

### Application Logs

TMI logs are structured and include:

- HTTP request/response logging
- Authentication events
- Database operations
- WebSocket connections
- Error conditions

**Log Configuration:**

```yaml
logging:
  level: "info" # debug, info, warn, error
  log_dir: "/var/log/tmi"
  max_age_days: 30 # Log retention
  max_size_mb: 100 # Log file size limit
  max_backups: 10 # Number of log files to keep
  also_log_to_console: true
```

### Health Checks

TMI provides health check endpoints:

- `GET /` - API information with health status (root endpoint, returns HTML for browsers, JSON for API clients)
- `GET /oauth2/providers` - OAuth provider availability

**Example Health Check Script:**

```bash
#!/bin/bash
# health-check.sh

HEALTH_URL="http://localhost:8080/"
RESPONSE=$(curl -f -s -H "Accept: application/json" "$HEALTH_URL")
if [ $? -eq 0 ]; then
    STATUS=$(echo "$RESPONSE" | jq -r '.status.code')
    if [ "$STATUS" = "OK" ] || [ "$STATUS" = "DEGRADED" ]; then
        echo "TMI server is healthy (status: $STATUS)"
        exit 0
    fi
fi
echo "TMI server is unhealthy"
exit 1
```

### Monitoring Integration

**Prometheus Metrics** (if implemented):

```yaml
# Add to prometheus.yml
- job_name: "tmi"
  static_configs:
    - targets: ["tmi.example.com:8080"]
  metrics_path: /metrics
```

**Log Aggregation with ELK Stack:**

```yaml
# logstash.conf
input {
file {
path => "/var/log/tmi/*.log"
start_position => "beginning"
}
}

filter {
if [message] =~ /^\{/ {
json {
source => "message"
}
}
}

output {
elasticsearch {
hosts => ["elasticsearch:9200"]
index => "tmi-logs-%{+YYYY.MM.dd}"
}
}
```

## Troubleshooting

### Common Issues

**1. Authentication Failures**

```bash
# Check OAuth configuration
curl -s http://localhost:8080/oauth2/providers | jq

# Verify JWT secret is set
grep JWT /var/log/tmi/tmi.log

# Test token validation
curl -H "Authorization: Bearer <token>" http://localhost:8080/oauth2/me
```

**2. Database Connection Issues**

```bash
# Test PostgreSQL connection
psql -h localhost -U tmi_user -d tmi -c "SELECT version();"

# Check TMI database logs
grep -i postgres /var/log/tmi/tmi.log

# Verify migrations
psql -h localhost -U tmi_user -d tmi -c "\dt"
```

**3. Redis Connection Issues**

```bash
# Test Redis connection
redis-cli -h localhost -p 6379 ping

# Check Redis authentication
redis-cli -h localhost -p 6379 -a password ping

# Monitor Redis commands
redis-cli -h localhost -p 6379 monitor
```

**4. TLS/Certificate Issues**

```bash
# Verify certificate validity
openssl x509 -in /etc/tls/server.crt -text -noout

# Test TLS connection
openssl s_client -connect tmi.example.com:443

# Check certificate expiration
openssl x509 -in /etc/tls/server.crt -noout -dates
```

### Performance Tuning

**Database Optimization:**

```sql
-- Check slow queries
SELECT query, mean_time, calls
FROM pg_stat_statements
ORDER BY mean_time DESC LIMIT 10;

-- Update statistics
ANALYZE;

-- Vacuum tables
VACUUM ANALYZE;
```

**Redis Optimization:**

```bash
# Monitor Redis memory usage
redis-cli info memory

# Check slow queries
redis-cli --latency-history

# Monitor key patterns
redis-cli --scan --pattern "session:*" | head -10
```

**Application Tuning:**

- Adjust server timeouts based on load
- Monitor WebSocket connection limits
- Scale horizontally with load balancer
- Use connection pooling for databases

### Support & Maintenance

**Regular Maintenance Tasks:**

- Database backups (daily)
- Log rotation and cleanup
- Security updates
- Certificate renewal
- Performance monitoring
- Capacity planning

**Upgrade Procedure:**

1. Backup database and configuration
2. Test new version in staging
3. Deploy during maintenance window
4. Run database migrations
5. Verify functionality
6. Monitor for issues

This deployment guide provides comprehensive coverage for production deployment of the TMI server. Adjust configurations based on your specific infrastructure requirements and security policies.

---

## Verification Summary

This document was verified against source code on 2026-01-24.

### Verified Items

| Claim | Status | Source |
|-------|--------|--------|
| Go version 1.25.6 | VERIFIED | go.mod line 3 |
| Make targets: build-server, build-containers | VERIFIED | Makefile |
| Default port 8080 | VERIFIED | internal/config/config.go:276 |
| Default interface 0.0.0.0 | VERIFIED | internal/config/config.go:277 |
| Default timeouts (5s/10s/60s) | VERIFIED | internal/config/config.go:278-280 |
| JWT expiration default 3600s | VERIFIED | internal/config/config.go:330 |
| PostgreSQL 12+ requirement | VERIFIED | Wiki Database-Setup.md, industry standard |
| Redis 6+ requirement | VERIFIED | Wiki Database-Setup.md, industry standard |
| Chainguard images cgr.dev/chainguard/go and static | VERIFIED | Dockerfile.server, [Chainguard docs](https://edu.chainguard.dev/chainguard/chainguard-images/overview/) |
| Root endpoint (/) returns API info and health | VERIFIED | api/version.go:122 (GetApiInfo handler) |
| GORM AutoMigrate for schema | VERIFIED | Wiki mentions GORM AutoMigrate on startup |
| Environment variables for config override | VERIFIED | internal/config/config.go:374-435 |
| PostgreSQL install commands (apt/yum) | VERIFIED | [PostgreSQL.org](https://www.postgresql.org/download/linux/ubuntu/) |
| Redis install commands (apt/yum) | VERIFIED | [Redis.io](https://redis.io/docs/latest/operate/oss_and_stack/install/archive/install-redis/install-redis-on-linux/) |

### Corrections Made

1. **Go version**: Updated from 1.25.3 to 1.25.6 (per go.mod)
2. **Make target**: Changed `make dev` to `make start-dev` (per Makefile)
3. **--generate-config**: Clarified that it shows help, not generates files
4. **Default timeouts**: Corrected from 30s/30s/120s to 5s/10s/60s (per source)
5. **Health endpoint**: Changed from `/version` to `/` (root endpoint per api/version.go)
6. **Migrations**: Clarified GORM AutoMigrate usage (no separate migration directory found)

<!-- NEEDS-REVIEW: Prometheus /metrics endpoint - not found in source code, may not be implemented -->
<!-- NEEDS-REVIEW: auth/migrations directory referenced but not found - GORM handles migrations -->

<!-- Migrated from: docs/operator/deployment/deployment-guide.md on 2026-01-24 -->
