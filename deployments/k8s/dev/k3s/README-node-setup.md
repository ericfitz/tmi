# k3s dev target — one-time host & node setup

`make dev-up CLUSTER=k3s` deploys TMI to the remote **k3s-rp** cluster
(nodes `rp2`/`rp3`/`rp4` = `192.168.1.2`/`.3`/`.4`, all arm64). Images are served
from an in-cluster registry at **`rp2:30500`** (plain HTTP). Two one-time,
out-of-band configuration steps are required before the first `dev-up CLUSTER=k3s`,
because they need root/SSH on machines the dev tooling cannot reach.

## 0. Mac: make `rp2` resolve reliably

The k3s kubeconfig context (`k3s-rp`) and the registry ref (`rp2:30500`) both use
the bare short name `rp2`. On macOS that resolves only via mDNS (`rp2.local`), and
the bare form is unreliable — in particular, kubectl's Go resolver returns
`lookup rp2: no such host` right after the node reboots (mDNS hasn't re-advertised
yet), which aborts `make dev-up CLUSTER=k3s` in pre-flight. Pin it in `/etc/hosts`
so every resolver (kubectl, docker, curl) resolves it deterministically:

```bash
echo "192.168.1.2  rp2" | sudo tee -a /etc/hosts
# verify
dscacheutil -q host -a name rp2   # -> ip_address: 192.168.1.2
```

One-time and persists across reboots. Use `rp2.local` or `192.168.1.2` directly if
you prefer not to edit `/etc/hosts`, but then the kubeconfig server URL and the
registry refs would need to match — pinning `rp2` keeps everything as-is.

## 1. Mac: trust the registry over plain HTTP

The registry has no TLS, so the Docker daemon must treat `rp2:30500` as insecure,
or `docker push` refuses it.

Docker Desktop → **Settings → Docker Engine**, add `rp2:30500` to
`insecure-registries`:

```json
{
  "insecure-registries": ["rp2:30500"]
}
```

**Apply & Restart**. Verify:

```bash
docker info 2>/dev/null | grep -A2 "Insecure Registries"   # should list rp2:30500
```

## 2. Each k3s node: mirror rp2:30500 over HTTP

containerd on every node must also be told the registry is plain HTTP, or pods
fail to pull with an `http: server gave HTTP response to HTTPS client` error.

On **each** node (`rp2`, `rp3`, `rp4`), create/merge
`/etc/rancher/k3s/registries.yaml`. Note the endpoint is the **IP**
`192.168.1.2` (rp2), not the hostname: the nodes do not resolve each other's
bare hostnames (`lookup rp2: no such host`), so the mirror must dial an IP. The
mirror *key* stays `rp2:30500` so image references and the Mac's Docker config
are unchanged.

```yaml
mirrors:
  "rp2:30500":
    endpoint:
      - "http://192.168.1.2:30500"
```

Then restart k3s so containerd reloads:

```bash
# server nodes (this cluster is 3x control-plane):
sudo systemctl restart k3s
```

Verify a node can pull once an image has been pushed (after the first build):

```bash
sudo k3s crictl pull rp2:30500/tmi-server:dev   # should succeed
```

### Optional helper

To push the file to all three nodes at once (requires SSH + sudo on each):

```bash
for n in rp2 rp3 rp4; do
  ssh "$n" 'sudo mkdir -p /etc/rancher/k3s && \
    printf "mirrors:\n  \"rp2:30500\":\n    endpoint:\n      - \"http://192.168.1.2:30500\"\n" | sudo tee /etc/rancher/k3s/registries.yaml >/dev/null && \
    sudo systemctl restart k3s'
done
```

## Notes

- These steps are **idempotent** and only needed once per machine (they persist
  across reboots). They do not affect the default `kind` dev path.
- `rp2` must resolve from the Mac — pin it in `/etc/hosts` (step 0). Relying on
  mDNS alone is what breaks `dev-up` after a node reboot.
