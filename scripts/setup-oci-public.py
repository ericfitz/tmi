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

Usage:
    uv run scripts/setup-oci-public.py all
    uv run scripts/setup-oci-public.py verify
    uv run scripts/setup-oci-public.py dns
    uv run scripts/setup-oci-public.py certs
    uv run scripts/setup-oci-public.py configure
"""

import base64
import json
import os
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

import click  # type: ignore # ty:ignore[unresolved-import]
import requests  # ty:ignore[unresolved-import]
from dotenv import load_dotenv  # type: ignore # ty:ignore[unresolved-import]

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
                click.echo(
                    f"ERROR: Required variable {key} is not set in .env file", err=True
                )
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


def run_cmd(
    args: list[str], *, check: bool = True, capture: bool = True
) -> subprocess.CompletedProcess:
    """Run a subprocess command and return the result."""
    result = subprocess.run(args, capture_output=capture, text=True, check=False)
    if check and result.returncode != 0:
        stderr = result.stderr.strip() if result.stderr else ""
        click.echo(f"ERROR: Command failed: {' '.join(args)}", err=True)
        if stderr:
            click.echo(f"  {stderr}", err=True)
        sys.exit(1)
    return result


def oci_cmd(cfg: Config, args: list[str], **kwargs) -> subprocess.CompletedProcess:
    """Run an OCI CLI command with the configured profile."""
    return run_cmd(
        ["oci", "--profile", cfg.oci_profile, "--region", cfg.oci_region] + args,
        **kwargs,
    )


def kubectl_cmd(cfg: Config, args: list[str], **kwargs) -> subprocess.CompletedProcess:
    """Run a kubectl command with the configured context and namespace."""
    base = ["kubectl"]
    if cfg.kube_context:
        base += ["--context", cfg.kube_context]
    base += ["--namespace", cfg.tmi_namespace]
    return run_cmd(base + args, **kwargs)


def kubectl_cmd_no_ns(
    cfg: Config, args: list[str], **kwargs
) -> subprocess.CompletedProcess:
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
        click.echo(
            f"ERROR: Failed to get token: {resp.status_code} {resp.text}", err=True
        )
        click.echo("  Ensure TMI_CLIENT_ID and TMI_CLIENT_SECRET are valid.", err=True)
        click.echo(
            "  To obtain: 1) OAuth login as admin  2) POST /me/client_credentials",
            err=True,
        )
        sys.exit(1)
    return resp.json()["access_token"]


def tmi_api(token: str, method: str, url: str, **kwargs) -> requests.Response:
    """Make an authenticated TMI API request."""
    headers = kwargs.pop("headers", {})
    headers["Authorization"] = f"Bearer {token}"
    return requests.request(method, url, headers=headers, timeout=30, **kwargs)


def get_ingress_lb_ip(cfg: Config) -> str | None:
    """Get the public IP of the ingress controller's LoadBalancer service."""
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
        ns = svc["metadata"].get("namespace", "")
        if "ingress" in ns.lower():
            ingress_list = (
                svc.get("status", {}).get("loadBalancer", {}).get("ingress", [])
            )
            if ingress_list and ingress_list[0].get("ip"):
                return ingress_list[0]["ip"]
    return None


