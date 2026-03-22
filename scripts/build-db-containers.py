#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Build TMI database containers (PostgreSQL).

The DB container is always PostgreSQL (Chainguard Dockerfile.postgres).
There is no --db-backend flag — the database engine choice affects only the
server container. Cloud deployments that use managed database services
(OCI Oracle ADB, Heroku Postgres addon) do not need a database container.
"""

import argparse
import sys
from pathlib import Path

# Import shared helpers from same directory
sys.path.insert(0, str(Path(__file__).resolve().parent))
import container_build_helpers as helpers  # noqa: E402


VALID_TARGETS = ("local", "aws", "azure", "gcp")
UNSUPPORTED_TARGETS = {
    "oci": "OCI uses Oracle ADB (managed service); no database container to build.",
    "heroku": "Heroku uses Postgres addon; no database container to build.",
}
VALID_ARCHS = ("arm64", "amd64", "both")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build TMI database containers",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--target",
        default="local",
        help=f"Deployment target: {', '.join(VALID_TARGETS)} (default: local)",
    )
    parser.add_argument(
        "--arch",
        choices=VALID_ARCHS,
        default=None,
        help="Target architecture (default: auto-detect for local, provider default for cloud)",
    )
    parser.add_argument(
        "--registry",
        default=None,
        help="Container registry URL (auto-determined from target if not set)",
    )
    parser.add_argument(
        "--push",
        action="store_true",
        help="Push images to registry after building",
    )
    parser.add_argument(
        "--scan",
        action="store_true",
        help="Run security scanning (Grype + Syft SBOM) after building",
    )
    parser.add_argument(
        "--scan-only",
        action="store_true",
        help="Scan existing images without building",
    )
    parser.add_argument(
        "--no-cache",
        action="store_true",
        help="Build without Docker cache",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()

    # Check for unsupported targets
    if args.target in UNSUPPORTED_TARGETS:
        helpers.log_error(UNSUPPORTED_TARGETS[args.target])
        sys.exit(1)

    if args.target not in VALID_TARGETS:
        helpers.log_error(
            f"Unknown target: {args.target}. "
            f"Valid targets: {', '.join(VALID_TARGETS)}"
        )
        sys.exit(1)

    # Validate flag combinations
    if args.push and args.target == "local":
        helpers.log_error(
            "Cannot push to local Docker daemon. "
            "Use a cloud target or specify --registry."
        )
        sys.exit(1)

    if args.arch == "both" and not args.push:
        helpers.log_error(
            "Multi-arch builds require --push "
            "(cannot load multi-platform images into local Docker daemon). "
            "Use --push with a registry, or build for a single architecture."
        )
        sys.exit(1)

    project_root = helpers.get_project_root()

    # Get target config — use postgresql backend since this is the DB builder
    config = helpers.get_target_config(
        args.target, args.arch, "postgresql", args.registry
    )

    # Check prerequisites
    helpers.check_prerequisites(
        need_buildx=config.use_buildx,
        need_scan=args.scan or args.scan_only,
    )

    # Read version and git info
    version = helpers.read_version(project_root)
    git_commit = helpers.get_git_commit()
    build_date = helpers.get_build_date()

    helpers.log_info(f"Target: {args.target}")
    helpers.log_info(f"Component: postgres")
    helpers.log_info(f"Platform: {config.platform}")
    helpers.log_info(f"Version: {helpers.format_version(version)}")

    # Dockerfile for postgres
    dockerfile = "Dockerfile.postgres"
    dockerfile_path = project_root / dockerfile
    if not dockerfile_path.exists():
        helpers.log_error(f"Dockerfile not found: {dockerfile_path}")
        sys.exit(1)

    image_name = f"{config.image_name_prefix}postgresql"

    if args.scan_only:
        passed = helpers.scan_image(
            f"{image_name}:latest",
            project_root / "security-reports",
        )
        helpers.generate_security_summary(
            project_root / "security-reports", build_date, git_commit
        )
        if not passed:
            helpers.log_error("Database image failed security scan")
            sys.exit(1)
        helpers.log_success("Scan completed")
        return

    # Authenticate if pushing
    if args.push:
        helpers.authenticate_registry(config)

    # Build
    helpers.log_info(f"Building postgres using {dockerfile}...")
    tags = [
        f"{image_name}:latest",
        f"{image_name}:v{helpers.format_version(version)}",
        f"{image_name}:{git_commit}",
    ]
    build_args = helpers.get_build_args(version, git_commit, build_date)

    helpers.run_docker_build(
        config,
        dockerfile,
        project_root,
        tags,
        build_args,
        push=args.push,
        no_cache=args.no_cache,
    )

    # Scan if requested
    if args.scan:
        passed = helpers.scan_image(
            f"{image_name}:latest",
            project_root / "security-reports",
        )
        helpers.generate_security_summary(
            project_root / "security-reports", build_date, git_commit
        )
        if not passed:
            helpers.log_error("Database image failed security scan")
            sys.exit(1)

    helpers.log_success("Database container build complete!")


if __name__ == "__main__":
    main()
