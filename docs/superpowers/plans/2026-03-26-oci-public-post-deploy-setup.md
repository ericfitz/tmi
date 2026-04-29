# OCI Public Post-Deploy Setup Script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Python CLI script that completes OCI public deployment configuration: DNS records, TLS certificates, Google OAuth, CORS, and webhook registration for tmi-tf-wh.

**Architecture:** Single Python script with click CLI framework, reading secrets from a `.env` file. Five phases (verify, dns, certs, configure, webhook) each idempotent and independently runnable. External tools (`oci`, `kubectl`, `dig`) called via subprocess; TMI API calls via `requests`.

**Tech Stack:** Python 3.11+, click, requests, python-dotenv, uv inline script metadata

**Spec:** `docs/superpowers/specs/2026-03-26-oci-public-post-deploy-setup-design.md` — Part 2

**Depends on:** `docs/superpowers/plans/2026-03-26-oci-ingress-controller.md` (ingress controller must be deployed first)

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `scripts/setup-oci-public.py` | Create | Main CLI script with all five phases |
| `scripts/setup-oci-public.env.example` | Create | Template `.env` file (committed) |
| `.gitignore` | Modify | Add `scripts/setup-oci-public.env` pattern |

---

### Task 1: Add .gitignore entry and create .env template

**Files:**
- Modify: `.gitignore`
- Create: `scripts/setup-oci-public.env.example`

- [ ] **Step 1: Add gitignore entry**

Add to the end of `.gitignore`:

```
# OCI public deployment secrets
scripts/setup-oci-public.env
```

- [ ] **Step 2: Create the .env template**

Create `scripts/setup-oci-public.env.example`:

```bash
# OCI Public Post-Deploy Setup Configuration
# Copy to setup-oci-public.env and fill in values.
# This file is gitignored — never commit the real .env file.

# ---------------------------------------------------------------------------
# OCI Configuration
# ---------------------------------------------------------------------------
OCI_PROFILE=tmi
OCI_COMPARTMENT_ID=ocid1.compartment.oc1..example
OCI_DNS_ZONE_ID=ocid1.dns-zone.oc1..example
OCI_REGION=us-ashburn-1

# ---------------------------------------------------------------------------
# Domain
# ---------------------------------------------------------------------------
DOMAIN=oci.tmi.dev
API_HOSTNAME=api.oci.tmi.dev
UX_HOSTNAME=app.oci.tmi.dev

# ---------------------------------------------------------------------------
# ACME / Let's Encrypt
# ---------------------------------------------------------------------------
ACME_EMAIL=admin@example.com
# Use "staging" for testing, "production" for real certs
ACME_DIRECTORY=staging

# ---------------------------------------------------------------------------
# Google OAuth (from Google Cloud Console)
# ---------------------------------------------------------------------------
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=GOCSPX-your-client-secret

# ---------------------------------------------------------------------------
# TMI Admin Bootstrap
# Obtain by: 1) OAuth login as admin (login_hint=charlie)
#            2) POST /me/client_credentials to create credentials
# ---------------------------------------------------------------------------
TMI_CLIENT_ID=tmi_cc_example
TMI_CLIENT_SECRET=your-client-secret

# ---------------------------------------------------------------------------
# Kubernetes
# ---------------------------------------------------------------------------
# Leave empty to use current kubectl context
KUBE_CONTEXT=
TMI_NAMESPACE=tmi
```

- [ ] **Step 3: Commit**

```bash
git add .gitignore scripts/setup-oci-public.env.example
git commit -m "chore: add OCI post-deploy setup env template and gitignore"
```

---

### Task 2: Create script skeleton with click CLI and env loading

**Files:**
- Create: `scripts/setup-oci-public.py`

- [ ] **Step 1: Create the script with uv metadata, imports, env loading, and CLI structure**

Create `scripts/setup-oci-public.py`:

