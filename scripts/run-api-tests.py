# /// script
# requires-python = ">=3.11"
# ///
"""Run TMI API tests via Newman/Postman collections.

Supports three modes:
  - Default: run the full API test suite via test/postman/run-tests.sh
  - --collection NAME: run a specific collection via run-postman-collection.sh
  - --list: list available collection names
"""

import argparse
import os
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (
    add_verbosity_args,
    apply_verbosity,
    check_tool,
    get_project_root,
    log_error,
    log_info,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run TMI API tests via Newman/Postman collections."
    )
    add_verbosity_args(parser)

    mode = parser.add_mutually_exclusive_group()
    mode.add_argument(
        "--collection",
        metavar="NAME",
        default=None,
        help="Run a specific Postman collection by name (stem of .json file)",
    )
    mode.add_argument(
        "--list",
        action="store_true",
        default=False,
        help="List available Postman collections",
    )

    parser.add_argument(
        "--start-server",
        action="store_true",
        default=False,
        help="Auto-start the TMI server before running tests",
    )
    parser.add_argument(
        "--response-time-multiplier",
        metavar="N",
        type=int,
        default=1,
        help="Scale response-time thresholds (default: 1, higher for remote DBs)",
    )

    return parser.parse_args()


def list_collections(postman_dir: Path) -> list[str]:
    """Return sorted collection names (stems of .json files)."""
    return sorted(p.stem for p in postman_dir.glob("*.json"))


def main() -> int:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()
    postman_dir = project_root / "test" / "postman"

    # --list mode: no tool check needed
    if args.list:
        log_info("Available Postman collections:")
        for name in list_collections(postman_dir):
            print(f"  {name}")
        return 0

    # All other modes require newman
    check_tool("newman", install_instructions="pnpm install -g newman")

    env = {**os.environ, "RESPONSE_TIME_MULTIPLIER": str(args.response_time_multiplier)}

    if args.collection:
        # --collection mode
        collection_file = postman_dir / f"{args.collection}.json"
        if not collection_file.exists():
            log_error(f"Collection not found: {collection_file}")
            log_info("Available collections:")
            for name in list_collections(postman_dir):
                print(f"  {name}")
            return 1

        log_info(f"Running Postman collection: {args.collection}")
        script = postman_dir / "run-postman-collection.sh"
        result = subprocess.run(
            ["bash", str(script), args.collection],
            cwd=str(project_root),
            env=env,
            check=False,
        )
        return result.returncode

    # Default mode: full test suite
    log_info("Running comprehensive API test suite...")
    script = postman_dir / "run-tests.sh"
    if not script.exists():
        log_error(f"API test script not found at {script}")
        return 1

    cmd = ["bash", str(script)]
    if args.start_server:
        cmd.append("--start-server")

    result = subprocess.run(
        cmd,
        cwd=str(project_root),
        env=env,
        check=False,
    )
    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
