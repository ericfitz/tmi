"""PostgreSQL dev/test container lifecycle. Single source of truth shared by
scripts/devenv.py (dev) and scripts/manage-database.py (dev + --test)."""
from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from tmi_common import (
    container_is_running,
    ensure_container,
    ensure_volume,
    get_project_root,
    log_info,
    log_success,
    log_warn,
    remove_container,
    run_cmd,
    stop_container,
    wait_for_container_ready,
)

# Container port is always 5432 inside the container image
_POSTGRES_CONTAINER_PORT = 5432

# Docker image shared by dev and test
_POSTGRES_IMAGE = "tmi/tmi-postgresql:latest"

# Default DB credentials (same for dev and test)
_DEFAULT_USER = "tmi_dev"
_DEFAULT_PASSWORD = "dev123"
_DEFAULT_DATABASE = "tmi_dev"


@dataclass(frozen=True)
class DBProfile:
    """Immutable profile describing a PostgreSQL container instance.

    Attributes:
        container:   Docker container name.
        volume:      Named Docker volume for persistent data, or "" for ephemeral (test).
        port:        Host port the container is bound to (127.0.0.1).
        config_path: Path to the YAML config file used by migrations.
        user:        PostgreSQL user.
        password:    PostgreSQL password.
        database:    PostgreSQL database name.
        image:       Docker image reference.
    """

    container: str
    volume: str          # "" means ephemeral — no volume mount
    port: int
    config_path: str
    user: str = _DEFAULT_USER
    password: str = _DEFAULT_PASSWORD
    database: str = _DEFAULT_DATABASE
    image: str = _POSTGRES_IMAGE


def dev_profile(config_path: str = "config-development.yml") -> DBProfile:
    """Return the profile for the persistent dev PostgreSQL container.

    Container name matches scripts/lib/devstatus.py expectations
    (name=tmi-postgresql).  Volume name copied verbatim from manage-database.py
    DEV_DEFAULTS["volume"] = "tmi-postgres-data".
    """
    return DBProfile(
        container="tmi-postgresql",
        volume="tmi-postgres-data",
        port=5432,
        config_path=config_path,
    )


def test_profile(config_path: str = "config-test.yml") -> DBProfile:
    """Return the ephemeral test PostgreSQL profile.

    Faithful to the original manage-database.py TEST behavior: shares the same
    container name as dev (``tmi-postgresql``) and uses NO volume (ephemeral —
    container data is discarded when the container stops).  This mirrors the
    original ``resolve_config`` logic where ``volume = None`` for the test mode.
    """
    return DBProfile(
        container="tmi-postgresql",
        volume="",       # ephemeral — no persistent volume
        port=5432,
        config_path=config_path,
    )


def is_running(profile: DBProfile) -> bool:
    """Return True if the profile's container is currently running."""
    return container_is_running(profile.container)


def up(profile: DBProfile) -> None:
    """Start the PostgreSQL container, creating it and its volume if needed.

    Copied from manage-database.py cmd_start body.
    """
    if profile.volume:
        ensure_volume(profile.volume)

    volumes = {}
    if profile.volume:
        volumes[profile.volume] = "/var/lib/postgresql/data"

    ensure_container(
        name=profile.container,
        host_port=profile.port,
        container_port=_POSTGRES_CONTAINER_PORT,
        image=profile.image,
        env_vars={
            "POSTGRES_USER": profile.user,
            "POSTGRES_PASSWORD": profile.password,
            "POSTGRES_DB": profile.database,
        },
        volumes=volumes if volumes else None,
    )
    log_success(f"PostgreSQL container is running on port {profile.port}")


def down(profile: DBProfile) -> None:
    """Stop the PostgreSQL container (preserves volume data).

    Copied from manage-database.py cmd_stop body.
    """
    log_info(f"Stopping PostgreSQL container: {profile.container}")
    stop_container(profile.container)
    log_success("PostgreSQL container stopped")


def destroy(profile: DBProfile) -> None:
    """Remove the PostgreSQL container and its volume (data wiped).

    Copied from manage-database.py cmd_clean body.
    """
    volume = profile.volume or None
    log_warn(
        f"Removing PostgreSQL container: {profile.container}"
        + (f" and volume: {volume}" if volume else "")
    )
    remove_container(profile.container, volumes=[volume] if volume else None)
    log_success(
        "PostgreSQL container" + (" and data" if volume else "") + " removed"
    )


def wait(profile: DBProfile, timeout: int = 300) -> None:
    """Wait until the PostgreSQL container is ready to accept connections.

    Copied from manage-database.py cmd_wait body.

    Args:
        profile: The database profile describing the container.
        timeout: Maximum seconds to wait (default: 300).
    """
    health_cmd = [
        "docker", "exec", profile.container,
        "pg_isready", "-U", profile.user,
    ]
    wait_for_container_ready(
        health_cmd=health_cmd,
        timeout=timeout,
        label=f"PostgreSQL ({profile.container})",
    )


def migrate(profile: DBProfile, *, verbose: bool = False) -> None:
    """Run database migrations against the container described by profile.

    Copied from manage-database.py cmd_migrate body.

    Args:
        profile: The database profile; profile.config_path is resolved and
                 passed to the migrate binary.
        verbose: If True, log the migration command before running.
    """
    log_info("Running database migrations...")
    project_root = get_project_root()
    config_path = Path(profile.config_path).resolve()
    migrate_dir = project_root / "cmd" / "migrate"
    run_cmd(
        ["go", "run", "main.go", "--config", str(config_path)],
        cwd=migrate_dir,
        verbose=verbose,
    )
    log_success("Database migrations completed")
