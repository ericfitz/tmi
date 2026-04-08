# /// script
# requires-python = ">=3.11"
# ///
"""Generate CycloneDX SBOMs for the TMI application.

Usage:
    uv run scripts/generate-sbom.py [flags]

Flags:
    --all    Also generate module SBOMs (cyclonedx-gomod mod)
    -v/--verbose, -q/--quiet
"""

import argparse
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    format_version,
    get_project_root,
    log_error,
    log_info,
    log_success,
    read_version,
    run_cmd,
)


def check_cyclonedx() -> None:
    """Verify cyclonedx-gomod is installed; exit with instructions if not."""
    if shutil.which("cyclonedx-gomod") is None:
        log_error("cyclonedx-gomod not found")
        print("")
        log_info("Install using:")
        print("  Homebrew: brew install cyclonedx/cyclonedx/cyclonedx-gomod")
        print("  Go:       go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest")
        sys.exit(1)


def check_grype() -> None:
    """Verify grype is installed; exit with instructions if not."""
    if shutil.which("grype") is None:
        log_error("Grype not found")
        print("")
        log_info("Install using:")
        print("  Homebrew: brew install grype")
        print("  Script:   curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin")
        sys.exit(1)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Generate CycloneDX SBOMs for the TMI application.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--all",
        action="store_true",
        default=False,
        help="Also generate module SBOMs (cyclonedx-gomod mod)",
    )
    add_verbosity_args(parser)
    args = parser.parse_args()
    apply_verbosity(args)

    check_cyclonedx()

    project_root = get_project_root()
    version = read_version()
    version_str = format_version(version)

    sbom_dir = project_root / "security-reports" / "sbom"
    sbom_dir.mkdir(parents=True, exist_ok=True)

    json_output = sbom_dir / f"tmi-server-{version_str}-sbom.json"
    xml_output = sbom_dir / f"tmi-server-{version_str}-sbom.xml"

    verbose = getattr(args, "verbose", False)

    log_info(f"Generating SBOM for Go application (version {version_str})...")

    run_cmd(
        [
            "cyclonedx-gomod", "app",
            "-json",
            "-output", str(json_output),
            "-main", "cmd/server",
        ],
        cwd=project_root,
        verbose=verbose,
    )
    log_success(f"SBOM generated: {json_output.relative_to(project_root)}")

    run_cmd(
        [
            "cyclonedx-gomod", "app",
            "-output", str(xml_output),
            "-main", "cmd/server",
        ],
        cwd=project_root,
        verbose=verbose,
    )
    log_success(f"SBOM generated: {xml_output.relative_to(project_root)}")

    if args.all:
        log_info("Generating module SBOMs...")

        mod_json = sbom_dir / f"tmi-module-{version_str}-sbom.json"
        mod_xml = sbom_dir / f"tmi-module-{version_str}-sbom.xml"

        run_cmd(
            [
                "cyclonedx-gomod", "mod",
                "-json",
                "-output", str(mod_json),
            ],
            cwd=project_root,
            verbose=verbose,
        )
        run_cmd(
            [
                "cyclonedx-gomod", "mod",
                "-output", str(mod_xml),
            ],
            cwd=project_root,
            verbose=verbose,
        )
        log_success("All Go SBOMs generated in security-reports/sbom/")


if __name__ == "__main__":
    main()
