# /// script
# requires-python = ">=3.11"
# ///
"""Manage Terraform infrastructure operations.

Subcommands: init, plan, apply, validate, fmt, output, destroy
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
    log_info,
    log_success,
    log_warn,
    run_cmd,
)

INSTALL_INSTRUCTIONS = "Homebrew: brew install terraform"


def get_tf_dir(environment: str) -> Path:
    """Return the Terraform environment directory."""
    return get_project_root() / "terraform" / "environments" / environment


def cmd_init(tf_dir: Path, _args: argparse.Namespace) -> None:
    log_info(f"Initializing Terraform in {tf_dir}...")
    run_cmd(["terraform", "init"], cwd=tf_dir)
    log_success("Terraform initialized successfully")


def cmd_plan(tf_dir: Path, _args: argparse.Namespace) -> None:
    cmd_init(tf_dir, _args)
    log_info("Planning Terraform changes...")
    run_cmd(
        ["terraform", "plan", "-out=tfplan"],
        cwd=tf_dir,
        env={"GODEBUG": "x509negativeserial=1"},
    )
    log_success(f"Terraform plan saved to {tf_dir}/tfplan")


def cmd_apply(tf_dir: Path, args: argparse.Namespace) -> None:
    cmd_init(tf_dir, args)
    log_info("Applying Terraform changes...")
    cmd = ["terraform", "apply"]
    if args.from_plan:
        cmd.append("tfplan")
    elif args.auto_approve:
        cmd.append("-auto-approve")
    run_cmd(cmd, cwd=tf_dir, env={"GODEBUG": "x509negativeserial=1"})
    log_success("Terraform apply completed")


def cmd_validate(tf_dir: Path, _args: argparse.Namespace) -> None:
    cmd_init(tf_dir, _args)
    log_info("Validating Terraform configuration...")
    run_cmd(["terraform", "validate"], cwd=tf_dir)
    log_success("Terraform configuration is valid")


def cmd_fmt(_tf_dir: Path, _args: argparse.Namespace) -> None:
    log_info("Formatting Terraform files...")
    run_cmd(
        ["terraform", "fmt", "-recursive", "terraform/"],
        cwd=get_project_root(),
    )
    log_success("Terraform files formatted")


def cmd_output(tf_dir: Path, _args: argparse.Namespace) -> None:
    log_info("Terraform outputs...")
    run_cmd(["terraform", "output"], cwd=tf_dir)


def cmd_destroy(tf_dir: Path, args: argparse.Namespace) -> None:
    log_warn("This will destroy all infrastructure!")
    cmd = ["terraform", "destroy"]
    if args.auto_approve:
        cmd.append("-auto-approve")
    run_cmd(cmd, cwd=tf_dir)


SUBCOMMANDS = {
    "init": (cmd_init, "Initialize Terraform"),
    "plan": (cmd_plan, "Plan changes"),
    "apply": (cmd_apply, "Apply changes"),
    "validate": (cmd_validate, "Validate configuration"),
    "fmt": (cmd_fmt, "Format files"),
    "output": (cmd_output, "Show outputs"),
    "destroy": (cmd_destroy, "Destroy infrastructure"),
}


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Manage Terraform infrastructure operations.",
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "subcommand",
        choices=list(SUBCOMMANDS.keys()),
        help="Terraform operation to perform",
    )
    parser.add_argument(
        "--environment",
        default="oci-public",
        help="Terraform environment (default: oci-public)",
    )
    parser.add_argument(
        "--auto-approve",
        action="store_true",
        default=False,
        help="Skip interactive approval for apply/destroy",
    )
    parser.add_argument(
        "--from-plan",
        action="store_true",
        default=False,
        help="Apply from saved tfplan file",
    )
    args = parser.parse_args()
    apply_verbosity(args)

    check_tool("terraform", install_instructions=INSTALL_INSTRUCTIONS)

    tf_dir = get_tf_dir(args.environment)
    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(tf_dir, args)


if __name__ == "__main__":
    main()