```python
#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "click>=8.0",
#     "requests>=2.31",
#     "python-dotenv>=1.0",
# ]
# ///
"""OCI Public Post-Deploy Setup Script.

Completes deployment-specific configuration after `make deploy-oci`:
  - DNS A records for api/app subdomains
  - TLS certificates via Let's Encrypt (certmgr function)
  - CORS and Google OAuth configuration
  - tmi-tf-wh webhook registration and client credentials

Usage:
    uv run scripts/setup-oci-public.py all
    uv run scripts/setup-oci-public.py verify
    uv run scripts/setup-oci-public.py dns
    uv run scripts/setup-oci-public.py certs
    uv run scripts/setup-oci-public.py configure
    uv run scripts/setup-oci-public.py webhook
"""

import json
import os
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path

import click
import requests
from dotenv import load_dotenv


# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

@dataclass
class Config:
    """Holds all configuration loaded from the .env file."""

    oci_profile: str
    oci_compartment_id: str
    oci_dns_zone_id: str
    oci_region: str
    domain: str
    api_hostname: str
    ux_hostname: str
    acme_email: str
    acme_directory: str
    google_client_id: str
    google_client_secret: str
    tmi_client_id: str
    tmi_client_secret: str
    kube_context: str | None
    tmi_namespace: str

    @classmethod
    def from_env(cls) -> "Config":
        """Load config from environment variables (set by dotenv)."""

        def require(key: str) -> str:
            val = os.environ.get(key, "").strip()
            if not val:
                click.echo(f"ERROR: Required variable {key} is not set in .env file", err=True)
                sys.exit(1)
            return val

        return cls(
            oci_profile=require("OCI_PROFILE"),
            oci_compartment_id=require("OCI_COMPARTMENT_ID"),
            oci_dns_zone_id=require("OCI_DNS_ZONE_ID"),
            oci_region=require("OCI_REGION"),
            domain=require("DOMAIN"),
            api_hostname=require("API_HOSTNAME"),
            ux_hostname=require("UX_HOSTNAME"),
            acme_email=require("ACME_EMAIL"),
            acme_directory=os.environ.get("ACME_DIRECTORY", "staging").strip(),
            google_client_id=require("GOOGLE_CLIENT_ID"),
            google_client_secret=require("GOOGLE_CLIENT_SECRET"),
            tmi_client_id=require("TMI_CLIENT_ID"),
            tmi_client_secret=require("TMI_CLIENT_SECRET"),
            kube_context=os.environ.get("KUBE_CONTEXT", "").strip() or None,
            tmi_namespace=os.environ.get("TMI_NAMESPACE", "tmi").strip(),
        )


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def run_cmd(args: list[str], *, check: bool = True, capture: bool = True) -> subprocess.CompletedProcess:
    """Run a subprocess command and return the result."""
    result = subprocess.run(args, capture_output=capture, text=True)
    if check and result.returncode != 0:
        stderr = result.stderr.strip() if result.stderr else ""
        click.echo(f"ERROR: Command failed: {' '.join(args)}", err=True)
        if stderr:
            click.echo(f"  {stderr}", err=True)
        sys.exit(1)
    return result


def oci_cmd(cfg: Config, args: list[str], **kwargs) -> subprocess.CompletedProcess:
    """Run an OCI CLI command with the configured profile."""
    return run_cmd(["oci", "--profile", cfg.oci_profile, "--region", cfg.oci_region] + args, **kwargs)


def kubectl_cmd(cfg: Config, args: list[str], **kwargs) -> subprocess.CompletedProcess:
    """Run a kubectl command with the configured context and namespace."""
    base = ["kubectl"]
    if cfg.kube_context:
        base += ["--context", cfg.kube_context]
    base += ["--namespace", cfg.tmi_namespace]
    return run_cmd(base + args, **kwargs)


def kubectl_cmd_no_ns(cfg: Config, args: list[str], **kwargs) -> subprocess.CompletedProcess:
    """Run a kubectl command without namespace (for cluster-wide resources)."""
    base = ["kubectl"]
    if cfg.kube_context:
        base += ["--context", cfg.kube_context]
    return run_cmd(base + args, **kwargs)


def get_tmi_token(cfg: Config, api_url: str) -> str:
    """Exchange client credentials for a TMI JWT token."""
    resp = requests.post(
        f"{api_url}/oauth2/token",
        json={
            "grant_type": "client_credentials",
            "client_id": cfg.tmi_client_id,
            "client_secret": cfg.tmi_client_secret,
        },
        timeout=30,
    )
    if resp.status_code != 200:
        click.echo(f"ERROR: Failed to get token: {resp.status_code} {resp.text}", err=True)
        click.echo("  Ensure TMI_CLIENT_ID and TMI_CLIENT_SECRET are valid.", err=True)
        click.echo("  To obtain: 1) OAuth login as admin  2) POST /me/client_credentials", err=True)
        sys.exit(1)
    return resp.json()["access_token"]


def tmi_api(token: str, method: str, url: str, **kwargs) -> requests.Response:
    """Make an authenticated TMI API request."""
    headers = kwargs.pop("headers", {})
    headers["Authorization"] = f"Bearer {token}"
    return requests.request(method, url, headers=headers, timeout=30, **kwargs)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

DEFAULT_ENV_FILE = Path(__file__).parent / "setup-oci-public.env"


@click.group()
@click.option("--env-file", type=click.Path(exists=True), default=None, help="Path to .env file")
@click.option("--dry-run", is_flag=True, default=False, help="Show what would be done without making changes")
@click.pass_context
def cli(ctx, env_file, dry_run):
    """OCI Public Post-Deploy Setup."""
    env_path = env_file or DEFAULT_ENV_FILE
    if not Path(env_path).exists():
        click.echo(f"ERROR: Env file not found: {env_path}", err=True)
        click.echo(f"  Copy scripts/setup-oci-public.env.example to {env_path} and fill in values.", err=True)
        sys.exit(1)
    load_dotenv(env_path, override=True)
    ctx.ensure_object(dict)
    ctx.obj["config"] = Config.from_env()
    ctx.obj["dry_run"] = dry_run


# Phase commands are added in subsequent tasks.
# Placeholder to allow the script to be importable.


if __name__ == "__main__":
    cli()
```

- [ ] **Step 2: Verify the script parses without errors**

Run: `uv run scripts/setup-oci-public.py --help`
Expected: Shows help text with the group description and `--env-file` / `--dry-run` options.

- [ ] **Step 3: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "feat(scripts): create setup-oci-public.py skeleton with env loading and CLI"
```

---

### Task 3: Implement the `verify` phase

**Files:**
- Modify: `scripts/setup-oci-public.py`

- [ ] **Step 1: Add the verify command**

Add before the `if __name__` block in `scripts/setup-oci-public.py`:

```python
# ---------------------------------------------------------------------------
# Phase 0: verify
# ---------------------------------------------------------------------------

def check_tool(name: str, args: list[str]) -> bool:
    """Check if a CLI tool is available."""
    try:
        subprocess.run([name] + args, capture_output=True, text=True, timeout=10)
        return True
    except FileNotFoundError:
        return False


def get_ingress_lb_ip(cfg: Config) -> str | None:
    """Get the public IP of the ingress controller's LoadBalancer service."""
    # The OCI Native Ingress Controller creates a LoadBalancer service.
    # Search all namespaces for it.
    result = kubectl_cmd_no_ns(
        cfg,
        ["get", "svc", "--all-namespaces", "-o", "json"],
        check=False,
    )
    if result.returncode != 0:
        return None
    services = json.loads(result.stdout)
    for svc in services.get("items", []):
        if svc["spec"].get("type") != "LoadBalancer":
            continue
        # The ingress controller's LB service is in the native-ingress-controller-system namespace
        ns = svc["metadata"].get("namespace", "")
        if "ingress" in ns.lower():
            ingress_list = svc.get("status", {}).get("loadBalancer", {}).get("ingress", [])
            if ingress_list and ingress_list[0].get("ip"):
                return ingress_list[0]["ip"]
    return None


