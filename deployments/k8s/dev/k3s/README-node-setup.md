# k3s dev target — one-time host & node setup

`make dev-up CLUSTER=k3s` deploys TMI to a remote k3s cluster you own (all nodes
arm64 in the reference setup). Images are served from an in-cluster registry at
**`<registry>`** (`<node1>:30500`, plain HTTP). Lab-specific values (kube context,
registry, node host) live in the untracked **`.local/k3s.json`**; copy
`deployments/k8s/dev/k3s/k3s.json.example` there and fill it in. Machine-specific
notes (real names/IPs) can go in `.local/k3s-node-setup.md`.

Two one-time, out-of-band configuration steps are required before the first
`dev-up CLUSTER=k3s`, because they need root/SSH on machines the dev tooling
cannot reach.

## 0. Mac: make `<node1>` resolve reliably

The k3s kubeconfig context (`<k3s-context>`) and the registry ref (`<registry>`) may
use a bare short name. On macOS that resolves only via mDNS (`<node1>.local`), and
mDNS is flaky: `dev-up` can fail with `lookup <node1>: no such host` right after the
node reboots. Pin the name in `/etc/hosts`:

```bash
echo "<node1-ip>  <node1>" | sudo tee -a /etc/hosts
dscacheutil -q host -a name <node1>   # -> ip_address: <node1-ip>
```

One-time and persists across reboots. Alternatively use the IP directly, but then
`registry` in `.local/k3s.json` and the kube context server URL must match.

## 1. Mac: allow the plain-HTTP registry in Docker

The registry has no TLS, so the Docker daemon must treat `<registry>` as insecure.
Docker Desktop -> **Settings -> Docker Engine**, add `<registry>` to
`insecure-registries`, then **Apply & Restart**:

```json
{
  "insecure-registries": ["<registry>"]
}
```

Verify:

```bash
docker info 2>/dev/null | grep -A2 "Insecure Registries"   # should list <registry>
```

## 2. Each k3s node: mirror `<registry>` over HTTP

k3s' containerd pulls from the registry over plain HTTP via a mirror. On **each**
node create/merge `/etc/rancher/k3s/registries.yaml`, using **`<node1-ip>`**, not
the hostname: nodes may not resolve each other's bare hostnames, so the mirror must
dial an IP. The mirror *key* stays `<registry>` so image references and the Mac's
Docker config are unchanged:

```yaml
mirrors:
  "<registry>":
    endpoint:
      - "http://<node1-ip>:30500"
```

Then restart the agent (`sudo systemctl restart k3s` on servers,
`k3s-agent` on agents) and verify:

```bash
sudo k3s crictl pull <registry>/tmi-server:dev   # should succeed
```

Optional helper to push the file to every node (requires SSH + sudo on each):

```bash
for n in <node1> <node2> <node3>; do
  ssh "$n" 'sudo mkdir -p /etc/rancher/k3s && \
    printf "mirrors:\n  \"<registry>\":\n    endpoint:\n      - \"http://<node1-ip>:30500\"\n" | sudo tee /etc/rancher/k3s/registries.yaml >/dev/null && \
    sudo systemctl restart k3s'
done
```

## 3. Server configuration

`dev-up` regenerates the `tmi-server-config` ConfigMap from
`config-development.yml` on **every** run (only the Postgres URL host is rewritten
for the pod). Anything patched into the live ConfigMap with `kubectl edit`/`kubectl
patch` is silently discarded on the next deploy, so put bootstrap changes in
`config-development.yml`, not in the cluster.

The ConfigMap carries **bootstrap keys only** (server, database, JWT secret,
logging). Operational settings, including the OAuth callback allowlist that
authorizes browser origins such as `http://<node1>:30081/*`, live in the
`system_settings` table and are read at request time (#419). To authorize a new
origin:

```bash
uv run scripts/set-server-setting.py --help    # PUT /admin/settings/auth.oauth.client_callback_allowlist
```

`make dev-config-snapshot CLUSTER=k3s` saves those DB settings to
`.local/dev-config-k3s.yaml`; teardown takes the snapshot automatically and
`dev-up` restores it, so a `dev-nuke` does not lose the allowlist.

## Notes

- These steps are **idempotent** and only needed once per machine.
- `<node1>` must resolve from the Mac; relying on mDNS alone is what breaks
  `dev-up` after a node reboot.
