# /// script
# requires-python = ">=3.11"
# ///
"""Manage OCI Functions operations.

Subcommands: build, deploy, invoke, logs
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    check_tool,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)

FN_INSTALL = "Homebrew: brew install fn"


def require_app(args: argparse.Namespace) -> str:
    if not args.app:
        log_error("--app is required for this operation")
        log_info("Set FN_APP env var or pass --app APP_NAME")
        sys.exit(1)
    return args.app


def cmd_build(args: argparse.Namespace) -> None:
    fn_dir = get_project_root() / "functions" / args.function
    log_info(f"Building function: {args.function}...")
    run_cmd(["fn", "build"], cwd=fn_dir)
    log_success(f"Function {args.function} built successfully")


def cmd_deploy(args: argparse.Namespace) -> None:
    app = require_app(args)
    fn_dir = get_project_root() / "functions" / args.function
    log_info(f"Deploying function {args.function} to app {app}...")
    run_cmd(["fn", "deploy", "--app", app], cwd=fn_dir)
    log_success(f"Function {args.function} deployed")


def cmd_invoke(args: argparse.Namespace) -> None:
    app = require_app(args)
    log_info(f"Invoking function {args.function}...")
    run_cmd(["fn", "invoke", app, args.function])
    log_success("Function invoked")


def cmd_logs(args: argparse.Namespace) -> None:
    app = require_app(args)
    log_info(f"Fetching logs for {args.function}...")
    run_cmd(["fn", "logs", app, args.function])


SUBCOMMANDS = {
    "build": (cmd_build, "Build the function"),
    "deploy": (cmd_deploy, "Deploy to OCI (requires --app)"),
    "invoke": (cmd_invoke, "Invoke function (requires --app)"),
    "logs": (cmd_logs, "View function logs (requires --app)"),
}


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage OCI Functions.")
    add_verbosity_args(parser)
    parser.add_argument("subcommand", choices=list(SUBCOMMANDS.keys()))
    parser.add_argument("--app", default=None, help="OCI Function Application name")
    parser.add_argument("--function", default="certmgr", help="Function name (default: certmgr)")
    args = parser.parse_args()
    apply_verbosity(args)

    check_tool("fn", install_instructions=FN_INSTALL)

    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(args)


if __name__ == "__main__":
    main()