@cli.command()
@click.pass_context
def verify(ctx):
    """Phase 0: Verify prerequisites (cluster, pods, tools)."""
    cfg = ctx.obj["config"]
    errors = []

    click.echo("=== Phase 0: Verify Prerequisites ===\n")

    # Check CLI tools
    for tool, test_args in [("oci", ["--version"]), ("kubectl", ["version", "--client"]), ("dig", ["-v"])]:
        if check_tool(tool, test_args):
            click.echo(f"  [OK] {tool} is installed")
        else:
            errors.append(f"{tool} is not installed or not in PATH")
            click.echo(f"  [FAIL] {tool} is not installed")

    # Check OCI CLI profile
    result = oci_cmd(cfg, ["iam", "region", "list", "--output", "json"], check=False)
    if result.returncode == 0:
        click.echo(f"  [OK] OCI CLI profile '{cfg.oci_profile}' works")
    else:
        errors.append(f"OCI CLI profile '{cfg.oci_profile}' failed: {result.stderr.strip()}")
        click.echo(f"  [FAIL] OCI CLI profile '{cfg.oci_profile}' failed")

    # Check kubectl connectivity
    result = kubectl_cmd(cfg, ["get", "namespace", cfg.tmi_namespace, "-o", "name"], check=False)
    if result.returncode == 0:
        click.echo(f"  [OK] kubectl connected, namespace '{cfg.tmi_namespace}' exists")
    else:
        errors.append(f"kubectl cannot reach cluster or namespace '{cfg.tmi_namespace}' not found")
        click.echo(f"  [FAIL] kubectl / namespace check failed")

    # Check tmi-api pods
    result = kubectl_cmd(cfg, ["get", "pods", "-l", "app=tmi-api", "-o", "jsonpath={.items[*].status.phase}"], check=False)
    if result.returncode == 0 and "Running" in (result.stdout or ""):
        click.echo("  [OK] tmi-api pods running")
    else:
        errors.append("tmi-api pods are not running")
        click.echo("  [FAIL] tmi-api pods not running")

    # Check tmi-ux pods (optional)
    result = kubectl_cmd(cfg, ["get", "deployment", "tmi-ux", "-o", "name"], check=False)
    if result.returncode == 0:
        result2 = kubectl_cmd(cfg, ["get", "pods", "-l", "app=tmi-ux", "-o", "jsonpath={.items[*].status.phase}"], check=False)
        if result2.returncode == 0 and "Running" in (result2.stdout or ""):
            click.echo("  [OK] tmi-ux pods running")
        else:
            errors.append("tmi-ux deployment exists but pods are not running")
            click.echo("  [FAIL] tmi-ux pods not running")
    else:
        click.echo("  [SKIP] tmi-ux not deployed")

    # Check tmi-tf-wh pods (optional)
    result = kubectl_cmd(cfg, ["get", "deployment", "tmi-tf-wh", "-o", "name"], check=False)
    if result.returncode == 0:
        result2 = kubectl_cmd(cfg, ["get", "pods", "-l", "app=tmi-tf-wh", "-o", "jsonpath={.items[*].status.phase}"], check=False)
        if result2.returncode == 0 and "Running" in (result2.stdout or ""):
            click.echo("  [OK] tmi-tf-wh pods running")
        else:
            errors.append("tmi-tf-wh deployment exists but pods are not running")
            click.echo("  [FAIL] tmi-tf-wh pods not running")
    else:
        click.echo("  [SKIP] tmi-tf-wh not deployed")

    # Check ingress LB IP
    lb_ip = get_ingress_lb_ip(cfg)
    if lb_ip:
        click.echo(f"  [OK] Ingress LB IP: {lb_ip}")
    else:
        errors.append("Ingress controller LoadBalancer has no public IP")
        click.echo("  [FAIL] Ingress LB IP not found")

    # Check DNS zone accessible
    result = oci_cmd(cfg, ["dns", "zone", "get", "--zone-name-or-id", cfg.oci_dns_zone_id, "--output", "json"], check=False)
    if result.returncode == 0:
        zone_data = json.loads(result.stdout)
        zone_name = zone_data.get("data", {}).get("name", "unknown")
        click.echo(f"  [OK] DNS zone accessible: {zone_name}")
    else:
        errors.append(f"Cannot access DNS zone {cfg.oci_dns_zone_id}")
        click.echo("  [FAIL] DNS zone not accessible")

    click.echo()
    if errors:
        click.echo(f"FAILED: {len(errors)} issue(s) found:", err=True)
        for e in errors:
            click.echo(f"  - {e}", err=True)
        sys.exit(1)
    else:
        click.echo("All prerequisites verified.")
```

- [ ] **Step 2: Test the script help includes the verify command**

Run: `uv run scripts/setup-oci-public.py --help`
Expected: Shows `verify` in the list of commands.

- [ ] **Step 3: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "feat(scripts): add verify phase to setup-oci-public.py"
```

---

### Task 4: Implement the `dns` phase

**Files:**
- Modify: `scripts/setup-oci-public.py`

- [ ] **Step 1: Add the dns command**

Add after the `verify` command in `scripts/setup-oci-public.py`:

