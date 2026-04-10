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
from urllib.parse import urlparse

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    config_get,
    docker_exec,
    ensure_container,
    ensure_volume,
    get_project_root,
    load_config,
    log_info,
    log_success,
    log_warn,
    remove_container,
    run_cmd,
    stop_container,
    wait_for_container_ready,
)

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEV_DEFAULTS = {
    "container": "tmi-postgresql",
    "port": 5432,
    "user": "tmi_dev",
    "password": "dev123",
    "database": "tmi_dev",
    "image": "tmi/tmi-postgresql:latest",
    "volume": "tmi-postgres-data",
}

TEST_DEFAULTS = {
    "container": "tmi-postgresql",
    "port": 5432,
    "user": "tmi_dev",
    "password": "dev123",
    "database": "tmi_dev",
    "image": "tmi/tmi-postgresql:latest",
    "volume": "tmi-postgres-data",
}

POSTGRES_CONTAINER_PORT = 5432


# ---------------------------------------------------------------------------
# Config resolution
# ---------------------------------------------------------------------------


def _parse_db_url(url: str) -> dict:
    """Extract connection details from a postgres:// URL.

    Returns a dict with keys: user, password, port, database.
    Missing components return None.
    """
    try:
        parsed = urlparse(url)
        result = {}
        if parsed.username:
            result["user"] = parsed.username
        if parsed.password:
            result["password"] = parsed.password
        if parsed.port:
            result["port"] = parsed.port
        if parsed.path and parsed.path.lstrip("/"):
            result["database"] = parsed.path.lstrip("/").split("?")[0]
        return result
    except Exception:
        return {}


def resolve_config(args: argparse.Namespace) -> dict:
    """Build the effective configuration by layering defaults, config file, and CLI flags.

    Priority (highest wins): CLI flags > config file > mode defaults.
    """
    mode_defaults = TEST_DEFAULTS if args.test else DEV_DEFAULTS
    cfg = dict(mode_defaults)

    # Load config file and extract DB URL fields
    config_path = Path(args.config)
    raw = load_config(config_path)
    db_url = config_get(raw, "database.url")
    if db_url:
        from_url = _parse_db_url(db_url)
        cfg.update(from_url)

    # CLI overrides
    if args.container:
        cfg["container"] = args.container
    if args.port is not None:
        cfg["port"] = args.port
    if args.user_name:
        cfg["user"] = args.user_name
    if args.database:
        cfg["database"] = args.database
    if args.image:
        cfg["image"] = args.image

    # In test mode, always use test container and no volume unless overridden
    if args.test and not args.container:
        cfg["container"] = TEST_DEFAULTS["container"]
    if args.test:
        cfg["volume"] = None

    return cfg


# ---------------------------------------------------------------------------
# Subcommand implementations
# ---------------------------------------------------------------------------


def cmd_start(cfg: dict, args: argparse.Namespace) -> None:
    """Start the PostgreSQL container (create if needed)."""
    volume = cfg.get("volume")

    if volume:
        ensure_volume(volume)

    volumes = {}
    if volume:
        volumes[volume] = "/var/lib/postgresql/data"

    ensure_container(
        name=cfg["container"],
        host_port=cfg["port"],
        container_port=POSTGRES_CONTAINER_PORT,
        image=cfg["image"],
        env_vars={
            "POSTGRES_USER": cfg["user"],
            "POSTGRES_PASSWORD": cfg["password"],
            "POSTGRES_DB": cfg["database"],
        },
        volumes=volumes if volumes else None,
    )
    log_success(f"PostgreSQL container is running on port {cfg['port']}")


def cmd_stop(cfg: dict, args: argparse.Namespace) -> None:
    """Stop the PostgreSQL container."""
    log_info(f"Stopping PostgreSQL container: {cfg['container']}")
    stop_container(cfg["container"])
    log_success("PostgreSQL container stopped")


def cmd_clean(cfg: dict, args: argparse.Namespace) -> None:
    """Remove the PostgreSQL container and volume (if dev)."""
    volume = cfg.get("volume")
    log_warn(f"Removing PostgreSQL container: {cfg['container']}" +
             (f" and volume: {volume}" if volume else ""))
    remove_container(cfg["container"], volumes=[volume] if volume else None)
    log_success("PostgreSQL container" + (" and data" if volume else "") + " removed")


def cmd_wait(cfg: dict, args: argparse.Namespace) -> None:
    """Wait until the PostgreSQL container is ready to accept connections."""
    timeout = args.timeout
    health_cmd = ["docker", "exec", cfg["container"], "pg_isready", "-U", cfg["user"]]
    wait_for_container_ready(
        health_cmd=health_cmd,
        timeout=timeout,
        label=f"PostgreSQL ({cfg['container']})",
    )


def cmd_migrate(cfg: dict, args: argparse.Namespace) -> None:
    """Run database migrations."""
    log_info("Running database migrations...")
    project_root = get_project_root()
    config_path = Path(args.config).resolve()
    migrate_dir = project_root / "cmd" / "migrate"
    run_cmd(
        ["go", "run", "main.go", "--config", str(config_path)],
        cwd=migrate_dir,
        verbose=getattr(args, "verbose", False),
    )
    log_success("Database migrations completed")


def cmd_reset(cfg: dict, args: argparse.Namespace) -> None:
    """Drop and recreate the database, then run migrations."""
    log_warn(f"Dropping and recreating database '{cfg['database']}' (DESTRUCTIVE)...")
    log_warn("This will delete all data in the database.")

    docker_exec(
        cfg["container"],
        ["psql", "-U", cfg["user"], "-d", "postgres", "-c",
         f"DROP DATABASE IF EXISTS {cfg['database']};"],
    )
    docker_exec(
        cfg["container"],
        ["psql", "-U", cfg["user"], "-d", "postgres", "-c",
         f"CREATE DATABASE {cfg['database']};"],
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
        help="Use development container (port 5432)",
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

    cfg = resolve_config(args)

    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(cfg, args)


if __name__ == "__main__":
    main()
