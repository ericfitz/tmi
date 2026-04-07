# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Manage TMI Redis containers.

Supports both dev (persistent) and test (ephemeral) containers.
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    config_get,
    ensure_container,
    load_config,
    log_info,
    log_success,
    log_warn,
    remove_container,
    stop_container,
)

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEV_DEFAULTS = {
    "container": "tmi-redis",
    "port": 6379,
    "image": "tmi/tmi-redis:latest",
}

TEST_DEFAULTS = {
    "container": "tmi-redis-test",
    "port": 6380,
    "image": "tmi/tmi-redis:latest",
}

REDIS_CONTAINER_PORT = 6379


# ---------------------------------------------------------------------------
# Config resolution
# ---------------------------------------------------------------------------


def resolve_config(args: argparse.Namespace) -> dict:
    """Build the effective configuration by layering defaults, config file, and CLI flags.

    Priority (highest wins): CLI flags > config file > mode defaults.
    """
    mode_defaults = TEST_DEFAULTS if args.test else DEV_DEFAULTS
    cfg = dict(mode_defaults)

    # Load config file and extract Redis port
    config_path = Path(args.config)
    raw = load_config(config_path)
    redis_port = config_get(raw, "database.redis.port")
    if redis_port is not None:
        cfg["port"] = redis_port

    # CLI overrides
    if args.container:
        cfg["container"] = args.container
    if args.port is not None:
        cfg["port"] = args.port
    if args.image:
        cfg["image"] = args.image

    # In test mode, always use test container name unless explicitly overridden
    if args.test and not args.container:
        cfg["container"] = TEST_DEFAULTS["container"]

    return cfg


# ---------------------------------------------------------------------------
# Subcommand implementations
# ---------------------------------------------------------------------------


def cmd_start(cfg: dict, args: argparse.Namespace) -> None:
    """Start the Redis container (create if needed)."""
    ensure_container(
        name=cfg["container"],
        host_port=cfg["port"],
        container_port=REDIS_CONTAINER_PORT,
        image=cfg["image"],
    )
    log_success(f"Redis container is running on port {cfg['port']}")


def cmd_stop(cfg: dict, args: argparse.Namespace) -> None:
    """Stop the Redis container."""
    log_info(f"Stopping Redis container: {cfg['container']}")
    stop_container(cfg["container"])
    log_success("Redis container stopped")


def cmd_clean(cfg: dict, args: argparse.Namespace) -> None:
    """Remove the Redis container."""
    log_warn(f"Removing Redis container: {cfg['container']}")
    remove_container(cfg["container"])
    log_success("Redis container removed")


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

SUBCOMMANDS = {
    "start": (cmd_start, "Start Redis container (create if needed)"),
    "stop": (cmd_stop, "Stop Redis container"),
    "clean": (cmd_clean, "Remove Redis container"),
}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="manage-redis.py",
        description="Manage TMI Redis containers.",
    )

    # Global flags
    add_config_arg(parser)
    parser.add_argument(
        "--test",
        action="store_true",
        default=False,
        help="Use ephemeral test container (port 6380, name tmi-redis-test)",
    )
    parser.add_argument(
        "--container",
        metavar="NAME",
        default=None,
        help="Override container name",
    )
    parser.add_argument(
        "--port",
        type=int,
        default=None,
        metavar="PORT",
        help="Override host port",
    )
    parser.add_argument(
        "--image",
        metavar="IMAGE",
        default=None,
        help="Override Docker image",
    )
    add_verbosity_args(parser)

    # Subcommands
    subparsers = parser.add_subparsers(dest="subcommand", metavar="SUBCOMMAND")
    subparsers.required = True
    for name, (_, help_text) in SUBCOMMANDS.items():
        subparsers.add_parser(name, help=help_text)

    return parser


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    apply_verbosity(args)

    cfg = resolve_config(args)

    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(cfg, args)


if __name__ == "__main__":
    main()
