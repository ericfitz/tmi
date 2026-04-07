# /// script
# requires-python = ">=3.11"
# ///
"""Generate API code from the OpenAPI specification using oapi-codegen.

Usage:
    uv run scripts/generate-api.py
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

    log_info("Generating API code from OpenAPI specification...")
    run_cmd(
        ["oapi-codegen", "-config", "oapi-codegen-config.yml", "api-schema/tmi-openapi.json"],
        cwd=project_root,
    )
    log_success("API code generated: api/api.go")


if __name__ == "__main__":
    main()
