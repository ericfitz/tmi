# /// script
# requires-python = ">=3.11"
# ///
"""Manage Arazzo workflow specification generation.

Subcommands: install, scaffold, enhance, generate (all three in sequence)
"""

import argparse
import sys
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


def cmd_install(project_root: Path) -> None:
    log_info("Installing Arazzo tooling...")
    run_cmd(["pnpm", "install"], cwd=project_root)
    log_success("Arazzo tools installed")


def cmd_scaffold(project_root: Path) -> None:
    log_info("Generating base scaffold with Redocly CLI...")
    run_cmd(
        ["bash", str(project_root / "scripts" / "generate-arazzo-scaffold.sh")],
        cwd=project_root,
    )
    log_success("Base scaffold generated")


def cmd_enhance(project_root: Path) -> None:
    log_info("Enhancing with TMI workflow data...")
    run_cmd(
        ["uv", "run", str(project_root / "scripts" / "enhance-arazzo-with-workflows.py")],
        cwd=project_root,
    )
    log_success("Enhanced Arazzo created at api-schema/tmi.arazzo.yaml and .json")


def cmd_validate(project_root: Path) -> None:
    run_cmd(
        [
            "uv",
            "run",
            str(project_root / "scripts" / "validate-arazzo.py"),
            str(project_root / "api-schema" / "tmi.arazzo.yaml"),
            str(project_root / "api-schema" / "tmi.arazzo.json"),
        ],
        cwd=project_root,
    )


def cmd_generate(project_root: Path) -> None:
    cmd_install(project_root)
    cmd_scaffold(project_root)
    cmd_enhance(project_root)
    cmd_validate(project_root)
    log_success("Arazzo specification generation complete")


SUBCOMMANDS = {
    "install": (cmd_install, "Install Arazzo tooling (pnpm)"),
    "scaffold": (cmd_scaffold, "Generate base scaffold"),
    "enhance": (cmd_enhance, "Enhance with TMI workflow data"),
    "generate": (cmd_generate, "Full pipeline: install + scaffold + enhance + validate"),
}


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage Arazzo workflow specification.")
    add_verbosity_args(parser)
    parser.add_argument("subcommand", choices=list(SUBCOMMANDS.keys()))
    args = parser.parse_args()
    apply_verbosity(args)

    project_root = get_project_root()
    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(project_root)


if __name__ == "__main__":
    main()
