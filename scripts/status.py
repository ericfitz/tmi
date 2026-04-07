# /// script
# requires-python = ">=3.11"
# ///
"""Display a status dashboard for all TMI services."""

import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    GREEN,
    NC,
    RED,
    YELLOW,
    get_project_root,
    read_pid_file,
    run_cmd,
)

# ---------------------------------------------------------------------------
# Service checks
# ---------------------------------------------------------------------------

BLUE = "\033[0;34m"


def get_service_pid() -> tuple[int | None, str]:
    """Find the tmiserver PID by scanning lsof on port 8080, with PID file fallback.

    Returns (pid, process_name) or (None, "").
    """
    result = run_cmd(["lsof", "-ti", ":8080"], check=False, capture=True)
    if result.returncode == 0 and result.stdout.strip():
        for line in result.stdout.splitlines():
            line = line.strip()
            if not line.isdigit():
                continue
            pid = int(line)
            proc = run_cmd(["ps", "-p", str(pid), "-o", "args="], check=False, capture=True)
            cmd_str = proc.stdout.strip() if proc.returncode == 0 else ""
            if "bin/tmiserver" in cmd_str or ("tmiserver" in cmd_str and "--config" in cmd_str):
                name_result = run_cmd(
                    ["bash", "-c", f"ps -p {pid} -o args= | awk '{{print $1}}' | xargs basename"],
                    check=False, capture=True,
                )
                name = name_result.stdout.strip() if name_result.returncode == 0 else "tmiserver"
                return pid, name

    # Fallback to PID file
    project_root = get_project_root()
    pid = read_pid_file(project_root / ".server.pid")
    if pid is not None:
        proc = run_cmd(["ps", "-p", str(pid), "-o", "args="], check=False, capture=True)
        cmd_str = proc.stdout.strip() if proc.returncode == 0 else ""
        if "bin/tmiserver" in cmd_str or ("tmiserver" in cmd_str and "--config" in cmd_str):
            name_result = run_cmd(
                ["bash", "-c", f"ps -p {pid} -o args= | awk '{{print $1}}' | xargs basename"],
                check=False, capture=True,
            )
            name = name_result.stdout.strip() if name_result.returncode == 0 else "tmiserver"
            return pid, name

    return None, ""


def check_service() -> None:
    """Print status row for the TMI server service on port 8080."""
    pid, name = get_service_pid()
    if pid is None:
        print(
            f"{RED}✗{NC} "
            f"{'Service':<23} {'8080':<6} {'Stopped':<13} {'-':<35} make start-server"
        )
        return

    # Attempt health check
    result = subprocess.run(
        ["curl", "-s", "--connect-timeout", "2", "--max-time", "5",
         "-w", "\n%{http_code}", "http://localhost:8080"],
        capture_output=True, text=True,
    )
    output = result.stdout if result.returncode == 0 else "\n000"
    lines = output.strip().splitlines()
    http_code = lines[-1] if lines else "000"
    body = "\n".join(lines[:-1]) if len(lines) > 1 else ""

    process_col = f"{pid} ({name})"

    if http_code in ("200", "429"):
        status = "Running (429)" if http_code == "429" else "Running"
        print(
            f"{GREEN}✓{NC} "
            f"{'Service':<23} {'8080':<6} {status:<13} {process_col:<35} make stop-server"
        )
        # Try to extract version info
        try:
            data = json.loads(body)
            api_version = data.get("api", {}).get("version", "unknown")
            service_build = data.get("service", {}).get("build", "unknown")
            if api_version != "unknown" or service_build != "unknown":
                print(f"  {'': <44} API Version: {api_version}, Build: {service_build}")
        except (json.JSONDecodeError, AttributeError):
            pass
    else:
        print(
            f"{YELLOW}⏳{NC} "
            f"{'Service':<23} {'8080':<6} {'Starting':<13} {process_col:<35} make stop-server"
        )
        print(f"  {'': <44} Process running but not responding (migrations/init)")


