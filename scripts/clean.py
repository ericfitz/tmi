# /// script
# requires-python = ">=3.11"
# ///
"""Cleanup orchestration for TMI services and artifacts.

Subcommands:
  logs       - Remove log files and PID files
  files      - Remove logs + CATS artifacts
  process    - Stop server and OAuth stub processes
  build      - Remove build artifacts from bin/ directory
  containers - Stop and remove development containers
  all        - Stop processes, clean containers, remove all artifacts
"""

import argparse
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_info,
    log_success,
    run_cmd,
)

# ---------------------------------------------------------------------------
# Subcommand implementations
# ---------------------------------------------------------------------------


def clean_logs() -> None:
    """Remove log files and PID files from the project root and logs/ directory."""
    project_root = get_project_root()

    log_info("Cleaning up log files...")
    for filename in ("integration-test.log", "server.log", ".server.pid"):
        path = project_root / filename
        if path.exists():
            log_info(f"Removing file: {filename}")
            path.unlink()

    logs_dir = project_root / "logs"
    if logs_dir.is_dir():
        contents = list(logs_dir.iterdir())
        if contents:
            log_info("Removing logs/* files")
            for item in contents:
                if item.is_file():
                    item.unlink()
                elif item.is_dir():
                    import shutil
                    shutil.rmtree(item)

    log_success("Log files cleaned")


def clean_files() -> None:
    """Remove logs, CATS artifacts, and the cats-report directory."""
    clean_logs()

    project_root = get_project_root()

    log_info("Cleaning CATS artifacts...")
    run_cmd(["pkill", "-f", "cats"], check=False)
    time.sleep(1)

    cats_dir = project_root / "test" / "outputs" / "cats"
    if cats_dir.is_dir():
        preserve = {"cats-results.db", "cats-results.db-shm", "cats-results.db-wal"}
        for item in cats_dir.iterdir():
            if item.name not in preserve:
                if item.is_file() or item.is_symlink():
                    item.unlink()
                elif item.is_dir():
                    import shutil
                    shutil.rmtree(item)

    cats_report = project_root / "cats-report"
    if cats_report.exists():
        import shutil
        shutil.rmtree(cats_report)

    # Clean wstest logs
    wstest_dir = project_root / "wstest"
    if wstest_dir.is_dir():
        for logfile in wstest_dir.glob("*.log"):
            logfile.unlink()

    log_success("File cleanup completed")


def clean_process() -> None:
    """Stop the TMI server and OAuth stub processes."""
    scripts_dir = get_project_root() / "scripts"
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-server.py"), "stop"],
        check=False,
    )
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-oauth-stub.py"), "stop"],
        check=False,
    )
    # Stop wstest processes
    run_cmd(["pkill", "-f", "wstest"], check=False)


def clean_all() -> None:
    """Stop processes, clean containers, and remove all artifacts."""
    clean_process()

    scripts_dir = get_project_root() / "scripts"
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-redis.py"), "clean"],
        check=False,
    )
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-database.py"), "--test", "clean"],
        check=False,
    )
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-redis.py"), "--test", "clean"],
        check=False,
    )

    clean_files()


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def clean_build() -> None:
    """Remove build artifacts from bin/ directory."""
    project_root = get_project_root()
    log_info("Cleaning build artifacts...")
    bin_dir = project_root / "bin"
    if bin_dir.is_dir():
        for item in bin_dir.iterdir():
            item.unlink()
    migrate = project_root / "migrate"
    if migrate.exists():
        migrate.unlink()
    log_success("Build artifacts cleaned")


def clean_containers() -> None:
    """Stop and remove development containers."""
    scripts_dir = get_project_root() / "scripts"
    log_info("Cleaning up containers...")
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-database.py"), "clean"],
        check=False,
    )
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-redis.py"), "clean"],
        check=False,
    )
    log_success("Container cleanup completed")


SUBCOMMANDS = {
    "logs": clean_logs,
    "files": clean_files,
    "process": clean_process,
    "build": clean_build,
    "containers": clean_containers,
    "all": clean_all,
}


def main() -> None:
    """Parse arguments and dispatch to the appropriate cleanup subcommand."""
    parser = argparse.ArgumentParser(
        description="Cleanup orchestration for TMI services and artifacts.",
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "subcommand",
        choices=list(SUBCOMMANDS.keys()),
        help="Cleanup scope: logs, files, process, build, containers, or all",
    )
    args = parser.parse_args()
    apply_verbosity(args)

    SUBCOMMANDS[args.subcommand]()


if __name__ == "__main__":
    main()