```python
# ---------------------------------------------------------------------------
# Phase 1: dns
# ---------------------------------------------------------------------------

def get_dns_records(cfg: Config, hostname: str) -> list[str]:
    """Get current A record IPs for a hostname from OCI DNS."""
    result = oci_cmd(
        cfg,
        ["dns", "record", "rrset", "get",
         "--zone-name-or-id", cfg.oci_dns_zone_id,
         "--domain", hostname,
         "--rtype", "A",
         "--output", "json"],
        check=False,
    )
    if result.returncode != 0:
        return []
    data = json.loads(result.stdout)
    return [r["rdata"] for r in data.get("data", {}).get("items", [])]


def set_dns_record(cfg: Config, hostname: str, ip: str, dry_run: bool) -> None:
    """Create or update an A record in OCI DNS."""
    if dry_run:
        click.echo(f"  [DRY RUN] Would set A record: {hostname} -> {ip}")
        return
    # Use patch-zone-records to upsert
    patch_body = json.dumps({
        "items": [{
            "domain": hostname,
            "rtype": "A",
            "rdata": ip,
            "ttl": 300,
            "operation": "ADD",
        }]
    })
    # First remove existing A records for this domain
    oci_cmd(
        cfg,
        ["dns", "record", "rrset", "delete",
         "--zone-name-or-id", cfg.oci_dns_zone_id,
         "--domain", hostname,
         "--rtype", "A",
         "--force"],
        check=False,  # OK if doesn't exist
    )
    # Then add the new record
    oci_cmd(
        cfg,
        ["dns", "record", "rrset", "update",
         "--zone-name-or-id", cfg.oci_dns_zone_id,
         "--domain", hostname,
         "--rtype", "A",
         "--items", json.dumps([{"domain": hostname, "rdata": ip, "rtype": "A", "ttl": 300}]),
         "--force"],
    )
    click.echo(f"  Set A record: {hostname} -> {ip}")


def wait_for_dns(hostname: str, expected_ip: str, timeout: int = 300, interval: int = 15) -> bool:
    """Poll DNS resolution until hostname resolves to expected IP."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        result = subprocess.run(
            ["dig", "+short", hostname, "A"],
            capture_output=True, text=True, timeout=10,
        )
        resolved = result.stdout.strip().split("\n")
        if expected_ip in resolved:
            return True
        remaining = int(deadline - time.time())
        click.echo(f"  Waiting for DNS propagation... ({remaining}s remaining)")
        time.sleep(interval)
    return False


@cli.command()
@click.pass_context
def dns(ctx):
    """Phase 1: Create DNS A records pointing to ingress LB."""
    cfg = ctx.obj["config"]
    dry_run = ctx.obj["dry_run"]

    click.echo("=== Phase 1: DNS Setup ===\n")

    # Get ingress LB IP
    lb_ip = get_ingress_lb_ip(cfg)
    if not lb_ip:
        click.echo("ERROR: Ingress LB IP not found. Run 'verify' phase first.", err=True)
        sys.exit(1)
    click.echo(f"  Ingress LB IP: {lb_ip}")

    # Check and set DNS records
    changed = False
    for hostname in [cfg.api_hostname, cfg.ux_hostname]:
        existing = get_dns_records(cfg, hostname)
        if existing == [lb_ip]:
            click.echo(f"  [OK] {hostname} -> {lb_ip} (already correct)")
        else:
            if existing:
                click.echo(f"  {hostname} currently points to {existing}, updating...")
            else:
                click.echo(f"  {hostname} has no A record, creating...")
            set_dns_record(cfg, hostname, lb_ip, dry_run)
            changed = True

    if dry_run:
        click.echo("\n  [DRY RUN] Skipping DNS propagation wait.")
        return

    if not changed:
        click.echo("\nDNS records already correct.")
        return

    # Wait for DNS propagation
    click.echo(f"\nWaiting for DNS propagation (up to 5 minutes)...")
    for hostname in [cfg.api_hostname, cfg.ux_hostname]:
        if wait_for_dns(hostname, lb_ip):
            click.echo(f"  [OK] {hostname} resolves to {lb_ip}")
        else:
            click.echo(f"  [WARN] {hostname} did not resolve within timeout.", err=True)
            click.echo("  DNS may still be propagating. Re-run 'dns' phase later.", err=True)
            sys.exit(1)

    click.echo("\nDNS setup complete.")
```

- [ ] **Step 2: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "feat(scripts): add dns phase to setup-oci-public.py"
```

---

### Task 5: Implement the `certs` phase

**Files:**
- Modify: `scripts/setup-oci-public.py`

- [ ] **Step 1: Add the certs command**

Add after the `dns` command:

```python
# ---------------------------------------------------------------------------
# Phase 2: certs
# ---------------------------------------------------------------------------

def get_tls_secret_expiry(cfg: Config) -> str | None:
    """Get the expiry date of the tmi-tls K8s secret, or None if not found."""
    result = kubectl_cmd(
        cfg,
        ["get", "secret", "tmi-tls", "-o", "jsonpath={.data.tls\\.crt}"],
        check=False,
    )
    if result.returncode != 0 or not result.stdout.strip():
        return None
    # Decode base64 cert and check expiry with openssl
    import base64
    import tempfile
    try:
        cert_pem = base64.b64decode(result.stdout.strip())
    except Exception:
        return None
    with tempfile.NamedTemporaryFile(suffix=".pem", delete=False) as f:
        f.write(cert_pem)
        f.flush()
        openssl_result = subprocess.run(
            ["openssl", "x509", "-enddate", "-noout", "-in", f.name],
            capture_output=True, text=True,
        )
        os.unlink(f.name)
    if openssl_result.returncode != 0:
        return None
    # Parse "notAfter=Mar 26 12:00:00 2027 GMT"
    line = openssl_result.stdout.strip()
    if "=" in line:
        return line.split("=", 1)[1]
    return None


def cert_days_remaining(expiry_str: str) -> int:
    """Parse an openssl date string and return days until expiry."""
    from datetime import datetime, timezone
    try:
        expiry = datetime.strptime(expiry_str, "%b %d %H:%M:%S %Y %Z")
        expiry = expiry.replace(tzinfo=timezone.utc)
        now = datetime.now(timezone.utc)
        return (expiry - now).days
    except ValueError:
        return -1


def find_certmgr_function(cfg: Config) -> str | None:
    """Find the certmgr OCI Function OCID."""
    # List functions applications in the compartment
    result = oci_cmd(
        cfg,
        ["fn", "application", "list",
         "--compartment-id", cfg.oci_compartment_id,
         "--output", "json"],
        check=False,
    )
    if result.returncode != 0:
        return None
    apps = json.loads(result.stdout).get("data", [])
    for app in apps:
        if "certmgr" in app.get("display-name", "").lower() or "cert" in app.get("display-name", "").lower():
            # Find the function within this app
            fn_result = oci_cmd(
                cfg,
                ["fn", "function", "list",
                 "--application-id", app["id"],
                 "--output", "json"],
                check=False,
            )
            if fn_result.returncode != 0:
                continue
            functions = json.loads(fn_result.stdout).get("data", [])
            for fn in functions:
                if "certmgr" in fn.get("display-name", "").lower():
                    return fn["id"]
    return None