def check_database() -> None:
    """Print status row for the PostgreSQL container on port 5432."""
    result = run_cmd(
        ["docker", "ps", "--filter", "name=tmi-postgresql", "--filter", "status=running",
         "--format", "{{.Names}}"],
        check=False, capture=True,
    )
    container = result.stdout.strip().splitlines()[0] if result.stdout.strip() else ""
    if container:
        print(
            f"{GREEN}✓{NC} "
            f"{'Database':<23} {'5432':<6} {'Running':<13} {'container: ' + container:<35} make stop-database"
        )
    else:
        print(
            f"{RED}✗{NC} "
            f"{'Database':<23} {'5432':<6} {'Stopped':<13} {'-':<35} make start-database"
        )


def check_redis() -> None:
    """Print status row for the Redis container on port 6379."""
    result = run_cmd(
        ["docker", "ps", "--filter", "name=tmi-redis", "--filter", "status=running",
         "--format", "{{.Names}}"],
        check=False, capture=True,
    )
    container = result.stdout.strip().splitlines()[0] if result.stdout.strip() else ""
    if container:
        print(
            f"{GREEN}✓{NC} "
            f"{'Redis':<23} {'6379':<6} {'Running':<13} {'container: ' + container:<35} make stop-redis"
        )
    else:
        print(
            f"{RED}✗{NC} "
            f"{'Redis':<23} {'6379':<6} {'Stopped':<13} {'-':<35} make start-redis"
        )


def check_application() -> None:
    """Print status row for the frontend application on port 4200."""
    result = run_cmd(
        ["lsof", "-sTCP:LISTEN", "-ti", ":4200"],
        check=False, capture=True,
    )
    pid_line = result.stdout.strip().splitlines()[0] if result.stdout.strip() else ""
    if pid_line and pid_line.isdigit():
        pid = int(pid_line)
        name_result = run_cmd(
            ["bash", "-c", f"ps -p {pid} -o args= | awk '{{print $1}}' | xargs basename"],
            check=False, capture=True,
        )
        name = name_result.stdout.strip() if name_result.returncode == 0 else "unknown"
        process_col = f"{pid} ({name})"
        print(
            f"{GREEN}✓{NC} "
            f"{'Application':<23} {'4200':<6} {'Running':<13} {process_col:<35} -"
        )
    else:
        print(
            f"{RED}✗{NC} "
            f"{'Application':<23} {'4200':<6} {'Stopped':<13} {'-':<35} -"
        )


def check_oauth_stub() -> None:
    """Print status row for the OAuth stub on port 8079 (optional service)."""
    result = run_cmd(
        ["lsof", "-ti", ":8079"],
        check=False, capture=True,
    )
    pid_line = result.stdout.strip().splitlines()[0] if result.stdout.strip() else ""
    if pid_line and pid_line.isdigit():
        pid = int(pid_line)
        name_result = run_cmd(
            ["bash", "-c", f"ps -p {pid} -o args= | awk '{{print $1}}' | xargs basename"],
            check=False, capture=True,
        )
        name = name_result.stdout.strip() if name_result.returncode == 0 else "unknown"
        process_col = f"{pid} ({name})"
        print(
            f"{GREEN}✓{NC} "
            f"{'OAuth Stub':<23} {'8079':<6} {'Running':<13} {process_col:<35} make stop-oauth-stub"
        )
    else:
        print(
            f"{YELLOW}⚬{NC} "
            f"{'OAuth Stub (Optional)':<23} {'8079':<6} {'Not running':<13} {'-':<35} make start-oauth-stub"
        )


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main() -> None:
    """Print the TMI service status dashboard."""
    print("TMI Service Status Check")
    print("========================")
    print()
    print(f"{'S':<1} {'SERVICE':<23} {'PORT':<6} {'STATUS':<13} {'PROCESS':<35} CHANGE STATE")
    print(
        f"{'-':<1} {'-' * 23:<23} {'-' * 6:<6} {'-' * 13:<13} {'-' * 35:<35} {'-' * 20}"
    )

    check_service()
    check_database()
    check_redis()
    check_application()
    check_oauth_stub()

    print()


if __name__ == "__main__":
    main()
