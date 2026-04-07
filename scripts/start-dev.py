# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Orchestrate the full TMI development environment.

Starts the database, Redis, waits for the database to be ready, then starts
the server.  With --restart, stops the server and rebuilds first.
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
    log_info,
    log_success,
    run_cmd,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Orchestrate the TMI development environment."
    )
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument(
        "--restart",
        action="store_true",
        help="Stop server, rebuild, clean logs, then start",
    )
    return parser.parse_args()


def uv_script(script: str) -> list[str]:
    """Return a uv run command list for the given script name."""
    project_root = get_project_root()
    return ["uv", "run", str(project_root / "scripts" / script)]


def start_normal(config: str) -> None:
    """Run the normal start sequence (DB + Redis + wait + server)."""
    run_cmd(uv_script("manage-database.py") + ["--config", config, "start"])
    run_cmd(uv_script("manage-redis.py") + ["--config", config, "start"])
    run_cmd(uv_script("manage-database.py") + ["--config", config, "wait"])
    run_cmd(uv_script("manage-server.py") + ["--config", config, "start"])


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    config = args.config

    if args.restart:
        log_info("Restarting development environment")
        run_cmd(uv_script("manage-server.py") + ["--config", config, "stop"])
        project_root = get_project_root()
        run_cmd(
            ["go", "build", "-tags=dev", "-o", "bin/tmiserver", "./cmd/server/"],
            cwd=str(project_root),
        )
    else:
        log_info("Starting development environment")

    start_normal(config)
    log_success("Development environment started on port 8080")


if __name__ == "__main__":
    main()
