"""Shared utilities for TMI container build scripts.

This module is not run directly — it is imported by build-app-containers.py
and build-db-containers.py via sys.path.
"""

import json
import os
import platform
import subprocess
import sys
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

# --- Colors ---
RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[1;33m"
BLUE = "\033[0;34m"
NC = "\033[0m"


def log_info(msg: str) -> None:
    print(f"{BLUE}[INFO]{NC} {msg}", flush=True)


def log_success(msg: str) -> None:
    print(f"{GREEN}[SUCCESS]{NC} {msg}", flush=True)


def log_warn(msg: str) -> None:
    print(f"{YELLOW}[WARN]{NC} {msg}", flush=True)


def log_error(msg: str) -> None:
    print(f"{RED}[ERROR]{NC} {msg}", file=sys.stderr, flush=True)


# --- Subprocess ---


def run(
    cmd: list[str],
    *,
    check: bool = True,
    capture: bool = False,
    cwd: str | Path | None = None,
) -> subprocess.CompletedProcess:
    """Run a command with logging."""
    log_info(f"Running: {' '.join(cmd)}")
    return subprocess.run(
        cmd,
        check=check,
        capture_output=capture,
        text=True,
        cwd=cwd,
    )


# --- Version ---


def get_project_root() -> Path:
    """Return the project root (parent of scripts/)."""
    return Path(__file__).resolve().parent.parent


def read_version(project_root: Path) -> dict:
    """Read version from .version JSON file.

    Returns dict with keys: major, minor, patch, prerelease.
    """
    version_file = project_root / ".version"
    try:
        data = json.loads(version_file.read_text())
        for key in ("major", "minor", "patch", "prerelease"):
            if key not in data:
                log_error(
                    f".version file missing '{key}' key. "
                    "Expected JSON with major, minor, patch, prerelease keys."
                )
                sys.exit(1)
        return data
    except FileNotFoundError:
        log_error(
            f"Cannot read {version_file}. "
            "Ensure .version file exists with valid JSON."
        )
        sys.exit(1)
    except json.JSONDecodeError as e:
        log_error(f".version file contains invalid JSON: {e}")
        sys.exit(1)


def format_version(v: dict) -> str:
    """Format version dict as string (e.g., '1.3.0' or '1.3.0-rc.0')."""
    base = f"{v['major']}.{v['minor']}.{v['patch']}"
    if v.get("prerelease"):
        return f"{base}-{v['prerelease']}"
    return base


def get_git_commit() -> str:
    """Get short git commit hash."""
    try:
        result = run(
            ["git", "rev-parse", "--short", "HEAD"], capture=True, check=False
        )
        return result.stdout.strip() if result.returncode == 0 else "development"
    except FileNotFoundError:
        return "development"


def get_build_date() -> str:
    """Get current UTC timestamp in ISO8601 format."""
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


# --- Build Args ---


def get_build_args(version: dict, git_commit: str, build_date: str) -> list[str]:
    """Assemble --build-arg flags for docker build."""
    args = [
        "--build-arg", f"BUILD_DATE={build_date}",
        "--build-arg", f"GIT_COMMIT={git_commit}",
        "--build-arg", f"VERSION_MAJOR={version['major']}",
        "--build-arg", f"VERSION_MINOR={version['minor']}",
        "--build-arg", f"VERSION_PATCH={version['patch']}",
        "--build-arg", f"VERSION_PRERELEASE={version.get('prerelease', '')}",
    ]
    return args


# --- Target Config ---


@dataclass
class TargetConfig:
    registry: str
    platform: str
    use_buildx: bool
    auth_commands: list[list[str]]
    dockerfile_map: dict[str, str]
    image_name_prefix: str
    labels: dict[str, str] = field(default_factory=dict)
    # Override image names for specific components (e.g., OCI maps "server" to "tmi/tmi")
    image_name_map: dict[str, str] = field(default_factory=dict)


def _local_arch() -> str:
    """Detect local platform architecture."""
    machine = platform.machine().lower()
    if machine in ("arm64", "aarch64"):
        return "linux/arm64"
    return "linux/amd64"


def _resolve_arch(arch: str | None, target: str) -> str:
    """Resolve architecture to docker platform string."""
    if arch == "both":
        return "linux/amd64,linux/arm64"
    if arch == "arm64":
        return "linux/arm64"
    if arch == "amd64":
        return "linux/amd64"
    # Auto-detect based on target
    if target == "local":
        return _local_arch()
    if target == "oci":
        return "linux/arm64"
    return "linux/amd64"


def _get_dockerfile_map(target: str, db_backend: str) -> dict[str, str]:
    """Determine Dockerfile for each component based on target and db backend."""
    use_oracle = target == "oci" or db_backend == "oracle-adb"
    return {
        "server": "Dockerfile.server-oracle" if use_oracle else "Dockerfile.server",
        "redis": "Dockerfile.redis-oracle" if target == "oci" else "Dockerfile.redis",
        "promtail": "Dockerfile.promtail",
        "postgres": "Dockerfile.postgres",
    }


def _discover_oci_namespace(profile: str) -> str:
    """Get OCI tenancy namespace via OCI CLI."""
    ns_result = run(
        ["oci", "os", "ns", "get", "--query", "data", "--raw-output", "--profile", profile],
        capture=True,
        check=False,
    )
    if ns_result.returncode != 0 or not ns_result.stdout.strip():
        log_error("Failed to get OCI tenancy namespace. Check OCI CLI configuration.")
        sys.exit(1)
    return ns_result.stdout.strip()


def get_target_config(
    target: str,
    arch: str | None,
    db_backend: str,
    registry_override: str | None,
) -> TargetConfig:
    """Get build configuration for a deployment target."""
    platform_str = _resolve_arch(arch, target)
    dockerfile_map = _get_dockerfile_map(target, db_backend)

    match target:
        case "local":
            return TargetConfig(
                registry="tmi",
                platform=platform_str,
                use_buildx=False,
                auth_commands=[],
                dockerfile_map=dockerfile_map,
                image_name_prefix="tmi/tmi-",
            )

        case "oci":
            profile = os.environ.get("OCI_CLI_PROFILE", "tmi")
            region = os.environ.get("OCI_REGION", "us-ashburn-1")
            name_prefix = os.environ.get("TMI_NAME_PREFIX", "tmi")

            if registry_override:
                base = registry_override
            else:
                namespace = _discover_oci_namespace(profile)
                base = f"{region}.ocir.io/{namespace}"

            return TargetConfig(
                registry=f"{base}/{name_prefix}",
                platform=platform_str,
                use_buildx=True,
                auth_commands=[
                    ["__oci_docker_login__", region, profile],
                ],
                dockerfile_map=dockerfile_map,
                image_name_prefix=f"{base}/{name_prefix}/tmi-",
                image_name_map={
                    "server": f"{base}/{name_prefix}/tmi",
                },
            )

        case "aws":
            if registry_override:
                ecr_registry = registry_override
            else:
                # Auto-discover from AWS CLI
                acct = run(
                    ["aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text"],
                    capture=True,
                ).stdout.strip()
                region = run(
                    ["aws", "configure", "get", "region"],
                    capture=True,
                    check=False,
                ).stdout.strip() or "us-east-1"
                ecr_registry = f"{acct}.dkr.ecr.{region}.amazonaws.com"

            # Extract region from registry URL for auth
            ecr_region = ecr_registry.split(".dkr.ecr.")[1].split(".amazonaws.com")[0] if ".dkr.ecr." in ecr_registry else "us-east-1"
            return TargetConfig(
                registry=ecr_registry,
                platform=platform_str,
                use_buildx=True,
                # AWS auth handled specially in authenticate_registry()
                auth_commands=[
                    ["__aws_ecr_login__", ecr_region, ecr_registry],
                ],
                dockerfile_map=dockerfile_map,
                image_name_prefix=f"{ecr_registry}/tmi-",
            )

        case "azure":
            if not registry_override:
                log_error("Azure requires --registry (e.g., --registry myacr.azurecr.io/tmi)")
                sys.exit(1)
            assert registry_override is not None  # guaranteed by check above
            acr_name = registry_override.split(".")[0]
            return TargetConfig(
                registry=registry_override,
                platform=platform_str,
                use_buildx=True,
                auth_commands=[
                    ["az", "acr", "login", "--name", acr_name],
                ],
                dockerfile_map=dockerfile_map,
                image_name_prefix=f"{registry_override}/tmi-",
            )

        case "gcp":
            if not registry_override:
                log_error("GCP requires --registry (e.g., --registry us-docker.pkg.dev/project/repo)")
                sys.exit(1)
            assert registry_override is not None  # guaranteed by check above
            gcp_host = registry_override.split("/")[0]
            return TargetConfig(
                registry=registry_override,
                platform=platform_str,
                use_buildx=True,
                auth_commands=[
                    ["gcloud", "auth", "configure-docker", gcp_host],
                ],
                dockerfile_map=dockerfile_map,
                image_name_prefix=f"{registry_override}/tmi-",
            )

        case "heroku":
            app_name = registry_override or os.environ.get("HEROKU_APP", "")
            if not app_name:
                log_error(
                    "Heroku requires app name via --registry or HEROKU_APP env var"
                )
                sys.exit(1)
            return TargetConfig(
                registry=f"registry.heroku.com/{app_name}",
                platform="linux/amd64",
                use_buildx=False,
                auth_commands=[
                    ["heroku", "container:login"],
                ],
                dockerfile_map=dockerfile_map,
                image_name_prefix=f"registry.heroku.com/{app_name}/",
            )

        case _:
            log_error(f"Unknown target: {target}")
            sys.exit(1)


# --- Prerequisites ---


def check_prerequisites(*, need_buildx: bool = False, need_scan: bool = False) -> None:
    """Verify required tools are available."""
    missing = []

    # Check docker CLI
    if _which("docker") is None:
        missing.append("docker (install Docker Desktop or Docker Engine)")
    else:
        # Check docker daemon is running
        result = run(["docker", "info"], capture=True, check=False)
        if result.returncode != 0:
            missing.append(
                "Docker daemon is not running. Start Docker Desktop or the Docker service."
            )

    # Check buildx if needed
    if need_buildx:
        result = run(["docker", "buildx", "version"], capture=True, check=False)
        if result.returncode != 0:
            missing.append("docker buildx (update Docker or install buildx plugin)")

    # Check scan tools if needed
    if need_scan:
        if not _which("grype"):
            missing.append("grype (install: brew install grype)")
        if not _which("syft"):
            missing.append("syft (install: brew install syft)")

    if missing:
        log_error("Missing prerequisites:")
        for m in missing:
            log_error(f"  - {m}")
        sys.exit(1)

    log_success("All prerequisites met")


def _which(cmd: str) -> str | None:
    """Check if a command is available."""
    import shutil
    return shutil.which(cmd)


# --- Docker Build ---

BUILDX_BUILDER_NAME = "tmi-multiarch"


def ensure_buildx_builder() -> None:
    """Ensure a buildx builder exists for multi-platform builds."""
    result = run(
        ["docker", "buildx", "inspect", BUILDX_BUILDER_NAME],
        capture=True,
        check=False,
    )
    if result.returncode != 0:
        log_info(f"Creating buildx builder: {BUILDX_BUILDER_NAME}")
        run(["docker", "buildx", "create", "--name", BUILDX_BUILDER_NAME, "--use", "--bootstrap"])
    else:
        run(["docker", "buildx", "use", BUILDX_BUILDER_NAME])
    log_success(f"Using buildx builder: {BUILDX_BUILDER_NAME}")


def authenticate_registry(config: TargetConfig) -> None:
    """Run registry authentication commands."""
    for cmd in config.auth_commands:
        try:
            if cmd[0] == "__aws_ecr_login__":
                _aws_ecr_login(cmd[1], cmd[2])
            elif cmd[0] == "__oci_docker_login__":
                _oci_docker_login(cmd[1], cmd[2])
            else:
                run(cmd)
        except subprocess.CalledProcessError:
            log_error(f"Registry authentication failed: {' '.join(cmd)}")
            sys.exit(1)


def _aws_ecr_login(region: str, registry: str) -> None:
    """Authenticate with AWS ECR using a safe two-step pipeline."""
    log_info(f"Authenticating with AWS ECR ({registry})...")
    password_result = run(
        ["aws", "ecr", "get-login-password", "--region", region],
        capture=True,
    )
    subprocess.run(
        ["docker", "login", "--username", "AWS", "--password-stdin", registry],
        input=password_result.stdout,
        text=True,
        check=True,
    )
    log_success("Authenticated with AWS ECR")


def _oci_docker_login(region: str, profile: str) -> None:
    """Authenticate with OCI Container Registry (OCIR).

    Checks (in order):
    1. OCIR_AUTH_TOKEN env var — uses it directly with OCI username
    2. Existing Docker credentials — skips login if already authenticated
    3. Otherwise — prints instructions and exits
    """
    registry = f"{region}.ocir.io"
    namespace = _discover_oci_namespace(profile)

    # Check if already logged in by inspecting Docker config
    docker_config_path = Path.home() / ".docker" / "config.json"
    if docker_config_path.exists():
        try:
            docker_config = json.loads(docker_config_path.read_text())
            auths = docker_config.get("auths", {})
            if registry in auths or f"https://{registry}" in auths:
                log_info(f"Already authenticated with {registry}")
                return
            # Also check credsStore/credHelpers
            cred_helpers = docker_config.get("credHelpers", {})
            creds_store = docker_config.get("credsStore", "")
            if registry in cred_helpers or creds_store:
                log_info(f"Credential helper configured for {registry}")
                return
        except (json.JSONDecodeError, OSError):
            pass

    # Try OCIR_AUTH_TOKEN env var
    auth_token = os.environ.get("OCIR_AUTH_TOKEN", "")
    if auth_token:
        # Get username from OCI CLI
        user_result = run(
            ["oci", "iam", "user", "list", "--profile", profile,
             "--query", "data[0].name", "--raw-output"],
            capture=True,
            check=False,
        )
        if user_result.returncode != 0 or not user_result.stdout.strip():
            log_error("Failed to get OCI username. Check OCI CLI configuration.")
            sys.exit(1)
        username = f"{namespace}/{user_result.stdout.strip()}"

        log_info(f"Authenticating with OCIR ({registry}) as {username}...")
        login_result = subprocess.run(
            ["docker", "login", registry, "-u", username, "--password-stdin"],
            input=auth_token,
            text=True,
            check=False,
            capture_output=True,
        )
        if login_result.returncode != 0:
            log_error(f"OCIR login failed: {login_result.stderr.strip()}")
            sys.exit(1)
        log_success(f"Authenticated with OCIR ({registry})")
        return

    # Not logged in and no token — print instructions
    log_error("OCIR authentication required.")
    log_info("Option 1: Set OCIR_AUTH_TOKEN env var:")
    log_info("  export OCIR_AUTH_TOKEN='<your-auth-token>'")
    log_info("Option 2: Log in manually:")
    log_info(f"  docker login {registry} -u {namespace}/<your-oci-username>")
    log_info("Use an OCI Auth Token as the password.")
    log_info("Create one at: OCI Console > User Settings > Auth Tokens")
    sys.exit(1)


def get_image_tags(
    config: TargetConfig,
    component: str,
    version: dict,
    git_commit: str,
) -> list[str]:
    """Generate image tags for a component."""
    image_name = config.image_name_map.get(
        component, f"{config.image_name_prefix}{component}"
    )
    version_str = format_version(version)
    return [
        f"{image_name}:latest",
        f"{image_name}:v{version_str}",
        f"{image_name}:{git_commit}",
    ]


def run_docker_build(
    config: TargetConfig,
    dockerfile: str,
    context: Path,
    tags: list[str],
    build_args: list[str],
    *,
    push: bool = False,
    no_cache: bool = False,
    extra_build_args: list[str] | None = None,
) -> None:
    """Run docker build or docker buildx build."""
    is_multiarch = "," in config.platform

    if is_multiarch and not push:
        log_error(
            "Multi-arch builds require --push "
            "(cannot load multi-platform images into local Docker daemon). "
            "Use --push with a registry, or build for a single architecture."
        )
        sys.exit(1)

    if config.use_buildx:
        ensure_buildx_builder()
        cmd = ["docker", "buildx", "build"]
        cmd.extend(["--platform", config.platform])
        if push:
            cmd.append("--push")
        else:
            cmd.append("--load")
    else:
        cmd = ["docker", "build"]

    cmd.extend(["-f", str(context / dockerfile)])

    for tag in tags:
        cmd.extend(["-t", tag])

    cmd.extend(build_args)

    if extra_build_args:
        cmd.extend(extra_build_args)

    if no_cache:
        cmd.append("--no-cache")

    cmd.append(str(context))

    try:
        run(cmd)
    except subprocess.CalledProcessError:
        log_error(f"Docker build failed for {dockerfile}")
        sys.exit(1)

    if push and not config.use_buildx:
        # Plain docker build — need explicit push
        for tag in tags:
            try:
                run(["docker", "push", tag])
            except subprocess.CalledProcessError:
                log_error(f"Failed to push {tag}")
                sys.exit(1)

    log_success(f"Built: {tags[0]}")


# --- Security Scanning ---


def scan_image(image_name: str, reports_dir: Path) -> bool:
    """Scan an image with Grype and generate SBOM with Syft.

    Returns True if within CVE thresholds.
    """
    max_critical = 0
    max_high = 5

    reports_dir.mkdir(parents=True, exist_ok=True)
    sbom_dir = reports_dir / "sbom"
    sbom_dir.mkdir(parents=True, exist_ok=True)

    # Derive report base name from image
    report_name = image_name.split("/")[-1].replace(":", "-")

    log_info(f"Scanning {image_name} for vulnerabilities...")

    # Grype SARIF report
    run(
        ["grype", image_name, "-o", "sarif"],
        check=False,
        capture=False,  # Let it print to stdout for redirection by caller
    )
    sarif_result = run(
        ["grype", image_name, "-o", "sarif"],
        capture=True,
        check=False,
    )
    sarif_file = reports_dir / f"{report_name}-scan.sarif"
    sarif_file.write_text(sarif_result.stdout)

    # Grype table report
    table_result = run(
        ["grype", image_name, "-o", "table"],
        capture=True,
        check=False,
    )
    table_file = reports_dir / f"{report_name}-scan.txt"
    table_file.write_text(table_result.stdout)
    # Print table to console
    print(table_result.stdout)

    # Count CVEs from JSON output
    json_result = run(
        ["grype", image_name, "-o", "json"],
        capture=True,
        check=False,
    )
    critical_count = 0
    high_count = 0
    if json_result.returncode == 0:
        try:
            data = json.loads(json_result.stdout)
            for match in data.get("matches", []):
                severity = match.get("vulnerability", {}).get("severity", "")
                if severity == "Critical":
                    critical_count += 1
                elif severity == "High":
                    high_count += 1
        except json.JSONDecodeError:
            log_warn("Failed to parse Grype JSON output")

    log_info(f"Found {critical_count} critical and {high_count} high severity vulnerabilities")

    # Syft SBOM
    if _which("syft"):
        log_info(f"Generating SBOM for {image_name}...")
        sbom_json = sbom_dir / f"{report_name}-sbom.json"
        run(
            ["syft", image_name, "-o", f"cyclonedx-json={sbom_json}"],
            check=False,
        )
        sbom_xml = sbom_dir / f"{report_name}-sbom.xml"
        run(
            ["syft", image_name, "-o", f"cyclonedx-xml={sbom_xml}"],
            check=False,
        )
        log_success(f"SBOM generated: {sbom_json.name}")

    # Check thresholds
    passed = True
    if critical_count > max_critical:
        log_error(
            f"{image_name} has {critical_count} critical CVEs "
            f"(max allowed: {max_critical})"
        )
        passed = False

    if high_count > max_high:
        log_warn(
            f"{image_name} has {high_count} high CVEs "
            f"(max recommended: {max_high})"
        )

    return passed


def generate_security_summary(reports_dir: Path, build_date: str, git_commit: str) -> None:
    """Generate a markdown security summary report."""
    summary_file = reports_dir / "security-summary.md"
    lines = [
        "# TMI Container Security Scan Summary",
        "",
        f"**Scan Date:** {build_date}",
        f"**Git Commit:** {git_commit}",
        "**Scanner:** Grype (Anchore)",
        "",
        "## Vulnerability Summary",
        "",
        "| Image | Critical | High | Status |",
        "|-------|----------|------|--------|",
    ]

    # Find all scan text files
    for scan_file in sorted(reports_dir.glob("*-scan.txt")):
        name = scan_file.stem.replace("-scan", "")
        content = scan_file.read_text()
        critical = content.count("Critical")
        high = content.count("High")
        if critical > 0:
            status = "FAIL"
        elif high > 5:
            status = "WARNING"
        else:
            status = "PASS"
        lines.append(f"| {name} | {critical} | {high} | {status} |")

    lines.extend([
        "",
        "## Detailed Reports",
        "",
        "See individual SARIF and text files in this directory.",
        "",
        "## SBOMs",
        "",
        "See `sbom/` subdirectory for CycloneDX JSON and XML files.",
    ])

    summary_file.write_text("\n".join(lines) + "\n")
    log_success(f"Security summary: {summary_file}")
