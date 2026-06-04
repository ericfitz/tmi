# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Orchestrate the full TMI development environment.

Starts the database, Redis, NATS, waits for the database to be ready, then
starts the server and the async-extraction workers.  With --restart, stops
the server and workers and rebuilds first.

The async-extraction pipeline requires NATS JetStream.  The NATS URL is set
in the server process environment so the monolith connects on boot:
  TMI_NATS_URL=nats://localhost:4222
"""

import argparse
import os
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

# NATS URL used by the monolith and workers in the dev environment.
DEV_NATS_URL = "nats://localhost:4222"


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
    parser.add_argument(
        "--no-workers",
        action="store_true",
        default=False,
        help="Skip starting NATS and the async-extraction workers",
    )
    return parser.parse_args()


def uv_script(script: str) -> list[str]:
    """Return a uv run command list for the given script name."""
    project_root = get_project_root()
    return ["uv", "run", str(project_root / "scripts" / script)]


def start_normal(config: str, no_workers: bool = False) -> None:
    """Run the normal start sequence.

    Order:
      1. Database container start
      2. Redis container start
      3. NATS container start  (skipped with --no-workers)
      4. Database wait (blocks until Postgres accepts connections)
      5. Server start  (TMI_NATS_URL set in env so monolith connects to NATS)
      6. Worker processes start  (skipped with --no-workers)
    """
    run_cmd(uv_script("manage-database.py") + ["--config", config, "start"])
    run_cmd(uv_script("manage-redis.py") + ["--config", config, "start"])

    if not no_workers:
        run_cmd(uv_script("manage-nats.py") + ["--config", config, "start"])

    run_cmd(uv_script("manage-database.py") + ["--config", config, "wait"])

    # Export TMI_NATS_URL into the current process environment so that the
    # server subprocess (launched by manage-server.py via subprocess.Popen
    # without an explicit env=) inherits it and wireExtractionNATS() connects.
    if not no_workers:
        os.environ.setdefault("TMI_NATS_URL", DEV_NATS_URL)
        log_info(f"TMI_NATS_URL={os.environ['TMI_NATS_URL']} (will be inherited by server)")

    run_cmd(uv_script("manage-server.py") + ["--config", config, "start"])

    if not no_workers:
        run_cmd(
            uv_script("manage-workers.py") + ["--nats-url", DEV_NATS_URL, "start"],
            # Workers inheriting TMI_NATS_URL from the environment is sufficient;
            # the explicit --nats-url flag makes the value visible in process listings.
        )


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    config = args.config

    if args.restart:
        log_info("Restarting development environment")
        run_cmd(uv_script("manage-server.py") + ["--config", config, "stop"])
        run_cmd(uv_script("manage-workers.py") + ["stop"], check=False)
        project_root = get_project_root()
        run_cmd(
            ["go", "build", "-tags=dev", "-o", "bin/tmiserver", "./cmd/server/"],
            cwd=str(project_root),
        )
    else:
        log_info("Starting development environment")

    start_normal(config, no_workers=args.no_workers)
    log_success("Development environment started on port 8080")


if __name__ == "__main__":
    main()
