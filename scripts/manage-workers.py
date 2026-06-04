# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Manage TMI worker processes (tmi-extractor and tmi-chunk-embed).

Starts and stops the two async-extraction worker binaries as local background
processes, mirroring the pattern used by manage-server.py.

Workers require NATS JetStream to be running before they start.  Use
manage-nats.py to bring up the NATS container first.

Environment variables passed to workers:
  TMI_NATS_URL          - NATS server URL (default: nats://localhost:4222)
  TMI_COMPONENT_NAME    - Set per-worker; overridden automatically

Embedding env vars (for tmi-chunk-embed in dev):
  TMI_EMBEDDING_MODEL   - Embedding model name (e.g. text-embedding-3-small)
  TMI_EMBEDDING_BASE_URL- OpenAI-compatible API base URL
  TMI_EMBEDDING_API_KEY - API key

If these are not already exported, the chunk-embed worker is started with
values read from the dev database (the post-#415 source of truth):
  timmy.text_embedding_model / timmy.text_embedding_base_url /
  timmy.text_embedding_api_key in the system_settings table.
An explicitly-exported env var always wins over the DB value.
"""

import argparse
import os
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    graceful_kill,
    log_error,
    log_info,
    log_success,
    log_warn,
    read_pid_file,
    run_cmd,
    write_pid_file,
)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

DEFAULT_NATS_URL = "nats://localhost:4222"

# Local dev PostgreSQL container (matches manage-database.py / start-dev).
DEV_PG_CONTAINER = "tmi-postgresql"
DEV_PG_USER = "tmi_dev"
DEV_PG_DB = "tmi_dev"

# Map of worker env var -> system_settings key (post-#415 source of truth).
# Used to populate the chunk-embed worker's embedding config from the DB when
# the operator has not exported the env vars explicitly.
EMBED_ENV_TO_SETTING = {
    "TMI_EMBEDDING_MODEL": "timmy.text_embedding_model",
    "TMI_EMBEDDING_BASE_URL": "timmy.text_embedding_base_url",
    "TMI_EMBEDDING_API_KEY": "timmy.text_embedding_api_key",
}

WORKERS = [
    {
        "name": "tmi-extractor",
        "binary": "bin/tmi-extractor",
        "build_cmd": ["go", "build", "-o", "bin/tmi-extractor", "./cmd/extractor/"],
        "pid_file": ".extractor.pid",
        "log_file": "logs/extractor.log",
        "component_name": "tmi-extractor",
        # Embedding env vars are NOT required for the extractor
        "require_embed_env": False,
    },
    {
        "name": "tmi-chunk-embed",
        "binary": "bin/tmi-chunk-embed",
        "build_cmd": ["go", "build", "-o", "bin/tmi-chunk-embed", "./cmd/chunkembed/"],
        "pid_file": ".chunkembed.pid",
        "log_file": "logs/chunkembed.log",
        "component_name": "tmi-chunk-embed",
        # Embedding env vars ARE required; warn if absent but do not abort —
        # the worker will exit on its own if they are missing, and the operator
        # can inspect the log file.
        "require_embed_env": True,
    },
]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _read_setting_from_db(key: str) -> str | None:
    """Read a single system_settings value from the dev PostgreSQL container.

    Returns the trimmed value, or None if the container is unreachable, the row
    is absent, or the value is empty. Never raises — a DB read failure here must
    not block worker startup (the worker will simply warn/exit on its own).
    """
    try:
        result = subprocess.run(
            [
                "docker",
                "exec",
                DEV_PG_CONTAINER,
                "psql",
                "-U",
                DEV_PG_USER,
                "-d",
                DEV_PG_DB,
                "-tAc",
                f"SELECT value FROM system_settings WHERE setting_key = '{key}';",
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
    except (subprocess.SubprocessError, OSError):
        return None
    if result.returncode != 0:
        return None
    value = result.stdout.strip()
    return value or None


def _inject_embedding_env_from_db(env: dict) -> None:
    """Populate TMI_EMBEDDING_* in env from the DB for any var not already set.

    An explicitly-exported env var always wins (we only fill in blanks). Reads
    the post-#415 source of truth: the timmy.text_embedding_* system_settings.
    """
    for env_var, setting_key in EMBED_ENV_TO_SETTING.items():
        if env.get(env_var):
            continue  # operator override wins
        value = _read_setting_from_db(setting_key)
        if value:
            env[env_var] = value


def _build_worker(worker: dict, project_root: Path, verbose: bool) -> None:
    """Build a worker binary if it does not already exist."""
    binary = project_root / worker["binary"]
    if not binary.exists():
        log_info(f"Building {worker['name']} binary...")
        run_cmd(
            worker["build_cmd"],
            cwd=project_root,
            verbose=verbose,
        )
    else:
        log_info(f"Binary already built: {worker['binary']}")


def _start_worker(
    worker: dict,
    project_root: Path,
    nats_url: str,
    verbose: bool,
) -> int | None:
    """Start a single worker process. Returns the PID on success, None on failure."""
    pid_file = project_root / worker["pid_file"]
    log_file_path = project_root / worker["log_file"]
    binary = project_root / worker["binary"]

    # Check for a stale / already-running process
    existing_pid = read_pid_file(pid_file)
    if existing_pid is not None:
        log_info(f"{worker['name']} already running (PID {existing_pid})")
        return existing_pid

    # Ensure binary exists (build if needed)
    _build_worker(worker, project_root, verbose)

    # Build the worker's environment
    env = {**os.environ}
    env["TMI_NATS_URL"] = nats_url
    env["TMI_COMPONENT_NAME"] = worker["component_name"]

    # Populate embedding config from the DB (post-#415 source of truth) for any
    # var the operator did not export, then warn about anything still missing.
    if worker.get("require_embed_env"):
        _inject_embedding_env_from_db(env)
        missing = [v for v in EMBED_ENV_TO_SETTING if not env.get(v)]
        if missing:
            log_warn(
                f"{worker['name']}: embedding config not set and not found in DB: "
                + ", ".join(missing)
                + " — worker will exit if embedding is attempted. "
                + "Seed the timmy.text_embedding_* system_settings or export the env vars."
            )
        else:
            log_info(
                f"{worker['name']}: embedding config resolved "
                "(env overrides + DB system_settings)"
            )

    # Ensure log directory exists
    log_file_path.parent.mkdir(parents=True, exist_ok=True)

    log_info(f"Starting {worker['name']} (log: {worker['log_file']})")
    with open(log_file_path, "w") as lf:
        proc = subprocess.Popen(
            [str(binary)],
            stdout=lf,
            stderr=lf,
            cwd=project_root,
            env=env,
        )

    write_pid_file(pid_file, proc.pid)

    # Brief pause to catch immediate crash (e.g. NATS not reachable)
    time.sleep(2)
    if proc.poll() is not None:
        log_error(
            f"{worker['name']} exited immediately after starting. "
            f"Check {worker['log_file']}"
        )
        pid_file.unlink(missing_ok=True)
        return None

    log_success(f"{worker['name']} started with PID: {proc.pid}")
    return proc.pid


def _stop_worker(worker: dict, project_root: Path) -> None:
    """Stop a single worker process."""
    pid_file = project_root / worker["pid_file"]

    log_info(f"Stopping {worker['name']}...")

    # Layer 1: Kill via PID file
    pid = read_pid_file(pid_file)
    if pid is not None:
        graceful_kill(pid)

    # Layer 2: Find by binary name via ps aux
    binary_name = Path(worker["binary"]).name
    try:
        result = subprocess.run(
            ["ps", "aux"],
            capture_output=True,
            text=True,
        )
        for line in result.stdout.splitlines():
            if binary_name in line and "grep" not in line.split():
                parts = line.split()
                if len(parts) >= 2:
                    try:
                        orphan_pid = int(parts[1])
                        if orphan_pid != pid:  # avoid double-killing
                            graceful_kill(orphan_pid)
                    except ValueError:
                        pass
    except Exception:
        pass

    # Clean up PID file
    pid_file.unlink(missing_ok=True)
    log_success(f"{worker['name']} stopped")


# ---------------------------------------------------------------------------
# Subcommand implementations
# ---------------------------------------------------------------------------


def cmd_start(cfg: dict, args: argparse.Namespace) -> None:
    """Start all worker processes.

    Workers that exit immediately (e.g. tmi-chunk-embed when embedding env
    vars are absent) are logged as warnings rather than hard failures.  The
    caller (make start-dev) should still succeed so that the extraction
    pipeline's happy path (tmi-extractor) is exercisable without a full
    embedding-service setup.
    """
    project_root = get_project_root()
    nats_url = cfg["nats_url"]
    verbose = getattr(args, "verbose", False)

    for w in WORKERS:
        _start_worker(w, project_root, nats_url, verbose)


def cmd_stop(cfg: dict, args: argparse.Namespace) -> None:
    """Stop all worker processes."""
    project_root = get_project_root()
    for w in WORKERS:
        _stop_worker(w, project_root)


# ---------------------------------------------------------------------------
# Config resolution
# ---------------------------------------------------------------------------


def resolve_config(args: argparse.Namespace) -> dict:
    """Build effective configuration from defaults and CLI flags."""
    nats_url = getattr(args, "nats_url", None) or os.environ.get("TMI_NATS_URL") or DEFAULT_NATS_URL
    return {"nats_url": nats_url}


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

SUBCOMMANDS = {
    "start": (cmd_start, "Start extractor and chunk-embed worker processes"),
    "stop": (cmd_stop, "Stop extractor and chunk-embed worker processes"),
}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="manage-workers.py",
        description="Manage TMI async-extraction worker processes.",
    )

    add_config_arg(parser)
    parser.add_argument(
        "--nats-url",
        metavar="URL",
        default=None,
        dest="nats_url",
        help=f"NATS server URL (default: {DEFAULT_NATS_URL})",
    )
    add_verbosity_args(parser)

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
