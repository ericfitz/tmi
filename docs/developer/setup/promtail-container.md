# Promtail Container Setup

This document describes how to build, configure, and run the Promtail log aggregation container for TMI.

## Overview

The TMI project includes a containerized Promtail instance that collects logs from the TMI server and forwards them to a Loki instance (e.g., Grafana Cloud). The container is designed with security in mind, using environment variables for credentials rather than embedding them in the container image.

## Architecture

### Base Image

- **Base**: `cgr.dev/chainguard/bash:latest`
- **Why**: Minimal, secure Chainguard image with bash and core utilities
- **Size**: ~187MB

### Security Features

1. **No Hardcoded Credentials**: Loki credentials are passed at runtime via environment variables
2. **Template-Based Configuration**: Configuration is generated at container startup from a template
3. **Non-Root User**: Container runs as `nonroot:nonroot` user
4. **Read-Only Mounts**: Log directories are mounted read-only

### Log Sources

The container monitors logs from both development and production environments:

- **Development** (local): `./logs/tmi.log` and `./logs/server.log`
- **Production**: `/var/log/tmi/tmi.log`

## Building the Container

### Prerequisites

- Docker
- Git repository access

### Build Process

```bash
# Build the container
make build-promtail
```

This will:
1. Download the latest Promtail release from GitHub (currently v3.5.5)
2. Create a multi-stage build using Chainguard images
3. Copy the configuration template (no secrets)
4. Tag as `tmi/promtail:latest`

## Configuration

### Environment Variables

The container requires the following environment variables:

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `LOKI_URL` | **Yes** | Loki push endpoint (may include embedded credentials) | `https://1234:key@logs.grafana.net/api/prom/push` |
| `LOKI_USERNAME` | No | Basic auth username (if not embedded in URL) | `1234567` |
| `LOKI_PASSWORD` | No | Basic auth password (if not embedded in URL) | `glc_xxxxx` |

### Configuration Methods

#### Method 1: Auto-Detect from Local Config (Recommended for Development)

If you have a `promtail/config.yaml` file (gitignored), the Makefile will automatically extract the Loki URL:

```bash
make start-promtail
```

#### Method 2: Explicit Environment Variables (Recommended for Production)

Pass credentials explicitly:

```bash
# Embedded credentials in URL (Grafana Cloud format)
LOKI_URL="https://1234:glc_xxxxx@logs-prod-036.grafana.net/api/prom/push" make start-promtail

# Separate username and password
LOKI_URL="https://loki.example.com/api/prom/push" \
LOKI_USERNAME="12345" \
LOKI_PASSWORD="secret-key" \
make start-promtail
```

### Configuration Template

The configuration template is located at `promtail/config.yaml.template` and includes:

- **Server Configuration**: HTTP and gRPC ports
- **Positions File**: Tracks log file positions
- **Clients**: Loki endpoint configuration with environment variable substitution
- **Scrape Configs**: Log file paths and labels for dev and prod environments

Example template structure:

```yaml
clients:
  - url: ${LOKI_URL}
    # Optional: basic_auth for username/password authentication
    # basic_auth:
    #   username: ${LOKI_USERNAME}
    #   password: ${LOKI_PASSWORD}

scrape_configs:
  - job_name: tmiserver-dev
    static_configs:
      - targets: [localhost]
        labels:
          environment: development
          __path__: /logs/tmi.log
```

## Running the Container

### Start Promtail

```bash
make start-promtail
```

This will:
1. Check for `LOKI_URL` environment variable
2. If not set, extract URL from `promtail/config.yaml` (if exists)
3. Start container with appropriate volume mounts
4. Generate runtime configuration from template

### Stop Promtail

```bash
make stop-promtail
```

### Remove Promtail Container

```bash
make clean-promtail
```

### Check Container Status

```bash
docker ps --filter "name=promtail"
docker logs promtail
```

## Volume Mounts

The container automatically mounts the following directories:

| Host Path | Container Path | Mode | Purpose |
|-----------|---------------|------|---------|
| `./logs/` | `/logs/` | Read-only | Development logs (tmi.log, server.log) |
| `/var/log/tmi/` | `/var/log/tmi/` | Read-only | Production logs |

## Troubleshooting

### Container Fails to Start

**Symptom**: Container exits immediately

**Cause**: Missing `LOKI_URL` environment variable

**Solution**:
```bash
# Check if promtail/config.yaml exists
ls -la promtail/config.yaml

# Or set LOKI_URL explicitly
LOKI_URL="https://your-loki-url" make start-promtail
```

