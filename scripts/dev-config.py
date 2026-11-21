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

(3) is how a dev cluster silently loses every OAuth provider and
feature flag that was ever configured through the admin API (#791). This script
is the missing durable copy for that third bucket: `snapshot` writes the
database's explicit settings to a gitignored file under .local/, and `restore`
puts them back.

The safety net is automatic, not a discipline you have to remember:
scripts/lib/deploy.py snapshots before every teardown (including the namespace
delete that dev-nuke performs) and restores after every start. So the sequence
that used to destroy the configuration now round-trips it.

SNAPSHOT FORMAT AND RESTORE SEMANTICS (#1180)

`snapshot` runs `tmi-dbtool --export-config --explicit-only`: it writes only the
rows an operator deliberately set (origin "explicit") and puts a format marker on
the first line. Seeded rows are not snapshotted; the server re-seeds registry
defaults on every start.

`restore` runs `tmi-dbtool --restore-snapshot`, in one transaction:

  - writes exactly the keys in the file, each as an explicit row (struct
    defaults and the host environment never contribute a row or a value);
  - replaces an existing seeded row (so an admin edit to a key that has a
    registry default survives a nuke);
  - skips an existing explicit row, so restore never reverts an edit made
    since the snapshot, and a second run is a no-op;
  - refuses a file without the marker (a snapshot taken before this format
    holds every row and would stamp seeded defaults explicit). The deploy
    treats the refusal as a warning; re-take the snapshot with
    `make dev-config-snapshot CLUSTER=<cluster>`;
  - treats an empty snapshot (a database with no explicit rows) as nothing to do.

A manifest pin (TMI_* in the deployment) therefore loses only to a real admin
edit, which is what the settings-precedence ADR specifies.

COVERAGE: WHAT RESTORE DOES NOT COVER

Restore writes the keys that `Config.GetMigratableSettings()` knows about. A
handful of keys live in the classification registry (so the server seeds them)
without a matching Config struct field, so `--export-config` writes them out but
restore ignores them (it logs their names):

    features.webhooks_enabled       upload.max_file_size_mb
    features.websocket_enabled      websocket.max_participants
    session.timeout_minutes         ui.default_theme

What does not survive a nuke is a CUSTOMIZED value for one of them; see the
follow-up issue linked from #791. Everything with a struct field round-trips.

USAGE

    uv run scripts/dev-config.py snapshot --cluster k3s
    uv run scripts/dev-config.py restore  --cluster k3s
    uv run scripts/dev-config.py status   --cluster k3s

or via the make targets: dev-config-snapshot / dev-config-restore /
dev-config-status, each honoring CLUSTER=.
"""
from __future__ import annotations

import argparse
import base64
import contextlib
import os
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from deploy import (  # noqa: E402
    NS, OAUTH_PROVIDERS_ENV_FILE, ensure_port_forward, kubectl, set_active_context,
)
from tmi_common import (  # noqa: E402
    add_config_arg, add_verbosity_args, apply_verbosity, get_project_root,
    log_error, log_info, log_success, log_warn, run_cmd,
)

# Per-cluster because the two dev clusters legitimately disagree on
# host-dependent database values, and a single shared file would push the
# wrong one to whichever cluster it was not taken from. (The OAuth callback URL
# is no longer one of them: it is startup-only, set by TMI_OAUTH_CALLBACK_URL.)
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


# Secret/tmi-secrets key -> logical secret name the Go encryptor asks its
# provider for (internal/secrets/provider.go SecretKeys, read by
# internal/crypto/settings_encryptor.go NewSettingsEncryptor). The FILE
# provider reads <dir>/<logical name>. The first entry is required for any
# decryption; the rest are optional.
SETTINGS_KEY_SECRETS = (
    ("TMI_SECRET_SETTINGS_ENCRYPTION_KEY", "settings_encryption_key"),
    ("TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID", "settings_encryption_context_id"),
    ("TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY", "settings_encryption_previous_key"),
    ("TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID", "settings_encryption_previous_context_id"),
)


@contextlib.contextmanager
def settings_key_env():
    """Yield env vars that point dbtool's FILE secrets provider at a private
    temp dir holding the cluster's settings-encryption key, or None when
    Secret/tmi-secrets lacks the key (fresh cluster, oracle).

    The server encrypts every setting at rest, and the host-side dbtool cannot
    decrypt without the key, which lives only in the cluster Secret (#1033).
    Values travel kubectl stdout -> memory -> 0600 file; they never reach
    argv, the environment, or logs. The only env vars set (provider name and
    directory) hold no secret. The directory is removed on exit.
    """
    old_umask = os.umask(0o077)
    try:
        with tempfile.TemporaryDirectory(prefix="tmi-dev-secrets-") as tmp:
            os.chmod(tmp, 0o700)
            written = set()
            for secret_key, name in SETTINGS_KEY_SECRETS:
                out = kubectl(
                    ["-n", NS, "get", "secret", "tmi-secrets", "-o",
                     f"jsonpath={{.data.{secret_key}}}"],
                    check=False, capture=True,
                )
                b64 = (out.stdout or "").strip() if out.returncode == 0 else ""
                if not b64:
                    continue
                fd = os.open(os.path.join(tmp, name), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(fd, "wb") as fh:
                    fh.write(base64.b64decode(b64))
                written.add(secret_key)
            if "TMI_SECRET_SETTINGS_ENCRYPTION_KEY" not in written:
                log_warn(
                    "Secret/tmi-secrets has no TMI_SECRET_SETTINGS_ENCRYPTION_KEY; "
                    "encrypted settings cannot be decrypted and will be skipped from the snapshot"
                )
                yield None
                return
            yield {"TMI_SECRETS_PROVIDER": "file", "TMI_SECRETS_FILE_DIR": tmp}
    finally:
        os.umask(old_umask)


def _dbtool(argv: list[str], cluster: str, *, with_settings_key: bool = False) -> None:
    """Run tmi-dbtool from the project root.

    The bootstrap config points the database at localhost:5432 for host-side
    tools, so every call needs the Postgres port-forward to the in-cluster
    StatefulSet. ensure_port_forward hard-fails if an unrelated process holds
    the port rather than silently talking to some other database -- which is
    the failure mode that matters most here, since restoring into the wrong
    database would be worse than not restoring at all.

    Pins the active kube context to `cluster` first (#955): this runs as its
    own `uv run` subprocess (invoked by deploy.py's snapshot_dev_config /
    restore_dev_config), so it has no context pinned yet, and ensure_port_forward
    must target the cluster this snapshot/restore is actually for, not
    whatever the ambient kubeconfig happens to have selected.

    with_settings_key (export only): give dbtool the cluster's settings key
    via settings_key_env() so it can decrypt at-rest-encrypted settings. Restore
    does not need it: import writes with the key if present but reads only
    plaintext snapshots.
    """
    set_active_context(cluster)
    ensure_port_forward("postgres")
    if with_settings_key:
        with settings_key_env() as env:
            run_cmd(argv, cwd=str(get_project_root()), env=env)
    else:
        run_cmd(argv, cwd=str(get_project_root()))


SKIPPED_PREFIX = "# skipped-explicit: "  # keep in step with cmd/dbtool skippedExplicitPrefix


def snapshot_keys(path: Path) -> set[str] | None:
    """Dotted leaf keys of a snapshot file (empty set for an empty or
    marker-only file), or None when it cannot be read or parsed."""
    try:
        doc = yaml.safe_load(path.read_text())
    except (OSError, yaml.YAMLError):
        return None
    if doc is None:
        return set()
    if not isinstance(doc, dict):
        return None

    def walk(node, prefix):
        if isinstance(node, dict):
            for k, v in node.items():
                yield from walk(v, f"{prefix}.{k}" if prefix else str(k))
        else:
            yield prefix

    return set(walk(doc, ""))


def snapshot_skipped(path: Path) -> list[str]:
    """Explicit rows dbtool reported as not exported (header comment), names only."""
    try:
        for line in path.read_text().splitlines():
            if line.startswith(SKIPPED_PREFIX):
                return [k.strip() for k in line[len(SKIPPED_PREFIX):].split(",") if k.strip()]
    except OSError:
        pass
    return []


def _write_private(path: Path, data: bytes) -> None:
    """Replace path with data, mode 0600 from creation: write a private temp
    file (mkstemp is O_EXCL, 0600) and rename it over path, so an old
    world-readable file is never truncated and refilled with secrets."""
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=path.name + ".tmp-")
    try:
        with os.fdopen(fd, "wb") as fh:
            fh.write(data)
        os.replace(tmp, path)
    except BaseException:
        Path(tmp).unlink(missing_ok=True)
        raise


def _loses_to_skips(existing: set[str], new_keys: set[str], skipped: list[str]) -> bool:
    """True when replacing `existing` with `new_keys` would lose data the export
    only failed to read: the export is empty while the file has keys, or a key
    that disappeared is one dbtool reported as skipped (undecryptable, empty).
    A key that vanished with no skip entry was deleted by an admin, and the
    snapshot must follow, or deleted rows would resurrect on restore."""
    if not new_keys:
        return True
    return bool((existing - new_keys) & set(skipped))


def cmd_snapshot(args: argparse.Namespace) -> None:
    out = Path(args.file) if args.file else snapshot_path(args.cluster)
    out.parent.mkdir(parents=True, exist_ok=True)
    root = get_project_root()

    # Export to a sibling temp file and only then replace the good snapshot.
    # The snapshot is the only copy of settings that exist nowhere else, so a
    # failed export, an export of a database with no explicit rows (a freshly
    # nuked one whose restore was refused or failed), or a PARTIAL export (an
    # explicit secret that could not be decrypted) must not destroy it or its
    # .prev generation.
    tmp = out.with_name(out.name + ".new")
    for stale in (*out.parent.glob(out.name + ".new*"), *out.parent.glob(out.name + ".prev.tmp-*")):
        stale.unlink(missing_ok=True)

    build_dbtool()
    log_info(f"Exporting explicit settings from the {args.cluster} dev database...")
    try:
        # --allow-skips: an explicit row dbtool cannot export (a secret with no
        # decryptor, an empty value) must not fail the best-effort snapshot that
        # runs before every teardown; dbtool names the keys in the file header
        # and in a warning instead, and the subset check below protects the
        # existing snapshot.
        _dbtool([
            "./bin/tmi-dbtool", "--export-config", "--explicit-only", "--allow-skips",
            f"--config={args.config}",
            "-f", args.config,
            "--output", str(tmp),
        ], cluster=args.cluster, with_settings_key=True)

        new_keys = snapshot_keys(tmp)
        if new_keys is None:
            raise RuntimeError(f"dbtool wrote an unparseable snapshot to {tmp}")
        skipped = snapshot_skipped(tmp)

        rotate = out.exists()
        if rotate:
            existing = snapshot_keys(out)
            if existing is None:
                # Never let a corrupt file displace the good .prev.
                stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
                corrupt = out.with_suffix(out.suffix + f".corrupt-{stamp}")
                os.replace(out, corrupt)
                log_warn(
                    f"{out.relative_to(root)} is unreadable; moved aside to "
                    f"{corrupt.relative_to(root)} and left .prev untouched."
                )
                rotate = False
            elif not args.force and existing and _loses_to_skips(existing, new_keys, skipped):
                lost = sorted((existing - new_keys) if new_keys else existing)
                log_warn(
                    f"The new export lacks {len(lost)} key(s) the existing snapshot "
                    f"{out.relative_to(root)} has and the loss is not an admin deletion "
                    f"(empty export or skipped rows): {', '.join(lost)}. Keeping the existing "
                    "snapshot and its .prev unchanged. Re-run with --force "
                    "(make dev-config-snapshot FORCE=1) to accept the new export."
                )
                return

        if rotate:
            # Keep one generation back.
            backup = out.with_suffix(out.suffix + ".prev")
            _write_private(backup, out.read_bytes())
            log_info(f"Previous snapshot kept at {backup.relative_to(root)}")
        os.chmod(tmp, 0o600)
        os.replace(tmp, out)
    finally:
        tmp.unlink(missing_ok=True)
    note = f"; {len(skipped)} explicit rows skipped: {', '.join(skipped)}" if skipped else ""
    msg = f"Snapshot written: {out.relative_to(root)} ({len(new_keys)} keys{note})"
    (log_warn if skipped else log_success)(msg)


def cmd_restore(args: argparse.Namespace) -> None:
    src = Path(args.file) if args.file else snapshot_path(args.cluster)
    if not src.is_file():
        log_error(
            f"No snapshot at {src}. Take one with:\n"
            f"    make dev-config-snapshot CLUSTER={args.cluster}"
        )
        sys.exit(1)

    build_dbtool()
    log_info(
        f"Restoring operational settings into the {args.cluster} dev database "
        "(snapshot keys as explicit; existing explicit rows kept)..."
    )
    _dbtool([
        "./bin/tmi-dbtool", "--restore-snapshot",
        f"--config={args.config}",
        "-f", str(src),
    ], cluster=args.cluster)
    log_success("Settings restored. Roll the server to pick up startup-read values:")
    log_success("    kubectl -n tmi-platform rollout restart deploy/tmi-server")


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
        "--force", action="store_true",
        help="snapshot: accept an export that lacks keys the existing snapshot has "
             "(empty export or skipped rows) instead of keeping the existing file",
    )
    parser.add_argument(
        "--cluster", choices=CLUSTERS, default="docker-desktop",
        help="which dev cluster's snapshot to act on (default: docker-desktop)",
    )
    parser.add_argument(
        "--file", metavar="FILE", default="",
        help=f"override the snapshot path (default: {SNAPSHOT_DIR}/dev-config-<cluster>.yaml)",
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
