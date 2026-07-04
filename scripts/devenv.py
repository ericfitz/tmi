# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Single orchestrator for the TMI dev environment.

Verbs (the make targets are 1:1 thin wrappers):
  up       bring up the cluster context, build+push images, deploy, wait
  down     tear down the in-cluster stack; KEEP db data
  restart  rebuild server image + roll the server pod (cluster + db untouched)
  reset    soft known-state: redeploy the stack with fresh images; KEEP db data
  nuke     hard known-state: destroy everything incl. db data + images, rebuild
  status   cluster-aware status dashboard
  deploy   (re)apply manifests + rollout without recreating cluster/db
  logs     stream the tmi-server pod logs
  cluster  up|down the cluster target

Global: --db postgres|oracle (default postgres), --cluster docker-desktop|k3s
        (default docker-desktop), --yes
"""
from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import cluster      # noqa: E402
import deploy       # noqa: E402
import devstatus    # noqa: E402
from tmi_common import (  # noqa: E402
    add_verbosity_args, apply_verbosity, log_info, log_success,
)

VERBS = ["up", "down", "restart", "reset", "nuke",
         "status", "deploy", "logs", "cluster"]


def cmd_up(args) -> None:
    cluster.up(cluster=args.cluster)
    deploy.start(db=args.db, cluster_target=args.cluster,
                 skip_context_guard=args.yes)


def cmd_down(args) -> None:
    deploy.teardown(db=args.db)
    cluster.down(cluster=args.cluster)
    log_success("dev environment down (db data preserved)")


def cmd_restart(args) -> None:
    deploy.restart(db=args.db, cluster_target=args.cluster,
                   skip_context_guard=args.yes)


def cmd_reset(args) -> None:
    log_info("dev-reset: redeploying the in-cluster stack (keeping cluster + db data)")
    deploy.teardown(db=args.db)
    deploy.start(db=args.db, cluster_target=args.cluster,
                 skip_context_guard=args.yes)
    log_success("dev-reset complete")


def cmd_nuke(args) -> None:
    log_info("dev-nuke: destroying EVERYTHING incl. db data + built images")
    # Namespace-scoped hard reset: we don't own these clusters, so wipe the
    # tmi-platform namespace (workloads + in-cluster Postgres data) and redeploy.
    cluster.up(cluster=args.cluster)      # ensure the right context is active
    deploy.teardown_namespace()
    deploy.remove_local_images(args.db, cluster_target=args.cluster)
    _clean_logs_and_files()
    deploy.start(db=args.db, cluster_target=args.cluster,
                 skip_context_guard=args.yes)
    log_success(f"dev-nuke complete (fresh {args.cluster} environment up)")


def _clean_logs_and_files() -> None:
    scripts_dir = Path(__file__).resolve().parent
    from tmi_common import run_cmd
    run_cmd(["uv", "run", str(scripts_dir / "clean.py"), "files"], check=False)


def cmd_status(args) -> None:
    devstatus.print_dashboard()


def cmd_deploy(args) -> None:
    deploy.start(db=args.db, cluster_target=args.cluster,
                 skip_context_guard=args.yes)


def cmd_logs(args) -> None:
    deploy.tail_server_logs()


def cmd_cluster(args) -> None:
    {"up": cluster.up, "down": cluster.down}[args.action](cluster=args.cluster)


_DISPATCH = {
    "up": cmd_up, "down": cmd_down, "restart": cmd_restart, "reset": cmd_reset,
    "nuke": cmd_nuke, "status": cmd_status, "deploy": cmd_deploy, "logs": cmd_logs,
    "cluster": cmd_cluster,
}


def _add_global_options(
    parser: argparse.ArgumentParser,
    *,
    is_subparser: bool = False,
) -> None:
    """Add --db/--cluster/--yes to a parser (top-level or subparser).

    Defined as a helper so the exact same options are added to both the
    top-level parser and each subparser, enabling both orderings:
      devenv.py --db oracle up   (global before verb — Makefile style)
      devenv.py up --db oracle   (global after verb — test/interactive style)

    When ``is_subparser=True`` the option defaults use SUPPRESS rather than a
    concrete value.  This means a subparser that sees no --db on its own
    portion of argv will not override the value the top-level parser already
    set from the pre-verb segment of argv.  The top-level parser always
    supplies the true default ("postgres") so it is always present.
    """
    if is_subparser:
        parser.add_argument("--db", choices=["postgres", "oracle"],
                            default=argparse.SUPPRESS)
        parser.add_argument("--cluster", choices=["k3s", "docker-desktop"],
                            default=argparse.SUPPRESS)
        parser.add_argument("--yes", action="store_true", default=argparse.SUPPRESS,
                            help="Skip the local-kube-context safety check")
    else:
        parser.add_argument("--db", choices=["postgres", "oracle"], default="postgres")
        parser.add_argument("--cluster", choices=["k3s", "docker-desktop"], default="docker-desktop",
                            help="Kube cluster target: docker-desktop (default) or k3s (remote)")
        parser.add_argument("--yes", action="store_true",
                            help="Skip the local-kube-context safety check")


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="TMI dev environment orchestrator.")
    add_verbosity_args(p)
    _add_global_options(p)
    sub = p.add_subparsers(dest="verb", required=True)
    for v in VERBS:
        sp = sub.add_parser(v)
        _add_global_options(sp, is_subparser=True)
        if v == "cluster":
            sp.add_argument("action", choices=["up", "down"])
    return p


def main() -> None:
    args = build_parser().parse_args()
    apply_verbosity(args)
    _DISPATCH[args.verb](args)


if __name__ == "__main__":
    main()
