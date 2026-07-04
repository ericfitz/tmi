"""Dev cluster lifecycle helpers: context switching for k3s and docker-desktop.

Pure helpers (local_image_ref, is_local_kube_context) are unit-tested in
scripts/lib/tests/test_cluster.py. Shell wrappers delegate to tmi_common.run_cmd
and are exercised against a live cluster by scripts/devenv.py.
"""
from __future__ import annotations

from tmi_common import (
    check_tool,
    log_info, log_success, run_cmd,
)

# Remote k3s dev target (CLUSTER=k3s). We do not own this cluster: we select
# its context but never create/delete it. Images go to an in-cluster registry
# exposed at rp2:30500 (NodePort 30500).
K3S_CONTEXT = "k3s-rp"
K3S_REGISTRY = "rp2:30500"

# Docker Desktop dev target (CLUSTER=docker-desktop, the default). DD owns the
# cluster lifecycle; we only select its context and never create/delete it.
# Images are imported straight into the node's containerd (no registry):
# docker save <img> | docker exec -i DD_NODE ctr -n k8s.io images import -.
DD_CONTEXT = "docker-desktop"
DD_NODE = "desktop-control-plane"


def registry_for(cluster: str = "docker-desktop") -> str | None:
    """Return the dev image-registry hostname, or None for docker-desktop (no
    registry — images are imported into the node's containerd)."""
    if cluster == "docker-desktop":
        return None
    if cluster == "k3s":
        return K3S_REGISTRY
    raise ValueError(f"Unknown cluster target: {cluster!r}")


def expected_context(cluster: str = "docker-desktop") -> str:
    """Return the kube-context that must be active for the given cluster target."""
    if cluster == "docker-desktop":
        return DD_CONTEXT
    if cluster == "k3s":
        return K3S_CONTEXT
    raise ValueError(f"Unknown cluster target: {cluster!r}")


# Contexts we consider safe to deploy a dev environment into without --yes.
_LOCAL_CONTEXT_PREFIXES = ("kind-", "k3d-")
_LOCAL_CONTEXT_EXACT = {"k3s", "default", "rancher-desktop", "docker-desktop", "minikube"}


def local_image_ref(name: str, tag: str = "dev", *, cluster: str = "docker-desktop") -> str:
    """Return the dev image reference for the cluster: registry-qualified for
    k3s, or a bare name:tag for docker-desktop (imported, not pulled)."""
    reg = registry_for(cluster)
    return f"{name}:{tag}" if reg is None else f"{reg}/{name}:{tag}"


def is_local_kube_context(name: str) -> bool:
    """True if the kubectl context name looks like a local dev cluster."""
    if not name:
        return False
    if name in _LOCAL_CONTEXT_EXACT:
        return True
    return any(name.startswith(p) for p in _LOCAL_CONTEXT_PREFIXES)


def up(cluster: str = "docker-desktop") -> None:
    """Bring up the dev cluster by selecting the appropriate kube context.

    For docker-desktop: select the DD context (DD owns the cluster lifecycle).
    For k3s: select the remote context (the in-cluster registry and workloads
    are applied by deploy; we never create/delete the remote cluster).
    """
    if cluster == "k3s":
        check_tool("kubectl")
        log_info(f"Using existing k3s context '{K3S_CONTEXT}' (no cluster create)")
        run_cmd(["kubectl", "config", "use-context", K3S_CONTEXT])
        log_success(f"kube context set to '{K3S_CONTEXT}'")
        return

    if cluster == "docker-desktop":
        check_tool("kubectl")
        log_info(f"Using Docker Desktop Kubernetes context '{DD_CONTEXT}' (no cluster create)")
        run_cmd(["kubectl", "config", "use-context", DD_CONTEXT])
        log_success(f"kube context set to '{DD_CONTEXT}'")
        return

    raise ValueError(f"Unknown cluster target: {cluster!r}")


def down(cluster: str = "docker-desktop") -> None:
    """No-op for docker-desktop and k3s: we do not own either cluster."""
    if cluster == "k3s":
        log_info("cluster down is a no-op for k3s (remote cluster is not ours to delete)")
        return
    if cluster == "docker-desktop":
        log_info("cluster down is a no-op for docker-desktop (Docker Desktop owns the cluster)")
        return
    raise ValueError(f"Unknown cluster target: {cluster!r}")
