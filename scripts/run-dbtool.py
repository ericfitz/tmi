# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Build and run the TMI database administration tool (tmi-dbtool).

Builds the tmi-dbtool binary (with or without Oracle support) and runs it
in import-test-data mode to seed test data for CATS API fuzzing.
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
        description="Build and run the TMI database administration tool."
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
    parser.add_argument(
        "--input-file",
        metavar="FILE",
        default="test/seeds/cats-seed-data.json",
        help="Seed data file (default: test/seeds/cats-seed-data.json)",
    )
    return parser.parse_args()


def build_dbtool(oci: bool, project_root: Path) -> None:
    """Build the tmi-dbtool binary."""
    if oci:
        oci_env_path = project_root / "scripts" / "oci-env.sh"
        if not oci_env_path.exists():
            log_error(f"OCI env script not found: {oci_env_path}")
            sys.exit(1)
        log_info("Building dbtool with Oracle support...")
        run_cmd(
            [
                "/bin/bash",
                "-c",
                f". {oci_env_path} && go build -tags oracle -o bin/tmi-dbtool github.com/ericfitz/tmi/cmd/dbtool",
            ],
            cwd=str(project_root),
        )
        log_success("Database tool built with Oracle support: bin/tmi-dbtool")
    else:
        log_info("Building dbtool...")
        run_cmd(
            ["go", "build", "-o", "bin/tmi-dbtool", "github.com/ericfitz/tmi/cmd/dbtool"],
            cwd=str(project_root),
        )
        log_success("Database tool built: bin/tmi-dbtool")


def run_dbtool(config: str, user: str, provider: str, server: str, input_file: str, project_root: Path) -> None:
    """Run the tmi-dbtool binary in import-test-data mode."""
    log_info(f"Seeding test data (user={user}, provider={provider}, server={server})...")
    run_cmd(
        [
            "./bin/tmi-dbtool",
            "-t",
            f"--config={config}",
            f"--input-file={input_file}",
            f"--user={user}",
            f"--provider={provider}",
            f"--server={server}",
        ],
        cwd=str(project_root),
    )
    log_success("Seeding completed")


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()

    config = args.config
    if args.oci:
        default_config = str(project_root / "config-development.yml")
        if config == default_config:
            config = str(project_root / "config-development-oci.yml")

    build_dbtool(args.oci, project_root)
    run_dbtool(config, args.user, args.provider, args.server, args.input_file, project_root)


if __name__ == "__main__":
    main()