### No Logs Being Shipped

**Symptom**: Promtail running but logs not appearing in Loki

**Check container logs**:
```bash
docker logs promtail 2>&1 | grep -E "error|fail|Adding target"
```

**Verify log files are being monitored**:
```bash
docker logs promtail 2>&1 | grep "Adding target"
```

Expected output:
```
level=info msg="Adding target" key="/logs/tmi.log:..."
level=info msg="Adding target" key="/logs/server.log:..."
```

**Verify log files exist and have content**:
```bash
ls -lh logs/tmi.log logs/server.log
```

### Authentication Failures

**Symptom**: Errors about authentication in container logs

**Check**:
1. Verify `LOKI_URL` format is correct
2. For Grafana Cloud: Ensure credentials are embedded in URL
3. For self-hosted Loki: Use `LOKI_USERNAME` and `LOKI_PASSWORD`

## Security Best Practices

### DO NOT Include Secrets in Git

The following files are gitignored and should **never** be committed:

- `promtail/config.yaml` (contains actual credentials)

Safe to commit:
- `promtail/config.yaml.template` (template with placeholders)
- `promtail/entrypoint.sh` (entrypoint script)

### Verify Image Doesn't Contain Secrets

Before pushing images to a registry:

```bash
# Check image layers for secrets
docker history tmi/promtail:latest --no-trunc | grep -i "secret\|password\|loki"

# Should return no results
```

### Production Deployment

For production deployments:

1. **Use Environment Variables**: Never hardcode credentials
2. **Use Secrets Management**: Store credentials in Kubernetes Secrets, AWS Secrets Manager, etc.
3. **Rotate Credentials**: Regularly rotate Loki credentials
4. **Monitor Access**: Review Grafana Cloud access logs

## Files Reference

| File | Purpose | Contains Secrets? |
|------|---------|-------------------|
| `Dockerfile.promtail` | Multi-stage container build | ❌ No |
| `promtail/config.yaml.template` | Configuration template | ❌ No |
| `promtail/config.yaml` | Actual config (gitignored) | ⚠️ **Yes** |
| `promtail/entrypoint.sh` | Runtime config generator | ❌ No |
| `scripts/build-promtail-container.sh` | Build automation | ❌ No |

## Make Commands Summary

| Command | Description |
|---------|-------------|
| `make build-promtail` | Build Promtail container image |
| `make start-promtail` | Start Promtail container with log collection |
| `make stop-promtail` | Stop Promtail container |
| `make clean-promtail` | Remove Promtail container and cleanup |

## Development Workflow

### Initial Setup

1. Obtain Loki credentials (e.g., from Grafana Cloud)
2. Create `promtail/config.yaml` from template:
   ```bash
   cp promtail/config.yaml.template promtail/config.yaml
   # Edit and add your Loki URL
   ```
3. Build and start:
   ```bash
   make build-promtail
   make start-promtail
   ```

### Daily Development

```bash
# Start TMI server
make start-dev

# Start Promtail (auto-detects config)
make start-promtail

# Check logs are being collected
docker logs promtail

# View logs in Grafana Cloud/Loki UI
```

### Cleanup

```bash
# Stop containers
make stop-promtail
make clean-dev
```

## Advanced Usage

### Custom Configuration

To use a custom configuration template:

1. Modify `promtail/config.yaml.template`
2. Rebuild container: `make build-promtail`
3. Restart: `make clean-promtail && make start-promtail`

### Testing Configuration

Test the entrypoint script without running the full container:

```bash
# Run entrypoint with test environment
docker run --rm \
  -e LOKI_URL="https://test:key@logs.example.com/push" \
  tmi/promtail:latest \
  cat /tmp/promtail-config.yaml
```

### Debugging

Enable verbose Promtail logging:

```bash
docker run --rm -it \
  -e LOKI_URL="$LOKI_URL" \
  -v $(pwd)/logs:/logs:ro \
  tmi/promtail:latest \
  -log.level=debug
```

## Related Documentation

- [Development Setup](development-setup.md) - TMI server development environment
- [Integration Testing](../testing/integration-testing.md) - Running integration tests
- [Logging Configuration](../../reference/configuration.md#logging) - TMI server logging config

## External Resources

- [Promtail Documentation](https://grafana.com/docs/loki/latest/send-data/promtail/)
- [Grafana Cloud Logs](https://grafana.com/products/cloud/logs/)
- [Chainguard Images](https://www.chainguard.dev/chainguard-images)
