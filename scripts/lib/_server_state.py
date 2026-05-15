"""Shared server-state helpers for TMI scripts.

Extracted from manage-server.py so that other scripts (clean.py, etc.) can
detect a running TMI server without duplicating the two-pronged PID / ps logic.
"""

import subprocess
from pathlib import Path

from tmi_common import read_pid_file


def running_server_pid(project_root: Path) -> int | None:
    """Return PID of a running TMI server, or None.

    Two-pronged detection:
      1. .server.pid exists and the PID is alive.
      2. ps aux shows a bin/tmiserver process (excluding grep lines).
    """
    pid_file = project_root / ".server.pid"
    if pid_file.exists():
        pid = read_pid_file(pid_file)
        if pid is not None:
            try:
                import os as _os
                _os.kill(pid, 0)
                return pid
            except (ProcessLookupError, PermissionError):
                pass

    try:
        result = subprocess.run(
            ["ps", "aux"],
            capture_output=True,
            text=True,
            check=False,
        )
        for line in result.stdout.splitlines():
            if "bin/tmiserver" in line and "grep" not in line.split():
                parts = line.split()
                if len(parts) >= 2:
                    try:
                        return int(parts[1])
                    except ValueError:
                        continue
    except Exception:
        pass

    return None
