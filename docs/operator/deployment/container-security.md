# TMI Container Security Guide

This document describes the enhanced container security features integrated with Docker Scout for automated vulnerability detection and patching.

## Overview

The TMI project now includes comprehensive container security scanning and automated patching capabilities using Docker Scout. This system:

- **Scans** all container images for critical and high-severity vulnerabilities
- **Patches** vulnerabilities during the build process
- **Reports** security findings in multiple formats
- **Enforces** security thresholds in CI/CD pipelines
- **Monitors** runtime security posture

## Quick Start

### Basic Security Scan

```bash
# Scan existing containers for vulnerabilities
make containers-security-scan

# Generate comprehensive security report
make containers-security-report

# View the security summary
cat security-reports/security-summary.md
```

### Build Secure Containers

```bash
# Build all containers with security patches
make containers-secure-build

# Build specific secure container
./scripts/build-secure-containers.sh postgresql

# Start development with secure containers
make containers-secure-dev
```

### Full Security Workflow

```bash
# Complete security build and reporting
make containers-secure
```

## Security Features

### 1. Enhanced Dockerfiles

The project includes security-enhanced Dockerfiles:

- **`Dockerfile.postgres.secure`** - PostgreSQL with security patches
- **`Dockerfile.redis.secure`** - Redis with security patches  
- **`Dockerfile.dev.secure`** - Application with security hardening

#### Security Improvements:

- **Vulnerability Patching**: Automatically updates packages to fix known CVEs
- **Non-root Users**: Runs containers as non-privileged users
- **Minimal Attack Surface**: Removes unnecessary packages and tools
- **Security Metadata**: Labels for tracking patch levels and scan dates
- **Enhanced Logging**: Security-focused logging configuration

### 2. Automated Security Scanning

#### Docker Scout Integration

```bash
# Scan specific image
docker scout cves cgr.dev/chainguard/postgres:latest --only-severity critical,high

# Scan with custom output
docker scout cves my-image:latest --format sarif --output security.sarif
```

#### Makefile Targets

| Target | Description |
|--------|-------------|
| `containers-security-scan` | Scan images for vulnerabilities |
| `containers-security-report` | Generate comprehensive reports |
| `containers-secure-build` | Build patched containers |
| `containers-secure-dev` | Start development with secure containers |
| `containers-secure` | Full security workflow |

### 3. CI/CD Integration

#### Automated Scanning Script

```bash
# Basic CI scan
./scripts/ci-security-scan.sh

# Custom thresholds
MAX_CRITICAL_CVES=0 MAX_HIGH_CVES=5 ./scripts/ci-security-scan.sh

# Scan custom images
IMAGES_TO_SCAN="my-app:latest redis:7" ./scripts/ci-security-scan.sh
```

#### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MAX_CRITICAL_CVES` | 0 | Maximum critical CVEs allowed |
| `MAX_HIGH_CVES` | 3 | Maximum high CVEs allowed |
| `MAX_MEDIUM_CVES` | 10 | Maximum medium CVEs allowed |
| `FAIL_ON_CRITICAL` | true | Fail build on critical CVEs |
| `FAIL_ON_HIGH` | false | Fail build on high CVEs |
| `IMAGES_TO_SCAN` | (default set) | Images to scan |
| `ARTIFACT_DIR` | ./security-artifacts | Output directory |

### 4. Security Reports

#### Report Types

1. **Summary Report** (`security-summary.md`)
   - High-level vulnerability counts
   - Pass/fail status by image
   - Remediation recommendations

2. **Detailed Scan Results** (`security-scan-results.json`)
   - Complete vulnerability details
   - CVSS scores and vectors
   - Affected packages and versions

3. **SARIF Reports** (`security-results.sarif`)
   - Standard format for security tools
   - Integration with IDEs and CI/CD
   - Machine-readable results

#### Sample Summary Report

```markdown
# TMI Container Security Report

**Generated:** 2025-09-19T21:17:37Z
**Scanner:** Docker Scout

## Vulnerability Summary

| Image | Critical | High | Status |
|-------|----------|------|--------|
| postgresql | 0 | 2 | ✅ Good |
| redis | 0 | 1 | ✅ Good |
| application | 0 | 0 | ✅ Good |

## Recommendations

1. Use `make containers-secure-build` to build patched containers
2. Regularly update base images
3. Implement runtime security monitoring
4. Review detailed scan results in security-reports/
```

### 5. Security Thresholds

#### Default Thresholds

- **Critical CVEs**: 0 allowed (build fails)
- **High CVEs**: 3 allowed (warning only)
- **Medium CVEs**: 10 allowed (informational)

#### Customizing Thresholds

```bash
# Strict security policy
MAX_CRITICAL_CVES=0 MAX_HIGH_CVES=0 make containers-security-scan

# Lenient policy for development
MAX_HIGH_CVES=10 FAIL_ON_HIGH=false make containers-security-scan
```

