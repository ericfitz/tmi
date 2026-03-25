#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Build TMI application containers (server, redis, promtail).

Supports local Docker builds and cloud registry push for OCI, AWS, Azure, GCP,
and Heroku targets. See --help for full usage.
"""

import argparse
import sys
from pathlib import Path

# Import shared helpers from same directory
sys.path.insert(0, str(Path(__file__).resolve().parent))
import container_build_helpers as helpers  # noqa: E402


VALID_TARGETS = ("local", "oci", "aws", "azure", "gcp", "heroku")
VALID_COMPONENTS = ("server", "redis", "promtail", "all")
VALID_ARCHS = ("arm64", "amd64", "both")
VALID_DB_BACKENDS = ("postgresql", "oracle-adb")

# Components that are valid per target
HEROKU_COMPONENTS = {"server"}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build TMI application containers",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--target",
        choices=VALID_TARGETS,
        default="local",
        help="Deployment target (default: local)",
    )
    parser.add_argument(
        "--component",
        choices=VALID_COMPONENTS,
        default="all",
        help="Component to build (default: all)",
    )
    parser.add_argument(
        "--arch",
        choices=VALID_ARCHS,
        default=None,
        help="Target architecture (default: auto-detect for local, provider default for cloud)",
    )
    parser.add_argument(
        "--db-backend",
        choices=VALID_DB_BACKENDS,
        default="postgresql",
        help="Database backend (affects server Dockerfile selection, default: postgresql)",
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


def resolve_components(component: str, target: str) -> list[str]:
    """Resolve 'all' to component list, applying target restrictions."""
    if component == "all":
        components = ["server", "redis", "promtail"]
    else:
        components = [component]

    if target == "heroku":
        skipped = [c for c in components if c not in HEROKU_COMPONENTS]
        if skipped:
            helpers.log_warn(
                f"Heroku uses addons for Redis/Promtail; "
                f"skipping: {', '.join(skipped)}. Only building server."
            )
        components = [c for c in components if c in HEROKU_COMPONENTS]
        if not components:
            helpers.log_error("No valid components to build for Heroku target")
            sys.exit(1)

    return components


def build_component(
    component: str,
    config: helpers.TargetConfig,
    project_root: Path,
    version: dict,
    git_commit: str,
    build_date: str,
    *,
    push: bool,
    no_cache: bool,
) -> None:
    """Build a single component container."""
    if component not in config.dockerfile_map:
        helpers.log_error(f"No Dockerfile configured for component: {component}")
        sys.exit(1)
    dockerfile: str = config.dockerfile_map[component]

    # Verify Dockerfile exists
    dockerfile_path = project_root / dockerfile
    if not dockerfile_path.exists():
        helpers.log_error(f"Dockerfile not found: {dockerfile_path}")
        sys.exit(1)

    helpers.log_info(f"Building {component} using {dockerfile}...")

    tags = helpers.get_image_tags(config, component, version, git_commit)
    build_args = helpers.get_build_args(version, git_commit, build_date)

    # Component-specific build args
    extra_args: list[str] = []
    if component == "redis" and "oracle" in dockerfile:
        extra_args.extend(["--build-arg", "REDIS_VERSION=8.4.0"])

    helpers.run_docker_build(
        config,
        dockerfile,
        project_root,
        tags,
        build_args,
        push=push,
        no_cache=no_cache,
        extra_build_args=extra_args if extra_args else None,
    )


def scan_component(
    component: str,
    config: helpers.TargetConfig,
    project_root: Path,
    version: dict,
    git_commit: str,
) -> bool:
    """Scan a component's container image. Returns True if passed."""
    image_name = config.image_name_map.get(
        component, f"{config.image_name_prefix}{component}"
    )
    reports_dir = project_root / "security-reports"
    return helpers.scan_image(f"{image_name}:latest", reports_dir)


def main() -> None:
    args = parse_args()

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

    # Get target config
    config = helpers.get_target_config(
        args.target, args.arch, args.db_backend, args.registry
    )

    # Check prerequisites
    need_buildx = config.use_buildx
    helpers.check_prerequisites(need_buildx=need_buildx, need_scan=args.scan or args.scan_only)

    # Resolve components
    components = resolve_components(args.component, args.target)

    # Read version and git info
    version = helpers.read_version(project_root)
    git_commit = helpers.get_git_commit()
    build_date = helpers.get_build_date()

    helpers.log_info(f"Target: {args.target}")
    helpers.log_info(f"Components: {', '.join(components)}")
    helpers.log_info(f"Platform: {config.platform}")
    helpers.log_info(f"Version: {helpers.format_version(version)}")
    helpers.log_info(f"Git commit: {git_commit}")

    if args.scan_only:
        # Scan existing images without building
        all_passed = True
        for component in components:
            if not scan_component(component, config, project_root, version, git_commit):
                all_passed = False
        helpers.generate_security_summary(
            project_root / "security-reports", build_date, git_commit
        )
        if not all_passed:
            helpers.log_error("Some images failed security scan")
            sys.exit(1)
        helpers.log_success("All scans completed")
        return

    # Authenticate if pushing
    if args.push:
        helpers.authenticate_registry(config)

    # Build each component
    all_passed = True
    for component in components:
        build_component(
            component,
            config,
            project_root,
            version,
            git_commit,
            build_date,
            push=args.push,
            no_cache=args.no_cache,
        )

        if args.scan:
            if not scan_component(component, config, project_root, version, git_commit):
                all_passed = False

    # Generate security summary if scanning was done
    if args.scan:
        helpers.generate_security_summary(
            project_root / "security-reports", build_date, git_commit
        )

    if not all_passed:
        helpers.log_error("Some images failed security scan")
        sys.exit(1)

    helpers.log_success("Build complete!")


if __name__ == "__main__":
    main()
