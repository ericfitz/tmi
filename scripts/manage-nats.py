# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Manage the TMI NATS JetStream container.

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
    ensure_container,
    log_info,
    log_success,
    log_warn,
    remove_container,
    stop_container,
    wait_for_container_ready,
)

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEV_DEFAULTS = {
    "container": "tmi-nats",
    "port": 4222,
    "image": "nats:2.10-alpine",
}

TEST_DEFAULTS = {
    "container": "tmi-nats-test",
    "port": 4222,
    "image": "nats:2.10-alpine",
}

NATS_CONTAINER_PORT = 4222

# JetStream flag — required for TMI's durable-consumer / object-store usage.
NATS_CMD_ARGS = ["-js"]


# ---------------------------------------------------------------------------
# Config resolution
# ---------------------------------------------------------------------------


def resolve_config(args: argparse.Namespace) -> dict:
    """Build the effective configuration by layering defaults, config file, and CLI flags.

    Priority (highest wins): CLI flags > mode defaults.
    """
    mode_defaults = TEST_DEFAULTS if args.test else DEV_DEFAULTS
    cfg = dict(mode_defaults)

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
    """Start the NATS container (create if needed)."""
    ensure_container(
        name=cfg["container"],
        host_port=cfg["port"],
        container_port=NATS_CONTAINER_PORT,
        image=cfg["image"],
        cmd_args=NATS_CMD_ARGS,
    )
    log_success(f"NATS container is running on port {cfg['port']}")


def cmd_wait(cfg: dict, args: argparse.Namespace) -> None:
    """Wait until NATS is accepting connections (TCP probe on the client port)."""
    from tmi_common import wait_for_port  # noqa: E402 (local import for clarity)

    wait_for_port(cfg["port"], timeout=30, label="NATS")


def cmd_stop(cfg: dict, args: argparse.Namespace) -> None:
    """Stop the NATS container."""
    log_info(f"Stopping NATS container: {cfg['container']}")
    stop_container(cfg["container"])
    log_success("NATS container stopped")


def cmd_clean(cfg: dict, args: argparse.Namespace) -> None:
    """Remove the NATS container."""
    log_warn(f"Removing NATS container: {cfg['container']}")
    remove_container(cfg["container"])
    log_success("NATS container removed")


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

SUBCOMMANDS = {
    "start": (cmd_start, "Start NATS container (create if needed)"),
    "wait": (cmd_wait, "Wait until NATS port is accepting connections"),
    "stop": (cmd_stop, "Stop NATS container"),
    "clean": (cmd_clean, "Remove NATS container"),
}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="manage-nats.py",
        description="Manage TMI NATS JetStream container.",
    )

    # Global flags
    add_config_arg(parser)
    parser.add_argument(
        "--test",
        action="store_true",
        default=False,
        help="Use test container (isolated from dev, container name: tmi-nats-test)",
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
        help="Override host port (default: 4222)",
    )
    parser.add_argument(
        "--image",
        metavar="IMAGE",
        default=None,
        help="Override Docker image (default: nats:2.10-alpine)",
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
