# /// script
# requires-python = ">=3.11"
# ///
"""Build TMI Go binaries (server, migrate, cats-seed).

Usage:
    uv run scripts/build-server.py [flags]

Flags:
    --component NAME  Component to build: server (default), migrate, cats-seed
    --tags TAGS       Additional build tags (space-separated)
    --oci             Build with Oracle support (cats-seed only)
    -v/--verbose, -q/--quiet
"""

import argparse
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    format_version,
    get_project_root,
    log_error,
    log_info,
    log_success,
    read_version,
    run_cmd,
)

# ---------------------------------------------------------------------------
# Component definitions
# ---------------------------------------------------------------------------

COMPONENTS = {
    "server": {
        "output": "bin/tmiserver",
        "package": "github.com/ericfitz/tmi/cmd/server",
        "tags": ["dev"],
        "ldflags": True,
    },
    "migrate": {
        "output": "bin/migrate",
        "package": "github.com/ericfitz/tmi/cmd/migrate",
        "tags": [],
        "ldflags": False,
    },
    "cats-seed": {
        "output": "bin/cats-seed",
        "package": "github.com/ericfitz/tmi/cmd/cats-seed",
        "tags": [],
        "ldflags": False,
    },
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def get_git_commit(project_root: Path) -> str:
    """Return the short git commit hash, or 'unknown' on failure."""
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--short", "HEAD"],
            capture_output=True,
            text=True,
            cwd=project_root,
            check=True,
        )
        return result.stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return "unknown"


def build_ldflags(version: dict, commit: str, build_date: str) -> str:
    """Construct the -ldflags string for the server binary."""
    prefix = "github.com/ericfitz/tmi/api"
    flags = [
        f"-X {prefix}.VersionMajor={version['major']}",
        f"-X {prefix}.VersionMinor={version['minor']}",
        f"-X {prefix}.VersionPatch={version['patch']}",
        f"-X {prefix}.VersionPreRelease={version.get('prerelease', '')}",
        f"-X {prefix}.GitCommit={commit}",
        f"-X {prefix}.BuildDate={build_date}",
    ]
    return " ".join(flags)


def source_oci_env(project_root: Path) -> dict:
    """Source scripts/oci-env.sh and return the resulting environment variables.

    Returns a dict of variables exported by the script that differ from the
    current environment, suitable for passing to subprocess env.
    """
    oci_env = project_root / "scripts" / "oci-env.sh"
    if not oci_env.exists():
        log_error(
            "scripts/oci-env.sh not found. "
            "Copy from scripts/oci-env.sh.example and configure."
        )
        sys.exit(1)

    # Source the script in a subshell and print the environment
    result = subprocess.run(
        ["/bin/bash", "-c", f'. "{oci_env}" && env'],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        log_error(f"Failed to source scripts/oci-env.sh: {result.stderr.strip()}")
        sys.exit(1)

    env_vars: dict[str, str] = {}
    for line in result.stdout.splitlines():
        if "=" in line:
            key, _, value = line.partition("=")
            env_vars[key] = value
    return env_vars


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Build TMI Go binaries.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--component",
        default="server",
        choices=list(COMPONENTS.keys()),
        help="Component to build (default: server)",
    )
    parser.add_argument(
        "--tags",
        default="",
        help="Additional build tags (space-separated)",
    )
    parser.add_argument(
        "--oci",
        action="store_true",
        default=False,
        help="Build with Oracle support (cats-seed only)",
    )
    add_verbosity_args(parser)
    args = parser.parse_args()
    apply_verbosity(args)

    component = args.component
    cfg = COMPONENTS[component]
    project_root = get_project_root()

    if args.oci and component != "cats-seed":
        log_error("--oci flag is only supported for the cats-seed component")
        sys.exit(1)

    log_info(f"Building {component} binary...")

    # Collect build tags
    raw_tags = cfg["tags"]
    assert isinstance(raw_tags, list)
    tags: list[str] = [str(t) for t in raw_tags]
    if args.tags:
        tags.extend(args.tags.split())
    if args.oci:
        tags.append("oracle")

    # Resolve OCI environment if needed
    extra_env: dict[str, str] | None = None
    if args.oci:
        log_info("Sourcing OCI environment from scripts/oci-env.sh...")
        extra_env = source_oci_env(project_root)

    # Build the go build command
    cmd = ["go", "build"]

    if tags:
        cmd.extend(["-tags", ",".join(tags)])

    if cfg["ldflags"]:
        version = read_version()
        commit = get_git_commit(project_root)
        build_date = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        ldflags = build_ldflags(version, commit, build_date)
        log_info(f"Version: {format_version(version)}, commit: {commit}, date: {build_date}")
        cmd.extend(["-ldflags", ldflags])

    output = cfg["output"]
    cmd.extend(["-o", output, cfg["package"]])

    run_cmd(cmd, cwd=project_root, env=extra_env, verbose=getattr(args, "verbose", False))

    log_success(f"{component.capitalize()} binary built: {output}")


if __name__ == "__main__":
    main()