## Security Workflows

### Development Workflow

1. **Daily Development**
   ```bash
   # Start secure development environment
   make containers-secure-dev
   ```

2. **Before Committing**
   ```bash
   # Run security checks
   make containers-security-report
   
   # Review security summary
   cat security-reports/security-summary.md
   ```

3. **Weekly Security Review**
   ```bash
   # Build updated secure containers
   make containers-secure-build
   
   # Compare vulnerability trends
   diff security-reports/security-summary.md.old security-reports/security-summary.md
   ```

### CI/CD Pipeline Integration

#### GitHub Actions Example

```yaml
name: Container Security Scan
on: [push, pull_request]

jobs:
  security-scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install Docker Scout
        run: |
          curl -sSfL https://raw.githubusercontent.com/docker/scout-cli/main/install.sh | sh -s --
          
      - name: Run Security Scan
        run: |
          ./scripts/ci-security-scan.sh
          
      - name: Upload Security Reports
        uses: actions/upload-artifact@v3
        with:
          name: security-reports
          path: security-artifacts/
          
      - name: Comment PR with Security Results
        if: github.event_name == 'pull_request'
        run: |
          # Post security summary as PR comment
          gh pr comment ${{ github.event.number }} --body-file security-artifacts/security-summary.md
```

#### GitLab CI Example

```yaml
container-security:
  stage: security
  script:
    - ./scripts/ci-security-scan.sh
  artifacts:
    reports:
      sast: security-artifacts/security-results.sarif
    paths:
      - security-artifacts/
  only:
    - merge_requests
    - main
```

### Production Deployment

1. **Pre-deployment Validation**
   ```bash
   # Ensure all containers pass security scan
   MAX_CRITICAL_CVES=0 MAX_HIGH_CVES=2 ./scripts/ci-security-scan.sh
   ```

2. **Secure Container Deployment**
   ```bash
   # Use secure container images
   docker run tmi/tmi-postgresql:latest
   docker run tmi/tmi-redis:latest
   docker run tmi/tmi-server:latest
   ```

3. **Runtime Monitoring**
   ```bash
   # Monitor container security logs
   docker logs tmi-postgresql-secure | grep -i security
   docker logs tmi-redis-secure | grep -i security
   ```

## Troubleshooting

### Common Issues

#### 1. Docker Scout Not Available

```bash
# Install Docker Scout
curl -sSfL https://raw.githubusercontent.com/docker/scout-cli/main/install.sh | sh -s --

# Verify installation
docker scout version
```

#### 2. High Vulnerability Count

```bash
# Check for available updates
docker scout recommendations cgr.dev/chainguard/postgres:latest

# Build with latest patches
./scripts/build-secure-containers.sh postgresql

# Verify improvement
docker scout cves tmi/tmi-postgresql:latest
```

#### 3. Build Failures Due to Security Policies

```bash
# Check specific vulnerabilities
docker scout cves my-image:latest --details

# Adjust thresholds temporarily
MAX_HIGH_CVES=10 make containers-security-scan

# Review and patch specific issues
# Edit Dockerfile.*.secure files to add specific patches
```

#### 4. SARIF Processing Issues

```bash
# Check if jq is installed
which jq || brew install jq  # macOS
which jq || apt-get install jq  # Debian/Ubuntu

# Validate SARIF format
jq . security-artifacts/security-results.sarif
```

## Security Best Practices

### 1. Regular Updates

- **Weekly**: Update base images and rebuild containers
- **Monthly**: Review security reports and trends
- **Quarterly**: Audit security thresholds and policies

### 2. Layered Security

- **Image Security**: Use patched base images
- **Runtime Security**: Implement resource limits and monitoring
- **Network Security**: Use proper network segmentation
- **Access Control**: Implement least-privilege principles

### 3. Monitoring and Alerting

```bash
# Set up automated scanning
crontab -e
# Add: 0 2 * * * /path/to/tmi/scripts/ci-security-scan.sh

# Monitor security logs
tail -f security-artifacts/security-summary.md
```

### 4. Incident Response

1. **High/Critical Vulnerabilities Detected**
   - Immediately rebuild affected containers
   - Test patched versions in development
   - Schedule maintenance window for production updates

2. **Security Scan Failures**
   - Review detailed vulnerability reports
   - Prioritize based on CVSS scores and exploitability
   - Implement compensating controls if patches unavailable

## Security Contacts

- **Security Team**: security@tmi.local
- **DevOps Team**: devops@tmi.local
- **Emergency**: security-incident@tmi.local

## Related Documentation

- [Docker Scout Documentation](https://docs.docker.com/scout/)
- [Container Security Guide](https://kubernetes.io/docs/concepts/security/)
- [NIST Container Security](https://csrc.nist.gov/publications/detail/sp/800-190/final)
- [TMI Development Guide](./DEVELOPMENT.md)

---

*Last Updated: September 2025*
*Version: 1.0*