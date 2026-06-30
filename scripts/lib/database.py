"""PostgreSQL dev/test container lifecycle. Single source of truth shared by
scripts/devenv.py (dev) and scripts/manage-database.py (dev + --test)."""
from __future__ import annotations

import sys
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlparse

from tmi_common import (
    config_get,
    container_is_running,
    ensure_container,
    ensure_volume,
    get_project_root,
    load_config,
    log_error,
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

# Container and volume names
DEV_CONTAINER = "tmi-postgresql"
DEV_VOLUME = "tmi-postgres-data"

# Isolated test container — distinct name and a distinct host port so the
# ephemeral integration-test database can never collide with or replace a
# developer's running dev container/data (#477).
TEST_CONTAINER = "tmi-postgresql-test"

# Host port for the isolated test container. Owned here in the tracked runner —
# not in config-test.yml, which is a gitignored, dev-local file — so the
# integration harness binds the test DB to a port distinct from dev's 5432
# reproducibly, regardless of what a given developer's config-test.yml says.
TEST_PORT = 5433


@dataclass(frozen=True)
class DBProfile:
    """Immutable profile describing a PostgreSQL container instance.

    Attributes:
        container:   Docker container name.
        volume:      Named Docker volume for persistent data, or "" for ephemeral (test).
        port:        Host port the container is bound to (127.0.0.1).
        config_path: Path to the YAML config file used by migrations.
        user:        PostgreSQL user (derived from config file database.url).
        password:    PostgreSQL password (derived from config file database.url).
        database:    PostgreSQL database name (derived from config file database.url).
        image:       Docker image reference.
    """

    container: str
    volume: str          # "" means ephemeral — no volume mount
    port: int
    config_path: str
    user: str
    password: str
    database: str
    image: str = _POSTGRES_IMAGE


def _parse_db_url(url: str) -> dict:
    """Extract connection details from a postgres:// URL.

    Returns a dict with keys: user, password, port, database.
    Missing components are omitted from the returned dict.
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


def _connection_from_config(config_path: str | Path) -> dict:
    """Load PostgreSQL connection settings from a config file's database.url field.

    Reads the YAML config at config_path, extracts database.url, and parses it
    into a dict with keys: user, password, port, database.

    Exits with a clear error message if the url is missing or any required
    field cannot be extracted. No hardcoded fallback values are used.

    Args:
        config_path: Path to the YAML config file.

    Returns:
        Dict with keys: user, password, port, database.
    """
    raw = load_config(config_path)
    db_url = config_get(raw, "database.url")
    if not db_url:
        log_error(
            f"Config file '{config_path}' does not contain 'database.url'. "
            "Cannot determine PostgreSQL connection settings."
        )
        sys.exit(1)

    parsed = _parse_db_url(db_url)
    missing = [k for k in ("user", "password", "port", "database") if k not in parsed]
    if missing:
        log_error(
            f"Could not extract {missing} from database.url in '{config_path}'. "
            f"URL was: {db_url!r}"
        )
        sys.exit(1)

    return parsed


def profile_from_config(
    config_path: str | Path,
    *,
    ephemeral: bool,
    container: str = DEV_CONTAINER,
    volume: str | None = None,
    overrides: dict | None = None,
) -> "DBProfile":
    """Build a DBProfile from a config file and optional overrides.

    Connection settings (user, password, port, database) are derived from
    config_path's database.url field. No hardcoded credential defaults are used.

    Args:
        config_path: Path to the YAML config file.
        ephemeral:   If True, the container uses no persistent volume.
        container:   Docker container name (default: DEV_CONTAINER).
        volume:      Named Docker volume for persistent data; ignored when
                     ephemeral=True (defaults to DEV_VOLUME when None and
                     ephemeral=False).
        overrides:   Optional dict of field overrides applied last
                     (keys: container, port, user, password, database, image).

    Returns:
        Immutable DBProfile.
    """
    conn = _connection_from_config(config_path)

    effective_volume = "" if ephemeral else (volume if volume is not None else DEV_VOLUME)

    kwargs: dict = {
        "container": container,
        "volume": effective_volume,
        "port": conn["port"],
        "config_path": str(config_path),
        "user": conn["user"],
        "password": conn["password"],
        "database": conn["database"],
    }

    if overrides:
        for key, val in overrides.items():
            if val is not None:
                kwargs[key] = val

    return DBProfile(**kwargs)


def dev_profile(config_path: str = "config-development.yml") -> DBProfile:
    """Return the profile for the persistent dev PostgreSQL container.

    Connection settings are derived from config_path's database.url field.
    Container name matches scripts/lib/devstatus.py expectations
    (name=tmi-postgresql).  Volume name is tmi-postgres-data.
    """
    return profile_from_config(
        config_path,
        ephemeral=False,
        container=DEV_CONTAINER,
        volume=DEV_VOLUME,
    )


def test_profile(config_path: str = "config-test.yml") -> DBProfile:
    """Return the ephemeral, isolated test PostgreSQL profile.

    Connection credentials (user/password/database) are derived from
    config_path's database.url field (config-test.yml: the ``tmi_test``
    database). The host port is forced to the runner-owned ``TEST_PORT`` so the
    isolated container always binds a port distinct from dev's 5432, regardless
    of the gitignored config file's url. Uses a dedicated container name
    (``tmi-postgresql-test``) distinct from the dev container and NO volume
    (ephemeral — data is discarded on stop), so the integration-test database
    can never collide with or replace a developer's running dev container (#477).
    """
    return profile_from_config(
        config_path,
        ephemeral=True,
        container=TEST_CONTAINER,
        overrides={"port": TEST_PORT},
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
    # Migrate against the profile's actual connection, not just config_path's
    # url: the test profile forces a runner-owned port (TEST_PORT) that may
    # differ from the gitignored config file's url. TMI_DATABASE_URL overrides
    # the file's database.url for this run (#477).
    db_url = (
        f"postgres://{profile.user}:{profile.password}@localhost:{profile.port}"
        f"/{profile.database}?sslmode=disable"
    )
    run_cmd(
        ["go", "run", "main.go", "--config", str(config_path)],
        cwd=migrate_dir,
        env={"TMI_DATABASE_URL": db_url},
        verbose=verbose,
    )
    log_success("Database migrations completed")