def get_api_url(cfg: Config) -> str:
    """Determine the TMI API URL, preferring HTTPS if available."""
    try:
        requests.get(f"https://{cfg.api_hostname}/", timeout=5, verify=True)
        return f"https://{cfg.api_hostname}"
    except (requests.exceptions.RequestException, OSError):
        pass  # HTTPS not available, fall back to port-forward
    # Fall back to port-forward
    click.echo("  HTTPS not available. Using kubectl port-forward...")
    import atexit

    pf_proc = subprocess.Popen(
        ["kubectl"]
        + (["--context", cfg.kube_context] if cfg.kube_context else [])
        + [
            "--namespace",
            cfg.tmi_namespace,
            "port-forward",
            "svc/tmi-api",
            "18080:8080",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    atexit.register(pf_proc.terminate)
    time.sleep(2)
    try:
        requests.get("http://localhost:18080/", timeout=5)
    except (requests.exceptions.RequestException, OSError):
        pf_proc.terminate()
        click.echo("ERROR: Cannot reach TMI API via HTTPS or port-forward.", err=True)
        sys.exit(1)
    return "http://localhost:18080"


def get_configmap_data(cfg: Config, name: str) -> dict:
    """Get the data from a K8s ConfigMap."""
    result = kubectl_cmd(cfg, ["get", "configmap", name, "-o", "json"], check=False)
    if result.returncode != 0:
        return {}
    return json.loads(result.stdout).get("data", {})


def patch_configmap(
    cfg: Config, name: str, updates: dict[str, str], dry_run: bool
) -> bool:
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
    kubectl_cmd(
        cfg, ["rollout", "status", f"deployment/{deployment}", f"--timeout={timeout}s"]
    )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

DEFAULT_ENV_FILE = Path(__file__).parent / "setup-oci-public.env"


@click.group(invoke_without_command=False)
@click.option("--env-file", type=click.Path(), default=None, help="Path to .env file")
@click.option(
    "--dry-run",
    is_flag=True,
    default=False,
    help="Show what would be done without making changes",
)
@click.pass_context
def cli(ctx, env_file, dry_run):
    """OCI Public Post-Deploy Setup."""
    ctx.ensure_object(dict)
    ctx.obj["dry_run"] = dry_run
    ctx.obj["env_file"] = env_file

    # Skip env loading if just requesting help
    if "--help" in sys.argv or "-h" in sys.argv:
        return

    env_path = Path(env_file) if env_file else DEFAULT_ENV_FILE
    if not env_path.exists():
        click.echo(f"ERROR: Env file not found: {env_path}", err=True)
        click.echo(
            f"  Copy scripts/setup-oci-public.env.example to {env_path} and fill in values.",
            err=True,
        )
        sys.exit(1)
    load_dotenv(str(env_path), override=True)
    ctx.obj["config"] = Config.from_env()


# ---------------------------------------------------------------------------
# Phase 0: verify
# ---------------------------------------------------------------------------


def check_tool(name: str, args: list[str]) -> bool:
    """Check if a CLI tool is available."""
    try:
        subprocess.run([name] + args, capture_output=True, text=True, timeout=10, check=False)
        return True
    except FileNotFoundError:
        return False


@cli.command()
@click.pass_context
def verify(ctx):
    """Phase 0: Verify prerequisites (cluster, pods, tools)."""
    cfg = ctx.obj["config"]
    errors = []

    click.echo("=== Phase 0: Verify Prerequisites ===\n")

    # Check CLI tools
    for tool, test_args in [
        ("oci", ["--version"]),
        ("kubectl", ["version", "--client"]),
        ("dig", ["-v"]),
    ]:
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
        errors.append(
            f"OCI CLI profile '{cfg.oci_profile}' failed: {result.stderr.strip()}"
        )
        click.echo(f"  [FAIL] OCI CLI profile '{cfg.oci_profile}' failed")

    # Check kubectl connectivity
    result = kubectl_cmd(
        cfg, ["get", "namespace", cfg.tmi_namespace, "-o", "name"], check=False
    )
    if result.returncode == 0:
        click.echo(f"  [OK] kubectl connected, namespace '{cfg.tmi_namespace}' exists")
    else:
        errors.append(
            f"kubectl cannot reach cluster or namespace '{cfg.tmi_namespace}' not found"
        )
        click.echo("  [FAIL] kubectl / namespace check failed")

    # Check tmi-api pods
    result = kubectl_cmd(
        cfg,
        ["get", "pods", "-l", "app=tmi-api", "-o", "jsonpath={.items[*].status.phase}"],
        check=False,
    )
    if result.returncode == 0 and "Running" in (result.stdout or ""):
        click.echo("  [OK] tmi-api pods running")
    else:
        errors.append("tmi-api pods are not running")
        click.echo("  [FAIL] tmi-api pods not running")

    # Check tmi-ux pods (optional)
    result = kubectl_cmd(
        cfg, ["get", "deployment", "tmi-ux", "-o", "name"], check=False
    )
    if result.returncode == 0:
        result2 = kubectl_cmd(
            cfg,
            [
                "get",
                "pods",
                "-l",
                "app=tmi-ux",
                "-o",
                "jsonpath={.items[*].status.phase}",
            ],
            check=False,
        )
        if result2.returncode == 0 and "Running" in (result2.stdout or ""):
            click.echo("  [OK] tmi-ux pods running")
        else:
            errors.append("tmi-ux deployment exists but pods are not running")
            click.echo("  [FAIL] tmi-ux pods not running")
    else:
        click.echo("  [SKIP] tmi-ux not deployed")

    # Check ingress LB IP
    lb_ip = get_ingress_lb_ip(cfg)
    if lb_ip:
        click.echo(f"  [OK] Ingress LB IP: {lb_ip}")
    else:
        errors.append("Ingress controller LoadBalancer has no public IP")
        click.echo("  [FAIL] Ingress LB IP not found")

    # Check DNS zone accessible
    result = oci_cmd(
        cfg,
        [
            "dns",
            "zone",
            "get",
            "--zone-name-or-id",
            cfg.oci_dns_zone_id,
            "--output",
            "json",
        ],
        check=False,
    )
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


# ---------------------------------------------------------------------------
# Phase 1: dns
# ---------------------------------------------------------------------------


def get_dns_records(cfg: Config, hostname: str) -> list[str]:
    """Get current A record IPs for a hostname from OCI DNS."""
    result = oci_cmd(
        cfg,
        [
            "dns",
            "record",
            "rrset",
            "get",
            "--zone-name-or-id",
            cfg.oci_dns_zone_id,
            "--domain",
            hostname,
            "--rtype",
            "A",
            "--output",
            "json",
        ],
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
    # Remove existing A records for this domain
    oci_cmd(
        cfg,
        [
            "dns",
            "record",
            "rrset",
            "delete",
            "--zone-name-or-id",
            cfg.oci_dns_zone_id,
            "--domain",
            hostname,
            "--rtype",
            "A",
            "--force",
        ],
        check=False,
    )
    # Add the new record
    oci_cmd(
        cfg,
        [
            "dns",
            "record",
            "rrset",
            "update",
            "--zone-name-or-id",
            cfg.oci_dns_zone_id,
            "--domain",
            hostname,
            "--rtype",
            "A",
            "--items",
            json.dumps([{"domain": hostname, "rdata": ip, "rtype": "A", "ttl": 300}]),
            "--force",
        ],
    )
    click.echo(f"  Set A record: {hostname} -> {ip}")


def wait_for_dns(
    hostname: str, expected_ip: str, timeout: int = 300, interval: int = 15
) -> bool:
    """Poll DNS resolution until hostname resolves to expected IP."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        result = subprocess.run(
            ["dig", "+short", hostname, "A"],
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
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

    lb_ip = get_ingress_lb_ip(cfg)
    if not lb_ip:
        click.echo(
            "ERROR: Ingress LB IP not found. Run 'verify' phase first.", err=True
        )
        sys.exit(1)
    assert isinstance(lb_ip, str)  # narrowing for type checkers
    click.echo(f"  Ingress LB IP: {lb_ip}")

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

    click.echo("\nWaiting for DNS propagation (up to 5 minutes)...")
    for hostname in [cfg.api_hostname, cfg.ux_hostname]:
        if wait_for_dns(hostname, lb_ip):
            click.echo(f"  [OK] {hostname} resolves to {lb_ip}")
        else:
            click.echo(f"  [WARN] {hostname} did not resolve within timeout.", err=True)
            click.echo(
                "  DNS may still be propagating. Re-run 'dns' phase later.", err=True
            )
            sys.exit(1)

    click.echo("\nDNS setup complete.")


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
    try:
        cert_pem = base64.b64decode(result.stdout.strip())
    except (ValueError, UnicodeDecodeError):
        return None
    with tempfile.NamedTemporaryFile(suffix=".pem", mode="wb", delete=False) as f:
        f.write(cert_pem)
        f.flush()
        openssl_result = subprocess.run(
            ["openssl", "x509", "-enddate", "-noout", "-in", f.name],
            capture_output=True,
            text=True,
            check=False,
        )
        os.unlink(f.name)
    if openssl_result.returncode != 0:
        return None
    line = openssl_result.stdout.strip()
    if "=" in line:
        return line.split("=", 1)[1]
    return None


def cert_days_remaining(expiry_str: str) -> int:
    """Parse an openssl date string and return days until expiry."""
    try:
        expiry = datetime.strptime(expiry_str, "%b %d %H:%M:%S %Y %Z")  # noqa: DTZ007
        expiry = expiry.replace(tzinfo=timezone.utc)
        now = datetime.now(timezone.utc)
        return (expiry - now).days
    except ValueError:
        return -1


def find_certmgr_function(cfg: Config) -> str | None:
    """Find the certmgr OCI Function OCID."""
    result = oci_cmd(
        cfg,
        [
            "fn",
            "application",
            "list",
            "--compartment-id",
            cfg.oci_compartment_id,
            "--output",
            "json",
        ],
        check=False,
    )
    if result.returncode != 0:
        return None
    apps = json.loads(result.stdout).get("data", [])
    for app in apps:
        if (
            "certmgr" in app.get("display-name", "").lower()
            or "cert" in app.get("display-name", "").lower()
        ):
            fn_result = oci_cmd(
                cfg,
                [
                    "fn",
                    "function",
                    "list",
                    "--application-id",
                    app["id"],
                    "--output",
                    "json",
                ],
                check=False,
            )
            if fn_result.returncode != 0:
                continue
            functions = json.loads(fn_result.stdout).get("data", [])
            for fn in functions:
                if "certmgr" in fn.get("display-name", "").lower():
                    return fn["id"]
    return None


def get_vault_secret_content(
    cfg: Config, secret_name_prefix: str, suffix: str
) -> str | None:
    """Retrieve a secret's content from OCI Vault by name pattern."""
    result = oci_cmd(
        cfg,
        [
            "vault",
            "secret",
            "list",
            "--compartment-id",
            cfg.oci_compartment_id,
            "--output",
            "json",
        ],
        check=False,
    )
    if result.returncode != 0:
        return None
    secrets = json.loads(result.stdout).get("data", [])
    target_name = f"{secret_name_prefix}-{suffix}"
    for secret in secrets:
        if (
            secret.get("secret-name", "") == target_name
            and secret.get("lifecycle-state") == "ACTIVE"
        ):
            bundle_result = oci_cmd(
                cfg,
                [
                    "secrets",
                    "secret-bundle",
                    "get",
                    "--secret-id",
                    secret["id"],
                    "--output",
                    "json",
                ],
            )
            bundle = json.loads(bundle_result.stdout)
            content = bundle.get("data", {}).get("secret-bundle-content", {})
            if content.get("content-type") == "BASE64":
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

    expiry = get_tls_secret_expiry(cfg)
    if expiry:
        days = cert_days_remaining(expiry)
        if days > 7:
            click.echo(
                f"  [OK] tmi-tls secret exists, expires: {expiry} ({days} days remaining)"
            )
            click.echo("  Certificate is valid. Skipping renewal.")
            return
        else:
            click.echo(f"  tmi-tls secret expires in {days} days. Renewing...")
    else:
        click.echo("  tmi-tls secret not found. Issuing new certificate...")

    if dry_run:
        click.echo("  [DRY RUN] Would invoke certmgr and create K8s TLS secret.")
        return

    fn_id = find_certmgr_function(cfg)
    if not fn_id:
        click.echo("ERROR: certmgr function not found.", err=True)
        click.echo(
            "  Enable 'enable_certificate_automation = true' in terraform.tfvars",
            err=True,
        )
        click.echo("  and re-run 'make deploy-oci'.", err=True)
        sys.exit(1)
    assert fn_id is not None  # narrowing for type checkers (checked above)
    click.echo(f"  Found certmgr function: {fn_id}")

    click.echo(f"  Invoking certmgr for *.{cfg.domain}...")
    result = oci_cmd(
        cfg,
        [
            "fn",
            "function",
            "invoke",
            "--function-id",
            fn_id,
            "--file",
            "-",
            "--body",
            "",
        ],
    )
    try:
        response = json.loads(result.stdout)
        if response.get("status") == "error":
            click.echo(
                f"ERROR: certmgr failed: {response.get('error', 'unknown')}", err=True
            )
            if "rate" in response.get("error", "").lower():
                click.echo(
                    "  ACME rate limit hit. Try ACME_DIRECTORY=staging first.", err=True
                )
            sys.exit(1)
        click.echo(f"  certmgr response: {response.get('message', 'OK')}")
    except json.JSONDecodeError:
        click.echo(f"  certmgr output: {result.stdout[:200]}")

    click.echo("  Retrieving certificate from OCI Vault...")
    cert_pem = get_vault_secret_content(cfg, "tmi", "certificate")
    key_pem = get_vault_secret_content(cfg, "tmi", "private-key")
    if not cert_pem or not key_pem:
        click.echo("ERROR: Could not retrieve certificate or key from Vault.", err=True)
        click.echo("  Check certmgr function logs and Vault IAM policies.", err=True)
        sys.exit(1)
    assert cert_pem is not None and key_pem is not None  # narrowing (checked above)
    click.echo("  [OK] Retrieved certificate and private key from Vault")

    click.echo("  Creating K8s TLS secret...")
    with (
        tempfile.NamedTemporaryFile(mode="w", suffix=".pem", delete=False) as cert_f,
        tempfile.NamedTemporaryFile(mode="w", suffix=".pem", delete=False) as key_f,
    ):
        cert_f.write(cert_pem)
        cert_f.flush()
        key_f.write(key_pem)
        key_f.flush()
        create_result = kubectl_cmd(
            cfg,
            [
                "create",
                "secret",
                "tls",
                "tmi-tls",
                f"--cert={cert_f.name}",
                f"--key={key_f.name}",
                "--dry-run=client",
                "-o",
                "yaml",
            ],
        )
        os.unlink(cert_f.name)
        os.unlink(key_f.name)
    apply_cmd = ["kubectl"]
    if cfg.kube_context:
        apply_cmd += ["--context", cfg.kube_context]
    apply_cmd += ["--namespace", cfg.tmi_namespace, "apply", "-f", "-"]
    apply_result = subprocess.run(
        apply_cmd,
        input=create_result.stdout,
        capture_output=True,
        text=True,
        check=False,
    )
    if apply_result.returncode != 0:
        click.echo(
            f"ERROR: Failed to apply TLS secret: {apply_result.stderr}", err=True
        )
        sys.exit(1)
    click.echo("  [OK] tmi-tls secret created/updated")

    click.echo(f"  Verifying HTTPS at https://{cfg.api_hostname}/...")
    time.sleep(5)
    try:
        resp = requests.get(f"https://{cfg.api_hostname}/", timeout=10, verify=True)
        click.echo(f"  [OK] HTTPS working: {resp.status_code}")
    except requests.exceptions.SSLError as e:
        click.echo(f"  [WARN] SSL error (cert may still be propagating): {e}", err=True)
    except requests.exceptions.ConnectionError:
        click.echo(
            "  [WARN] Connection failed. DNS may not have propagated yet.", err=True
        )

    click.echo("\nCertificate setup complete.")


# ---------------------------------------------------------------------------
# Phase 3: configure
# ---------------------------------------------------------------------------


def get_admin_setting(token: str, api_url: str, key: str) -> dict | None:
    """Get a system setting by key."""
    resp = tmi_api(token, "GET", f"{api_url}/admin/settings/{key}")
    if resp.status_code == 200:
        return resp.json()
    return None


def set_admin_setting(
    token: str, api_url: str, key: str, value: str, value_type: str, description: str
) -> None:
    """Create or update a system setting."""
    resp = tmi_api(
        token,
        "PUT",
        f"{api_url}/admin/settings/{key}",
        json={
            "value": value,
            "type": value_type,
            "description": description,
        },
    )
    if resp.status_code not in (200, 201):
        click.echo(
            f"ERROR: Failed to set {key}: {resp.status_code} {resp.text}", err=True
        )
        sys.exit(1)


@cli.command()
@click.pass_context
def configure(ctx):
    """Phase 3: Configure CORS, HTTP webhooks, and Google OAuth."""
    cfg = ctx.obj["config"]
    dry_run = ctx.obj["dry_run"]

    click.echo("=== Phase 3: Configuration ===\n")

    api_url = get_api_url(cfg)
    click.echo(f"  API URL: {api_url}")

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
        time.sleep(3)
        token = get_tmi_token(cfg, api_url)
    elif not changed:
        click.echo("  [OK] ConfigMap already has correct values")

    # --- Google OAuth via admin settings API ---
    click.echo("\n--- Google OAuth Configuration ---")
    oauth_settings = {
        "auth.oauth.providers.google.enabled": (
            "true",
            "bool",
            "Enable Google OAuth provider",
        ),
        "auth.oauth.providers.google.client_id": (
            cfg.google_client_id,
            "string",
            "Google OAuth client ID",
        ),
        "auth.oauth.providers.google.client_secret": (
            cfg.google_client_secret,
            "string",
            "Google OAuth client secret",
        ),
        "auth.oauth.providers.google.scopes": (
            '["openid", "email", "profile"]',
            "json",
            "Google OAuth scopes",
        ),
    }

    for key, (value, vtype, desc) in oauth_settings.items():
        existing = get_admin_setting(token, api_url, key)
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

    if not dry_run:
        click.echo("\n  Verifying Google OAuth provider...")
        resp = tmi_api(token, "GET", f"{api_url}/oauth2/providers")
        if resp.status_code == 200:
            providers = resp.json()
            provider_ids = [
                p.get("id") or p.get("provider_id")
                for p in providers.get("providers", providers.get("items", []))
            ]
            if "google" in provider_ids:
                click.echo("  [OK] Google OAuth provider is active")
            else:
                click.echo(
                    "  [WARN] Google OAuth not appearing in provider list yet.",
                    err=True,
                )
                click.echo(
                    "  It may take up to 60 seconds for the provider cache to refresh.",
                    err=True,
                )
        else:
            click.echo(
                f"  [WARN] Could not verify providers: {resp.status_code}", err=True
            )

    click.echo("\nConfiguration complete.")


# ---------------------------------------------------------------------------
# All phases
# ---------------------------------------------------------------------------


@cli.command(name="all")
@click.pass_context
def run_all(ctx):
    """Run all phases in order: verify -> dns -> certs -> configure."""
    phases = [
        ("verify", verify),
        ("dns", dns),
        ("certs", certs),
        ("configure", configure),
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
    cfg = ctx.obj["config"]
    if not ctx.obj["dry_run"]:
        click.echo(f"\n  API:  https://{cfg.api_hostname}")
        click.echo(f"  App:  https://{cfg.ux_hostname}")
        click.echo("\n  Google OAuth configured and available.")


if __name__ == "__main__":
    cli()
