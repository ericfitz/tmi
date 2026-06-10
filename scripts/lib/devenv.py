"""Pure helpers + thin shell wrappers for the cluster-based dev environment.

Pure functions are unit-tested in scripts/lib/tests/test_devenv.py.  Shell
wrappers delegate to tmi_common.run_cmd and are exercised by start-dev.py /
dev-cluster.py against a real cluster.
"""

from __future__ import annotations

import hashlib
import subprocess

from tmi_common import run_cmd

LOCAL_REGISTRY = "localhost:5000"
REGISTRY_CONTAINER = "tmi-dev-registry"
PLATFORM_NAMESPACE = "tmi-platform"

# Contexts we consider safe to deploy a dev environment into without --yes.
_LOCAL_CONTEXT_PREFIXES = ("kind-", "k3d-")
_LOCAL_CONTEXT_EXACT = {"k3s", "default", "rancher-desktop", "docker-desktop", "minikube"}


def local_image_ref(name: str, tag: str = "dev", registry: str = LOCAL_REGISTRY) -> str:
    """Return the fully-qualified local-registry image reference."""
    return f"{registry}/{name}:{tag}"


def is_local_kube_context(name: str) -> bool:
    """True if the kubectl context name looks like a local dev cluster."""
    if not name:
        return False
    if name in _LOCAL_CONTEXT_EXACT:
        return True
    return any(name.startswith(p) for p in _LOCAL_CONTEXT_PREFIXES)


def content_hash(text: str) -> str:
    """Stable 12-char hex digest of text (for config-change annotations)."""
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:12]


def render_configmap_yaml(*, name: str, namespace: str, file_key: str, content: str) -> str:
    """Render a ConfigMap manifest embedding `content` under `file_key`.

    Uses a block scalar with 4-space indentation; annotates the content hash.
    """
    # name/namespace/file_key are dev-internal identifiers, not user input — not escaped.
    indented = "\n".join("    " + line for line in content.splitlines())
    return (
        "apiVersion: v1\n"
        "kind: ConfigMap\n"
        "metadata:\n"
        f"  name: {name}\n"
        f"  namespace: {namespace}\n"
        "  annotations:\n"
        f"    tmi.dev/config-hash: \"{content_hash(content)}\"\n"
        "data:\n"
        f"  {file_key}: |\n"
        f"{indented}\n"
    )


# --- shell wrappers (not unit-tested; exercised against a live cluster) ---

def current_kube_context() -> str:
    """Return the active kubectl context name (empty string if none)."""
    try:
        out = subprocess.run(
            ["kubectl", "config", "current-context"],
            capture_output=True, text=True, check=True,
        )
        return out.stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""


def kubectl(args: list[str], *, check: bool = True, input_text: str | None = None):
    """Run kubectl with the given args."""
    return run_cmd(["kubectl", *args], check=check, input_text=input_text)
