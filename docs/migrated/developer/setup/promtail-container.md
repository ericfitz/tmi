# Promtail Container Setup

<!-- Migrated from: docs/developer/setup/promtail-container.md on 2025-01-24 -->

This document describes how to build, configure, and run the Promtail log aggregation container for TMI.

> **Important**: Promtail is entering End-of-Life (EOL) phase and is expected to reach EOL on March 2, 2026. All future feature development will occur in [Grafana Alloy](https://grafana.com/docs/alloy/latest/). Consider migrating to Alloy for new deployments.

## Overview

The TMI project includes a containerized Promtail instance that collects logs from the TMI server and forwards them to a Loki instance (e.g., Grafana Cloud). The container is designed with security in mind, using environment variables for credentials rather than embedding them in the container image.

## Architecture

### Base Image

- **Fetcher Stage**: `cgr.dev/chainguard/wolfi-base:latest` - Downloads and extracts Promtail binary
- **Runtime Stage**: `busybox:1.37-glibc` - Minimal runtime with POSIX shell support
- **Why**: Minimal attack surface with distroless-style security

### Security Features

1. **No Hardcoded Credentials**: Loki credentials are passed at runtime via environment variables
2. **Template-Based Configuration**: Configuration is generated at container startup from a template
3. **Non-Root User**: Container runs as user `65534:65534` (busybox nobody user)
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
1. Download the latest Promtail release from GitHub (auto-detected from GitHub API)
2. Create a multi-stage build using Chainguard wolfi-base for fetching
3. Use minimal busybox runtime for final image
4. Copy the entrypoint script (no secrets)
5. Tag as `tmi/promtail:latest`

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

If you have a `promtail/config.yaml` file (not tracked by git), the Makefile will automatically extract the Loki URL:

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

- **Server Configuration**: HTTP and gRPC ports (set to 0 for auto-assignment)
- **Positions File**: Tracks log file positions at `/tmp/positions.yaml`
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

The following files should **never** be committed (not tracked by git via whitelist pattern):

- `promtail/config.yaml` (contains actual credentials)

Safe to commit (tracked by git):
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
| `Dockerfile.promtail` | Multi-stage container build | No |
| `promtail/config.yaml.template` | Configuration template | No |
| `promtail/config.yaml` | Actual config (not tracked) | **Yes** |
| `promtail/entrypoint.sh` | Runtime config generator | No |
| `scripts/build-promtail-container.sh` | Build automation | No |

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
<!-- NEEDS-REVIEW: docs/developer/testing/integration-testing.md does not exist -->
<!-- NEEDS-REVIEW: docs/reference/configuration.md does not exist -->

## External Resources

- [Promtail Documentation](https://grafana.com/docs/loki/latest/send-data/promtail/)
- [Grafana Cloud Logs](https://grafana.com/products/cloud/logs/)
- [Chainguard Images](https://www.chainguard.dev/chainguard-images)
- [Grafana Alloy](https://grafana.com/docs/alloy/latest/) - Promtail's successor

---

## Verification Summary

**Verified on**: 2025-01-24

### File References
| Item | Status | Notes |
|------|--------|-------|
| `Dockerfile.promtail` | Verified | Exists at project root |
| `promtail/config.yaml.template` | Verified | Exists |
| `promtail/config.yaml` | Verified | Not tracked by git (whitelist pattern) |
| `promtail/entrypoint.sh` | Verified | Exists, tracked by git |
| `scripts/build-promtail-container.sh` | Verified | Exists |
| `docs/developer/setup/development-setup.md` | Verified | Exists |
| `docs/developer/testing/integration-testing.md` | Not Found | Marked for review |
| `docs/reference/configuration.md` | Not Found | Marked for review |

### Make Targets
| Target | Status | Notes |
|--------|--------|-------|
| `make build-promtail` | Verified | Line 1083-1085 in Makefile |
| `make start-promtail` | Verified | Line 1087-1116 in Makefile |
| `make stop-promtail` | Verified | Line 1118-1121 in Makefile |
| `make clean-promtail` | Verified | Line 1123-1126 in Makefile |

### Source Code Verification
| Claim | Status | Notes |
|-------|--------|-------|
| Base image `cgr.dev/chainguard/bash:latest` | Corrected | Actually uses `cgr.dev/chainguard/wolfi-base:latest` for fetcher, `busybox:1.37-glibc` for runtime |
| Non-root user `nonroot:nonroot` | Corrected | Actually runs as `65534:65534` (busybox nobody) |
| Promtail version v3.5.5 | Corrected | Now auto-detected from GitHub API (latest release) |
| Container size ~187MB | Not Verified | Size varies based on Promtail version |

### External Resources
| URL | Status | Notes |
|-----|--------|-------|
| Promtail Documentation | Verified | Official Grafana docs, confirmed via web search |
| Chainguard Images | Verified | Official site confirmed via WebFetch |
| Grafana Cloud Logs | Verified | URL resolves (standard Grafana product) |

### Corrections Made
1. **Base Image**: Changed from `cgr.dev/chainguard/bash:latest` to accurate multi-stage description (wolfi-base fetcher + busybox runtime)
2. **User**: Changed from `nonroot:nonroot` to `65534:65534` (busybox nobody user)
3. **Promtail Version**: Removed hardcoded v3.5.5, now auto-detected from GitHub API
4. **Gitignore Note**: Clarified that `promtail/config.yaml` is not tracked due to whitelist pattern (not explicit gitignore entry)
5. **Deprecation Notice**: Added warning about Promtail EOL (March 2, 2026) and recommendation to consider Grafana Alloy

### Items Needing Review
- `docs/developer/testing/integration-testing.md` - File does not exist
- `docs/reference/configuration.md` - File does not exist