def get_vault_secret_content(cfg: Config, secret_name_prefix: str, suffix: str) -> str | None:
    """Retrieve a secret's content from OCI Vault by name pattern."""
    # List secrets in compartment
    result = oci_cmd(
        cfg,
        ["vault", "secret", "list",
         "--compartment-id", cfg.oci_compartment_id,
         "--output", "json"],
        check=False,
    )
    if result.returncode != 0:
        return None
    secrets = json.loads(result.stdout).get("data", [])
    target_name = f"{secret_name_prefix}-{suffix}"
    for secret in secrets:
        if secret.get("secret-name", "") == target_name and secret.get("lifecycle-state") == "ACTIVE":
            # Get the secret bundle
            bundle_result = oci_cmd(
                cfg,
                ["secrets", "secret-bundle", "get",
                 "--secret-id", secret["id"],
                 "--output", "json"],
            )
            bundle = json.loads(bundle_result.stdout)
            content = bundle.get("data", {}).get("secret-bundle-content", {})
            if content.get("content-type") == "BASE64":
                import base64
                return base64.b64decode(content["content"]).decode("utf-8")
            return content.get("content")
    return None


@cli.command()
@click.pass_context
def certs(ctx):
    """Phase 2: Issue/renew TLS certificate and create K8s secret."""
    cfg = ctx.obj["config"]
    dry_run = ctx.obj["dry_run"]

    click.echo("=== Phase 2: TLS Certificate Setup ===\n")

    # Check existing cert
    expiry = get_tls_secret_expiry(cfg)
    if expiry:
        days = cert_days_remaining(expiry)
        if days > 7:
            click.echo(f"  [OK] tmi-tls secret exists, expires: {expiry} ({days} days remaining)")
            click.echo("  Certificate is valid. Skipping renewal.")
            return
        else:
            click.echo(f"  tmi-tls secret expires in {days} days. Renewing...")
    else:
        click.echo("  tmi-tls secret not found. Issuing new certificate...")

    if dry_run:
        click.echo("  [DRY RUN] Would invoke certmgr and create K8s TLS secret.")
        return

    # Find certmgr function
    fn_id = find_certmgr_function(cfg)
    if not fn_id:
        click.echo("ERROR: certmgr function not found.", err=True)
        click.echo("  Enable 'enable_certificate_automation = true' in terraform.tfvars", err=True)
        click.echo("  and re-run 'make deploy-oci'.", err=True)
        sys.exit(1)
    click.echo(f"  Found certmgr function: {fn_id}")

    # Invoke certmgr
    click.echo(f"  Invoking certmgr for *.{cfg.domain}...")
    result = oci_cmd(
        cfg,
        ["fn", "function", "invoke",
         "--function-id", fn_id,
         "--file", "-",
         "--body", ""],
    )
    try:
        response = json.loads(result.stdout)
        if response.get("status") == "error":
            click.echo(f"ERROR: certmgr failed: {response.get('error', 'unknown')}", err=True)
            if "rate" in response.get("error", "").lower():
                click.echo("  ACME rate limit hit. Try ACME_DIRECTORY=staging first.", err=True)
            sys.exit(1)
        click.echo(f"  certmgr response: {response.get('message', 'OK')}")
    except json.JSONDecodeError:
        click.echo(f"  certmgr output: {result.stdout[:200]}")

    # Retrieve cert and key from Vault
    click.echo("  Retrieving certificate from OCI Vault...")
    # The certmgr uses name_prefix (default: "tmi") for secret names
    cert_pem = get_vault_secret_content(cfg, "tmi", "certificate")
    key_pem = get_vault_secret_content(cfg, "tmi", "private-key")
    if not cert_pem or not key_pem:
        click.echo("ERROR: Could not retrieve certificate or key from Vault.", err=True)
        click.echo("  Check certmgr function logs and Vault IAM policies.", err=True)
        sys.exit(1)
    click.echo("  [OK] Retrieved certificate and private key from Vault")

    # Create/update K8s TLS secret
    click.echo("  Creating K8s TLS secret...")
    import tempfile
    with tempfile.NamedTemporaryFile(mode="w", suffix=".pem", delete=False) as cert_f, \
         tempfile.NamedTemporaryFile(mode="w", suffix=".pem", delete=False) as key_f:
        cert_f.write(cert_pem)
        cert_f.flush()
        key_f.write(key_pem)
        key_f.flush()
        # Create secret with dry-run to generate YAML, then apply
        create_result = kubectl_cmd(
            cfg,
            ["create", "secret", "tls", "tmi-tls",
             f"--cert={cert_f.name}",
             f"--key={key_f.name}",
             "--dry-run=client",
             "-o", "yaml"],
        )
        os.unlink(cert_f.name)
        os.unlink(key_f.name)
    # Apply the secret
    apply_result = subprocess.run(
        ["kubectl"] + (["--context", cfg.kube_context] if cfg.kube_context else []) +
        ["--namespace", cfg.tmi_namespace, "apply", "-f", "-"],
        input=create_result.stdout,
        capture_output=True, text=True,
    )
    if apply_result.returncode != 0:
        click.echo(f"ERROR: Failed to apply TLS secret: {apply_result.stderr}", err=True)
        sys.exit(1)
    click.echo("  [OK] tmi-tls secret created/updated")

    # Verify HTTPS
    click.echo(f"  Verifying HTTPS at https://{cfg.api_hostname}/...")
    time.sleep(5)  # Give ingress controller a moment to pick up the secret
    try:
        resp = requests.get(f"https://{cfg.api_hostname}/", timeout=10, verify=True)
        click.echo(f"  [OK] HTTPS working: {resp.status_code}")
    except requests.exceptions.SSLError as e:
        click.echo(f"  [WARN] SSL error (cert may still be propagating): {e}", err=True)
        click.echo("  Re-run 'certs' phase if this persists.", err=True)
    except requests.exceptions.ConnectionError:
        click.echo("  [WARN] Connection failed. DNS may not have propagated yet.", err=True)
        click.echo("  Run 'dns' phase first if needed.", err=True)

    click.echo("\nCertificate setup complete.")
