# /// script
# requires-python = ">=3.11"
# ///
"""Lint orchestration: unsafe union check + golangci-lint.

Usage:
    uv run scripts/lint.py
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_info,
    log_success,
    run_cmd,
)


def main() -> None:
    project_root = get_project_root()

    log_info("Checking for unsafe union method calls...")
    run_cmd(
        ["uv", "run", "scripts/check-unsafe-union-methods.py"],
        cwd=project_root,
    )

    log_info("Checking for c.JSON(error) calls missing c.Abort()/return...")
    run_cmd(
        ["uv", "run", "scripts/check-missing-abort.py"],
        cwd=project_root,
    )

    log_info("Checking for direct http.Client / http.DefaultClient use in api/...")
    run_cmd(
        ["uv", "run", "scripts/check-direct-http-client.py"],
        cwd=project_root,
    )

    log_info("Checking that every OpenAPI operation declares x-tmi-authz...")
    run_cmd(
        ["uv", "run", "scripts/check-x-tmi-authz.py"],
        cwd=project_root,
    )

    log_info("Checking handler files for ad-hoc authz calls...")
    run_cmd(
        ["uv", "run", "scripts/check-no-adhoc-authz.py"],
        cwd=project_root,
    )

    log_info("Running golangci-lint...")
    golangci = Path.home() / "go" / "bin" / "golangci-lint"
    run_cmd(
        [
            str(golangci), "run",
            "./api/...", "./auth/...", "./cmd/...", "./internal/...",
        ],
        cwd=project_root,
    )

    log_success("Lint passed")


if __name__ == "__main__":
    main()
