# /// script
# requires-python = ">=3.11"
# ///
"""Deploy TMI to Heroku.

Usage:
    uv run scripts/deploy-heroku.py
"""

import subprocess
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
    root = get_project_root()

    log_info("Starting Heroku deployment...")
    log_info("Building server binary...")
    run_cmd(["uv", "run", "scripts/build-server.py"], cwd=root)

    log_info("Checking git status...")
    result = run_cmd(["git", "status", "--porcelain"], capture=True, cwd=root)
    if result.stdout.strip():
        log_info("Committing changes...")
        run_cmd(["git", "add", "-A"], cwd=root)
        run_cmd(
            ["git", "commit", "-m", "chore: Build and deploy to Heroku [skip ci]"],
            check=False,
            cwd=root,
        )
    else:
        log_info("No changes to commit")

    log_info("Pushing to GitHub main branch...")
    run_cmd(["git", "push", "origin", "main"], cwd=root)

    log_info("Pushing to Heroku...")
    run_cmd(["git", "push", "heroku", "main"], cwd=root)

    log_success("Deployment complete!")

    log_info("Checking deployment status...")
    result = run_cmd(
        ["heroku", "releases", "--app", "tmi-server"],
        capture=True,
        cwd=root,
    )
    lines = result.stdout.splitlines()
    for line in lines[:3]:
        print(line)


if __name__ == "__main__":
    main()