```

- [ ] **Step 2: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "feat(scripts): add certs phase to setup-oci-public.py"
```

---

### Task 6: Implement the `configure` phase

**Files:**
- Modify: `scripts/setup-oci-public.py`

- [ ] **Step 1: Add the configure command**

Add after the `certs` command:

```python
# ---------------------------------------------------------------------------
# Phase 3: configure
# ---------------------------------------------------------------------------

def get_api_url(cfg: Config) -> str:
    """Determine the TMI API URL, preferring HTTPS if available."""
    # Try HTTPS first
    try:
        resp = requests.get(f"https://{cfg.api_hostname}/", timeout=5, verify=True)
        return f"https://{cfg.api_hostname}"
    except Exception:
        pass
    # Fall back to port-forward
    click.echo("  HTTPS not available. Using kubectl port-forward...")
    # Start port-forward in background
    pf_proc = subprocess.Popen(
        ["kubectl"] + (["--context", cfg.kube_context] if cfg.kube_context else []) +
        ["--namespace", cfg.tmi_namespace, "port-forward", "svc/tmi-api", "18080:8080"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    time.sleep(2)
    # Verify it works
    try:
        requests.get("http://localhost:18080/", timeout=5)
    except Exception:
        pf_proc.terminate()
        click.echo("ERROR: Cannot reach TMI API via HTTPS or port-forward.", err=True)
        sys.exit(1)
    # Store the process for cleanup
    import atexit
    atexit.register(pf_proc.terminate)
    return "http://localhost:18080"


def get_configmap_data(cfg: Config, name: str) -> dict:
    """Get the data from a K8s ConfigMap."""
    result = kubectl_cmd(cfg, ["get", "configmap", name, "-o", "json"], check=False)
    if result.returncode != 0:
        return {}
    return json.loads(result.stdout).get("data", {})


def patch_configmap(cfg: Config, name: str, updates: dict[str, str], dry_run: bool) -> bool:
    """Patch a ConfigMap with new key-value pairs. Returns True if changes were made."""
    current = get_configmap_data(cfg, name)
    changes = {k: v for k, v in updates.items() if current.get(k) != v}
    if not changes:
        return False
    if dry_run:
        for k, v in changes.items():
            click.echo(f"  [DRY RUN] Would set {k}={v}")
        return True
    patch = json.dumps({"data": {**current, **changes}})
    kubectl_cmd(cfg, ["patch", "configmap", name, "--type", "merge", "-p", patch])
    for k, v in changes.items():
        click.echo(f"  Set {k}={v}")
    return True


def wait_for_rollout(cfg: Config, deployment: str, timeout: int = 120) -> None:
    """Wait for a deployment rollout to complete."""
    kubectl_cmd(cfg, ["rollout", "status", f"deployment/{deployment}", f"--timeout={timeout}s"])


def get_admin_setting(token: str, api_url: str, key: str) -> dict | None:
    """Get a system setting by key."""
    resp = tmi_api(token, "GET", f"{api_url}/admin/settings/{key}")
    if resp.status_code == 200:
        return resp.json()
    return None


def set_admin_setting(token: str, api_url: str, key: str, value: str, value_type: str, description: str) -> None:
    """Create or update a system setting."""
    resp = tmi_api(token, "PUT", f"{api_url}/admin/settings/{key}", json={
        "value": value,
        "type": value_type,
        "description": description,
    })
    if resp.status_code not in (200, 201):
        click.echo(f"ERROR: Failed to set {key}: {resp.status_code} {resp.text}", err=True)
        sys.exit(1)


@cli.command()
@click.pass_context
def configure(ctx):
    """Phase 3: Configure CORS, HTTP webhooks, and Google OAuth."""
    cfg = ctx.obj["config"]
    dry_run = ctx.obj["dry_run"]

    click.echo("=== Phase 3: Configuration ===\n")

    # Determine API URL
    api_url = get_api_url(cfg)
    click.echo(f"  API URL: {api_url}")

    # Get auth token
    click.echo("  Authenticating...")
    token = get_tmi_token(cfg, api_url)
    click.echo("  [OK] Authenticated")

    # --- ConfigMap updates (CORS, HTTP webhooks) ---
    click.echo("\n--- ConfigMap Updates ---")
    configmap_name = "tmi-config"
    updates = {
        "TMI_CORS_ALLOWED_ORIGINS": f"https://{cfg.ux_hostname}",
        "TMI_WEBHOOK_ALLOW_HTTP_TARGETS": "true",
    }
    changed = patch_configmap(cfg, configmap_name, updates, dry_run)
    if changed and not dry_run:
        click.echo("  Restarting tmi-api pods...")
        kubectl_cmd(cfg, ["rollout", "restart", "deployment/tmi-api"])
        wait_for_rollout(cfg, "tmi-api")
        click.echo("  [OK] tmi-api restarted")
        # Re-acquire token after restart
        time.sleep(3)
        token = get_tmi_token(cfg, api_url)
    elif not changed:
        click.echo("  [OK] ConfigMap already has correct values")

    # --- Google OAuth via admin settings API ---
    click.echo("\n--- Google OAuth Configuration ---")
    oauth_settings = {
        "auth.oauth.providers.google.enabled": ("true", "bool", "Enable Google OAuth provider"),
        "auth.oauth.providers.google.client_id": (cfg.google_client_id, "string", "Google OAuth client ID"),
        "auth.oauth.providers.google.client_secret": (cfg.google_client_secret, "string", "Google OAuth client secret"),
        "auth.oauth.providers.google.scopes": ('["openid", "email", "profile"]', "json", "Google OAuth scopes"),
    }

    for key, (value, vtype, desc) in oauth_settings.items():
        existing = get_admin_setting(token, api_url, key)
        # For secrets, we can't compare (masked), so always set
        is_secret = "secret" in key
        if existing and not is_secret:
            existing_value = existing.get("value", "")
            if existing_value == value:
                click.echo(f"  [OK] {key} already set")
                continue
        if dry_run:
            display_value = "<redacted>" if is_secret else value
            click.echo(f"  [DRY RUN] Would set {key} = {display_value}")
        else:
            set_admin_setting(token, api_url, key, value, vtype, desc)
            display_value = "<redacted>" if is_secret else value
            click.echo(f"  Set {key} = {display_value}")

    # Verify Google OAuth is available
    if not dry_run:
        click.echo("\n  Verifying Google OAuth provider...")
        resp = tmi_api(token, "GET", f"{api_url}/oauth2/providers")
        if resp.status_code == 200:
            providers = resp.json()
            provider_ids = [p.get("id") or p.get("provider_id") for p in providers.get("providers", providers.get("items", []))]
            if "google" in provider_ids:
                click.echo("  [OK] Google OAuth provider is active")
            else:
                click.echo("  [WARN] Google OAuth not appearing in provider list yet.", err=True)
                click.echo("  It may take up to 60 seconds for the provider cache to refresh.", err=True)
        else:
            click.echo(f"  [WARN] Could not verify providers: {resp.status_code}", err=True)

    click.echo("\nConfiguration complete.")
```

