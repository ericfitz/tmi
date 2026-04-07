# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Build and run the CATS database seeding tool.

Builds the cats-seed binary (with or without Oracle support) and runs it
against the configured server to seed test data for CATS API fuzzing.
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build and run the CATS database seeding tool."
    )
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument(
        "--oci",
        action="store_true",
        default=False,
        help="Build with Oracle support (requires scripts/oci-env.sh)",
    )
    parser.add_argument(
        "--user",
        metavar="USER",
        default="charlie",
        help="CATS user (default: charlie)",
    )
    parser.add_argument(
        "--provider",
        metavar="PROVIDER",
        default="tmi",
        help="Auth provider (default: tmi)",
    )
    parser.add_argument(
        "--server",
        metavar="URL",
        default="http://localhost:8080",
        help="Server URL (default: http://localhost:8080)",
    )
    return parser.parse_args()


def build_cats_seed(oci: bool, project_root: Path) -> None:
    """Build the cats-seed binary."""
    if oci:
        oci_env_path = project_root / "scripts" / "oci-env.sh"
        if not oci_env_path.exists():
            log_error(f"OCI env script not found: {oci_env_path}")
            sys.exit(1)
        log_info("Building CATS seeding tool with Oracle support...")
        run_cmd(
            [
                "/bin/bash",
                "-c",
                f". {oci_env_path} && go build -tags oracle -o bin/cats-seed github.com/ericfitz/tmi/cmd/cats-seed",
            ],
            cwd=str(project_root),
        )
        log_success("CATS seeding tool built with Oracle support: bin/cats-seed")
    else:
        log_info("Building CATS seeding tool...")
        run_cmd(
            ["go", "build", "-o", "bin/cats-seed", "github.com/ericfitz/tmi/cmd/cats-seed"],
            cwd=str(project_root),
        )
        log_success("CATS seeding tool built: bin/cats-seed")


def run_cats_seed(config: str, user: str, provider: str, server: str, project_root: Path) -> None:
    """Run the cats-seed binary."""
    log_info(f"Seeding CATS test data (user={user}, provider={provider}, server={server})...")
    run_cmd(
        [
            "./bin/cats-seed",
            f"--config={config}",
            f"--user={user}",
            f"--provider={provider}",
            f"--server={server}",
        ],
        cwd=str(project_root),
    )
    log_success("CATS database seeding and API object creation completed")


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()

    # Determine config: if --oci and user hasn't overridden --config, use OCI config
    config = args.config
    if args.oci:
        default_config = str(project_root / "config-development.yml")
        if config == default_config:
            config = str(project_root / "config-development-oci.yml")

    build_cats_seed(args.oci, project_root)
    run_cats_seed(config, args.user, args.provider, args.server, project_root)


if __name__ == "__main__":
    main()
