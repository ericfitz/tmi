# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Snapshot and restore the dev cluster's operational configuration.

WHY THIS EXISTS

TMI's configuration lives in three places, and only one of them is durable:

  1. Bootstrap keys        config-development.yml -> ConfigMap. Durable (a file).
  2. Env-var-only settings .local/oauth-providers.env -> Secret/tmi-oauth-providers.
                           Durable (a file); rebuilt on every dev-up by
                           scripts/lib/deploy.py create_oauth_providers_secret().
  3. Operational settings  the system_settings table in the dev database.
                           NOT durable: `make dev-nuke` deletes the namespace and
                           with it the Postgres PVC, and the next `dev-up`
                           re-seeds only classification-registry defaults.

(3) is how a dev cluster silently loses every OAuth provider, callback URL and
feature flag that was ever configured through the admin API (#791). This script
is the missing durable copy for that third bucket: `snapshot` writes the
database's settings to a gitignored file under .local/, and `restore` puts them
back.

The safety net is automatic, not a discipline you have to remember:
scripts/lib/deploy.py snapshots before every teardown (including the namespace
delete that dev-nuke performs) and restores after every start. So the sequence
that used to destroy the configuration now round-trips it.

RESTORE SEMANTICS

`restore` runs `tmi-dbtool --import-config`, which by default only writes keys
that are MISSING from the database and leaves existing rows alone. That is what
makes the automatic restore on dev-up safe to run unconditionally: on a freshly
nuked database it repopulates everything, and on a database you have since
edited it will not revert your edits. Pass --overwrite to force the snapshot to
win for every key.

COVERAGE — WHAT RESTORE DOES NOT COVER

`--import-config` writes the keys that `Config.GetMigratableSettings()` knows
about. A handful of keys live in the classification registry (so the server
seeds them) without a matching Config struct field, so `--export-config` writes
them out but `--import-config` silently drops them:

    features.webhooks_enabled       rate_limit.requests_per_hour
    features.websocket_enabled      rate_limit.requests_per_minute
    ui.default_theme                upload.max_file_size_mb
    websocket.max_participants

The server re-seeds all of them at their DEFAULTS on startup, so they always
come back — what does not survive is a CUSTOMIZED value for one of them. That
is a much smaller hole than "restore does not work", but it is a real one; see
the follow-up issue linked from #791. Everything with a struct field — every
OAuth/SAML provider key, the callback allowlist, all the timmy and
content_extractors tuning, the JWT and cookie settings — round-trips.

USAGE

    uv run scripts/dev-config.py snapshot --cluster k3s
    uv run scripts/dev-config.py restore  --cluster k3s
    uv run scripts/dev-config.py status   --cluster k3s

or via the make targets: dev-config-snapshot / dev-config-restore /
dev-config-status, each honoring CLUSTER=.
"""
from __future__ import annotations

import argparse
import os
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from deploy import (  # noqa: E402
    OAUTH_PROVIDERS_ENV_FILE, ensure_port_forward,
)
from tmi_common import (  # noqa: E402
    add_config_arg, add_verbosity_args, apply_verbosity, get_project_root,
    log_error, log_info, log_success, log_warn, run_cmd,
)

# Per-cluster because the two dev clusters legitimately disagree on
# host-dependent values -- auth.oauth_callback_url is
# http://localhost:8080/oauth2/callback on docker-desktop but
# https://tmi.efitz.net/api/oauth2/callback on k3s -- and a single shared file
# would push the wrong callback to whichever cluster it was not taken from.
SNAPSHOT_DIR = ".local"
CLUSTERS = ("k3s", "docker-desktop")


def snapshot_path(cluster: str) -> Path:
    return get_project_root() / SNAPSHOT_DIR / f"dev-config-{cluster}.yaml"


def build_dbtool() -> Path:
    """Build bin/tmi-dbtool (Postgres/database-agnostic build)."""
    root = get_project_root()
    log_info("Building tmi-dbtool...")
    run_cmd(
        ["go", "build", "-o", "bin/tmi-dbtool", "github.com/ericfitz/tmi/cmd/dbtool"],
        cwd=str(root),
    )
    return root / "bin" / "tmi-dbtool"


def _dbtool(argv: list[str]) -> None:
    """Run tmi-dbtool from the project root.

    The bootstrap config points the database at localhost:5432 for host-side
    tools, so every call needs the Postgres port-forward to the in-cluster
    StatefulSet. ensure_port_forward hard-fails if an unrelated process holds
    the port rather than silently talking to some other database -- which is
    the failure mode that matters most here, since restoring into the wrong
    database would be worse than not restoring at all.
    """
    ensure_port_forward("postgres")
    run_cmd(argv, cwd=str(get_project_root()))


def cmd_snapshot(args: argparse.Namespace) -> None:
    out = Path(args.file) if args.file else snapshot_path(args.cluster)
    out.parent.mkdir(parents=True, exist_ok=True)

    if out.exists():
        # Keep one generation back. The snapshot is the only copy of settings
        # that exist nowhere else, so an export that fails halfway (or exports
        # an already-wiped database) must not be able to destroy the good one.
        backup = out.with_suffix(out.suffix + ".prev")
        backup.write_bytes(out.read_bytes())
        log_info(f"Previous snapshot kept at {backup.relative_to(get_project_root())}")

    build_dbtool()
    log_info(f"Exporting operational settings from the {args.cluster} dev database...")
    _dbtool([
        "./bin/tmi-dbtool", "--export-config",
        f"--config={args.config}",
        "-f", args.config,
        "--output", str(out),
    ])
    os.chmod(out, 0o600)
    log_success(f"Snapshot written: {out.relative_to(get_project_root())}")


def cmd_restore(args: argparse.Namespace) -> None:
    src = Path(args.file) if args.file else snapshot_path(args.cluster)
    if not src.is_file():
        log_error(
            f"No snapshot at {src}. Take one with:\n"
            f"    make dev-config-snapshot CLUSTER={args.cluster}"
        )
        sys.exit(1)

    build_dbtool()
    mode = "overwriting existing keys" if args.overwrite else "filling in missing keys only"
    log_info(f"Restoring operational settings into the {args.cluster} dev database ({mode})...")

    # --output redirects the *-migrated.yml byproduct that --import-config
    # otherwise drops next to the input file; we delete it afterwards rather
    # than leaving litter in .local/.
    byproduct = src.with_suffix(src.suffix + ".migrated")
    argv = [
        "./bin/tmi-dbtool", "--import-config",
        f"--config={args.config}",
        "-f", str(src),
        "--output", str(byproduct),
    ]
    if args.overwrite:
        argv.append("--overwrite")
    try:
        _dbtool(argv)
    finally:
        byproduct.unlink(missing_ok=True)
    log_success("Settings restored. Roll the server to pick up startup-read values:")
    log_success(f"    kubectl -n tmi-platform rollout restart deploy/tmi-server")


def cmd_status(args: argparse.Namespace) -> None:
    """Report on both durable configuration files, not just the database one.

    A developer debugging "my providers are gone" needs to know which of the two
    halves is missing, and they fail independently: the env file feeds the
    Secret (providers, callback URL), the snapshot feeds the database
    (everything configured through the admin API).
    """
    root = get_project_root()

    snap = Path(args.file) if args.file else snapshot_path(args.cluster)
    if snap.is_file():
        age = datetime.now(timezone.utc) - datetime.fromtimestamp(
            snap.stat().st_mtime, tz=timezone.utc)
        keys = sum(
            1 for line in snap.read_text().splitlines()
            if line.strip() and not line.lstrip().startswith("#") and ":" in line
        )
        log_success(
            f"database snapshot: {snap.relative_to(root)} "
            f"({age.days}d {age.seconds // 3600}h old, ~{keys} keys)"
        )
    else:
        log_warn(
            f"database snapshot: MISSING ({snap.relative_to(root)}) — a dev-nuke "
            f"would lose every setting configured through the admin API"
        )

    env_file = root / OAUTH_PROVIDERS_ENV_FILE
    if env_file.is_file():
        providers = sorted({
            line.split("_")[2]
            for line in env_file.read_text().splitlines()
            if line.startswith("OAUTH_PROVIDERS_") and len(line.split("_")) > 2
        })
        log_success(
            f"env-var settings:  {OAUTH_PROVIDERS_ENV_FILE} "
            f"(providers: {', '.join(p.lower() for p in providers) or 'none'})"
        )
    else:
        log_warn(
            f"env-var settings:  MISSING ({OAUTH_PROVIDERS_ENV_FILE}) — the server "
            f"will advertise only the built-in \"tmi\" provider. Start from "
            f"deployments/k8s/dev/oauth-providers.env.example"
        )


_DISPATCH = {"snapshot": cmd_snapshot, "restore": cmd_restore, "status": cmd_status}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Snapshot and restore the dev cluster's operational configuration.",
    )
    parser.add_argument("verb", choices=sorted(_DISPATCH), help="operation to perform")
    parser.add_argument(
        "--cluster", choices=CLUSTERS, default="docker-desktop",
        help="which dev cluster's snapshot to act on (default: docker-desktop)",
    )
    parser.add_argument(
        "--file", metavar="FILE", default="",
        help=f"override the snapshot path (default: {SNAPSHOT_DIR}/dev-config-<cluster>.yaml)",
    )
    parser.add_argument(
        "--overwrite", action="store_true",
        help="restore: let the snapshot win for keys already present in the database",
    )
    add_config_arg(parser)
    add_verbosity_args(parser)
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    apply_verbosity(args)
    _DISPATCH[args.verb](args)


if __name__ == "__main__":
    main()
