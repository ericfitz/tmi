# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Manage TMI PostgreSQL database containers and migrations.

Supports both dev (persistent volume) and test (ephemeral) containers.
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import database  # noqa: E402
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    docker_exec,
    get_project_root,
    log_info,
    log_success,
    log_warn,
    run_cmd,
)

# ---------------------------------------------------------------------------
# Profile helper
# ---------------------------------------------------------------------------


def _profile(args: argparse.Namespace) -> database.DBProfile:
    """Build a DBProfile from parsed CLI args.

    Derives connection settings from the config file's database.url field.
    CLI flags (--container, --port, --user-name, --database, --image) are
    applied as overrides on top of the config-derived values.
    """
    test_mode = args.test
    # In test mode, default to config-test.yml (isolated tmi_test DB on the
    # dedicated test host port) so --test never derives its connection from the
    # dev config (#477).
    cfg_path = args.config or (
        "config-test.yml" if test_mode else "config-development.yml"
    )
    # In test mode, always use the isolated test container unless the caller
    # explicitly overrides --container, so the test database can never collide
    # with or replace the dev container (#477).
    container_override = getattr(args, "container", None)
    if test_mode and container_override is None:
        container_override = database.TEST_CONTAINER
    overrides = {
        "container": container_override,
        "port": getattr(args, "port", None),
        "user": getattr(args, "user_name", None),
        "database": getattr(args, "database", None),
        "image": getattr(args, "image", None),
    }
    return database.profile_from_config(
        cfg_path,
        ephemeral=test_mode,
        overrides=overrides,
    )


# ---------------------------------------------------------------------------
# Subcommand implementations
# ---------------------------------------------------------------------------


def cmd_start(cfg: dict, args: argparse.Namespace) -> None:
    """Start the PostgreSQL container (create if needed)."""
    database.up(_profile(args))


def cmd_stop(cfg: dict, args: argparse.Namespace) -> None:
    """Stop the PostgreSQL container."""
    database.down(_profile(args))


def cmd_clean(cfg: dict, args: argparse.Namespace) -> None:
    """Remove the PostgreSQL container and volume (if dev)."""
    database.destroy(_profile(args))


def cmd_wait(cfg: dict, args: argparse.Namespace) -> None:
    """Wait until the PostgreSQL container is ready to accept connections."""
    database.wait(_profile(args), timeout=args.timeout)


def cmd_migrate(cfg: dict, args: argparse.Namespace) -> None:
    """Run database migrations."""
    database.migrate(_profile(args), verbose=getattr(args, "verbose", False))


def cmd_reset(cfg: dict, args: argparse.Namespace) -> None:
    """Drop and recreate the database, then run migrations."""
    profile = _profile(args)
    log_warn(f"Dropping and recreating database '{profile.database}' (DESTRUCTIVE)...")
    log_warn("This will delete all data in the database.")

    docker_exec(
        profile.container,
        ["psql", "-U", profile.user, "-d", "postgres", "-c",
         f"DROP DATABASE IF EXISTS {profile.database};"],
    )
    docker_exec(
        profile.container,
        ["psql", "-U", profile.user, "-d", "postgres", "-c",
         f"CREATE DATABASE {profile.database};"],
    )
    log_success("Database dropped and recreated")
    cmd_migrate(cfg, args)


def cmd_dedup(cfg: dict, args: argparse.Namespace) -> None:
    """Remove duplicate group_members rows (one-off maintenance task)."""
    log_info("Deduplicating group_members table...")
    project_root = get_project_root()
    config_path = Path(args.config).resolve()
    run_cmd(
        ["go", "run", "./cmd/dedup-group-members", f"--config={config_path}"],
        cwd=project_root,
        verbose=getattr(args, "verbose", False),
    )
    log_success("Group members deduplication completed")


def cmd_check(cfg: dict, args: argparse.Namespace) -> None:
    """Validate database schema via the migrate tool."""
    project_root = get_project_root()
    config_path = Path(args.config)
    log_info("Checking database schema...")
    run_cmd(
        ["go", "run", "main.go", "--config", str(config_path.resolve()), "--validate"],
        cwd=project_root / "cmd" / "migrate",
    )
    log_success("Database schema is valid")


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

SUBCOMMANDS = {
    "start": (cmd_start, "Start PostgreSQL container (create if needed)"),
    "stop": (cmd_stop, "Stop PostgreSQL container"),
    "clean": (cmd_clean, "Remove PostgreSQL container and volume (dev only)"),
    "wait": (cmd_wait, "Wait until PostgreSQL is accepting connections"),
    "migrate": (cmd_migrate, "Run database migrations"),
    "reset": (cmd_reset, "Drop and recreate database, then migrate (DESTRUCTIVE)"),
    "dedup": (cmd_dedup, "Remove duplicate group_members rows (one-off maintenance)"),
    "check": (cmd_check, "Validate database schema"),
}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="manage-database.py",
        description="Manage TMI PostgreSQL database containers and migrations.",
    )

    # Global flags
    add_config_arg(parser)
    parser.add_argument(
        "--test",
        action="store_true",
        default=False,
        help="Use the isolated, ephemeral test container "
        "(tmi-postgresql-test, config-test.yml); never touches the dev container",
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
        "--user-name",
        metavar="NAME",
        default=None,
        help="Override database user name",
    )
    parser.add_argument(
        "--database",
        metavar="NAME",
        default=None,
        help="Override database name",
    )
    parser.add_argument(
        "--image",
        metavar="IMAGE",
        default=None,
        help="Override Docker image",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=300,
        metavar="SECS",
        help="Wait timeout in seconds (default: 300)",
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

    fn, _ = SUBCOMMANDS[args.subcommand]
    fn({}, args)


if __name__ == "__main__":
    main()