- [ ] **Step 2: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "feat(scripts): add configure phase to setup-oci-public.py"
```

---

### Task 7: Implement the `webhook` phase

**Files:**
- Modify: `scripts/setup-oci-public.py`

- [ ] **Step 1: Add the webhook command**

Add after the `configure` command:

```python
# ---------------------------------------------------------------------------
# Phase 4: webhook
# ---------------------------------------------------------------------------

def find_webhook_subscription(token: str, api_url: str, name: str) -> dict | None:
    """Find a webhook subscription by name."""
    resp = tmi_api(token, "GET", f"{api_url}/webhooks/subscriptions?limit=100")
    if resp.status_code != 200:
        return None
    for sub in resp.json().get("items", []):
        if sub.get("name") == name:
            return sub
    return None


def wait_for_webhook_active(token: str, api_url: str, webhook_id: str, timeout: int = 180, interval: int = 15) -> bool:
    """Poll webhook subscription status until active."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        resp = tmi_api(token, "GET", f"{api_url}/webhooks/subscriptions/{webhook_id}")
        if resp.status_code == 200:
            status = resp.json().get("status")
            if status == "active":
                return True
            if status == "pending_delete":
                return False
        remaining = int(deadline - time.time())
        click.echo(f"  Waiting for challenge verification... ({remaining}s remaining)")
        time.sleep(interval)
    return False


@cli.command()
@click.pass_context
def webhook(ctx):
    """Phase 4: Register tmi-tf-wh webhook and provision client credentials."""
    cfg = ctx.obj["config"]
    dry_run = ctx.obj["dry_run"]

    click.echo("=== Phase 4: Webhook Registration ===\n")

    # Check tmi-tf-wh is deployed
    result = kubectl_cmd(cfg, ["get", "deployment", "tmi-tf-wh", "-o", "name"], check=False)
    if result.returncode != 0:
        click.echo("  [SKIP] tmi-tf-wh is not deployed. Skipping webhook registration.")
        return

    # Determine API URL and authenticate
    api_url = get_api_url(cfg)
    click.echo(f"  API URL: {api_url}")
    click.echo("  Authenticating...")
    token = get_tmi_token(cfg, api_url)
    click.echo("  [OK] Authenticated")

    # --- Webhook subscription ---
    click.echo("\n--- Webhook Subscription ---")
    webhook_name = "tmi-tf-wh"
    webhook_url = "http://tmi-tf-wh:8080"
    webhook_events = ["repository.created", "repository.updated", "addon.invoked"]

    existing = find_webhook_subscription(token, api_url, webhook_name)
    if existing and existing.get("status") == "active":
        click.echo(f"  [OK] Webhook '{webhook_name}' already exists and is active (id: {existing['id']})")
    elif existing and existing.get("status") == "pending_verification":
        click.echo(f"  Webhook '{webhook_name}' exists but pending verification. Waiting...")
        if wait_for_webhook_active(token, api_url, existing["id"]):
            click.echo("  [OK] Webhook verified and active")
        else:
            click.echo("ERROR: Webhook challenge verification failed.", err=True)
            click.echo("  Check tmi-tf-wh pod logs: kubectl logs -l app=tmi-tf-wh", err=True)
            click.echo("  Verify service reachability: kubectl exec -it <tmi-api-pod> -- curl http://tmi-tf-wh:8080/", err=True)
            sys.exit(1)
    else:
        if dry_run:
            click.echo(f"  [DRY RUN] Would create webhook subscription:")
            click.echo(f"    name: {webhook_name}")
            click.echo(f"    url: {webhook_url}")
            click.echo(f"    events: {webhook_events}")
        else:
            click.echo(f"  Creating webhook subscription '{webhook_name}'...")
            resp = tmi_api(token, "POST", f"{api_url}/webhooks/subscriptions", json={
                "name": webhook_name,
                "url": webhook_url,
                "events": webhook_events,
            })
            if resp.status_code not in (200, 201):
                click.echo(f"ERROR: Failed to create webhook: {resp.status_code} {resp.text}", err=True)
                sys.exit(1)
            webhook_data = resp.json()
            webhook_id = webhook_data["id"]
            click.echo(f"  Created webhook (id: {webhook_id}), waiting for challenge verification...")

            if wait_for_webhook_active(token, api_url, webhook_id):
                click.echo("  [OK] Webhook verified and active")
            else:
                click.echo("ERROR: Webhook challenge verification failed.", err=True)
                click.echo("  Check tmi-tf-wh pod logs: kubectl logs -l app=tmi-tf-wh", err=True)
                click.echo("  Verify service reachability from tmi-api pod.", err=True)
                sys.exit(1)

    # --- Client credentials for tmi-tf-wh ---
    click.echo("\n--- Client Credentials for tmi-tf-wh ---")
    cred_name = "tmi-tf-wh-service"

    # Check if tmi-tf-wh already has credentials configured
    wh_configmap = get_configmap_data(cfg, "tmi-tf-wh-config")
    existing_client_id = wh_configmap.get("TMI_CLIENT_ID", "")
    if existing_client_id.startswith("tmi_cc_"):
        click.echo(f"  [OK] tmi-tf-wh already has client credentials configured ({existing_client_id})")
    else:
        if dry_run:
            click.echo(f"  [DRY RUN] Would create client credentials '{cred_name}' and inject into tmi-tf-wh")
        else:
            # Create client credentials
            click.echo(f"  Creating client credentials '{cred_name}'...")
            resp = tmi_api(token, "POST", f"{api_url}/me/client_credentials", json={
                "name": cred_name,
                "description": "Service credentials for tmi-tf-wh webhook analyzer",
            })
            if resp.status_code not in (200, 201):
                click.echo(f"ERROR: Failed to create client credentials: {resp.status_code} {resp.text}", err=True)
                sys.exit(1)
            cred_data = resp.json()
            new_client_id = cred_data["client_id"]
            new_client_secret = cred_data["client_secret"]
            click.echo(f"  [OK] Created credentials: {new_client_id}")

            # Inject into tmi-tf-wh configmap
            click.echo("  Injecting credentials into tmi-tf-wh-config ConfigMap...")
            patch_configmap(cfg, "tmi-tf-wh-config", {
                "TMI_CLIENT_ID": new_client_id,
                "TMI_CLIENT_SECRET": new_client_secret,
            }, dry_run=False)

            # Restart tmi-tf-wh
            click.echo("  Restarting tmi-tf-wh pods...")
            kubectl_cmd(cfg, ["rollout", "restart", "deployment/tmi-tf-wh"])
            wait_for_rollout(cfg, "tmi-tf-wh")
            click.echo("  [OK] tmi-tf-wh restarted with credentials")

    # Verify tmi-tf-wh health
    if not dry_run:
        click.echo("\n  Verifying tmi-tf-wh health...")
        result = kubectl_cmd(
            cfg,
            ["get", "pods", "-l", "app=tmi-tf-wh", "-o", "jsonpath={.items[*].status.phase}"],
            check=False,
        )
        if result.returncode == 0 and "Running" in (result.stdout or ""):
            click.echo("  [OK] tmi-tf-wh pods running")
        else:
            click.echo("  [WARN] tmi-tf-wh pods may not be healthy. Check pod logs.", err=True)

    click.echo("\nWebhook registration complete.")
```

- [ ] **Step 2: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "feat(scripts): add webhook phase to setup-oci-public.py"
```

---

### Task 8: Implement the `all` command

**Files:**
- Modify: `scripts/setup-oci-public.py`

- [ ] **Step 1: Add the `all` command**

Add after the `webhook` command and before `if __name__`:

```python
# ---------------------------------------------------------------------------
# All phases
# ---------------------------------------------------------------------------

@cli.command(name="all")
@click.pass_context
def run_all(ctx):
    """Run all phases in order: verify -> dns -> certs -> configure -> webhook."""
    phases = [
        ("verify", verify),
        ("dns", dns),
        ("certs", certs),
        ("configure", configure),
        ("webhook", webhook),
    ]

    for name, cmd in phases:
        click.echo(f"\n{'=' * 60}")
        click.echo(f"Running phase: {name}")
        click.echo(f"{'=' * 60}\n")
        ctx.invoke(cmd)
        click.echo()

    click.echo("=" * 60)
    click.echo("All phases completed successfully!")
    click.echo("=" * 60)
    dry_run = ctx.obj["dry_run"]
    if not dry_run:
        cfg = ctx.obj["config"]
        click.echo(f"\n  API:  https://{cfg.api_hostname}")
        click.echo(f"  App:  https://{cfg.ux_hostname}")
        click.echo(f"\n  Google OAuth configured and available.")
        click.echo(f"  tmi-tf-wh webhook registered and active.")
```

- [ ] **Step 2: Verify the script shows all commands**

Run: `uv run scripts/setup-oci-public.py --help`
Expected: Shows commands: `all`, `certs`, `configure`, `dns`, `verify`, `webhook`

- [ ] **Step 3: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "feat(scripts): add 'all' command to run all phases sequentially"
```

---

### Task 9: Test script locally (dry-run mode)

This task verifies the script works end-to-end in dry-run mode without requiring a live OCI deployment.

- [ ] **Step 1: Create a test .env file**

Create `scripts/setup-oci-public.env` (this file is gitignored) with placeholder values:

```bash
OCI_PROFILE=tmi
OCI_COMPARTMENT_ID=ocid1.compartment.oc1..test
OCI_DNS_ZONE_ID=ocid1.dns-zone.oc1..test
OCI_REGION=us-ashburn-1
DOMAIN=oci.tmi.dev
API_HOSTNAME=api.oci.tmi.dev
UX_HOSTNAME=app.oci.tmi.dev
ACME_EMAIL=test@example.com
ACME_DIRECTORY=staging
GOOGLE_CLIENT_ID=test-client-id
GOOGLE_CLIENT_SECRET=test-client-secret
TMI_CLIENT_ID=tmi_cc_test
TMI_CLIENT_SECRET=test-secret
KUBE_CONTEXT=
TMI_NAMESPACE=tmi
```

- [ ] **Step 2: Run each command with --help**

Run: `uv run scripts/setup-oci-public.py verify --help`
Expected: Shows help for verify phase

Run: `uv run scripts/setup-oci-public.py dns --help`
Expected: Shows help for dns phase

Run: `uv run scripts/setup-oci-public.py all --help`
Expected: Shows help for all command

- [ ] **Step 3: Delete the test .env file**

```bash
rm scripts/setup-oci-public.env
```

- [ ] **Step 4: Final commit with any syntax fixes**

```bash
git add scripts/setup-oci-public.py
git commit -m "fix(scripts): polish setup-oci-public.py after dry-run testing"
```
