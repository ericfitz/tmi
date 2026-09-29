# ADR: Lab and personal deployment details live in untracked `.local/`

- Status: accepted
- Date: 2026-09-29
- Decision maker: **Eric Fitzgerald (human decision, 2026-09-29)**

## Context

The repository is public. Tracked files named the home lab (node hostnames, LAN
IPs, the kube context name) and personal domains. These identify the lab and
do not belong in tracked files.

## Decisions (human, Eric, 2026-09-29)

- **A. NodePorts stay tracked for now.** 30080/30500/30081 are arbitrary port
  numbers, not identifying. Moving them is deferred to issue #996.
- **B. Personal domains are in scope.** `tmi.efitz.net` / `tmiserver.efitz.net`
  are replaced by `example.com`-style placeholders in comments and examples.
  `home.efitz.net` in `scripts/deploy-aws.sh` becomes `api_cidr_host` in the
  untracked `.local/aws-deploy.json`. With neither `--api-cidr` nor that value,
  the script fails with an explicit message rather than silently opening the EKS
  public endpoint. `--api-cidr` behavior is unchanged.
- **C. Dated records stay as-is.** `PROGRESS.md` and `docs/superpowers/**` are
  historical and are not scrubbed.
- **D. tmi-ux does its own scrub** (separate repo, separate change).

## Implementation

- `.local/k3s.json` (`context`, `registry`, `node_host`) is loaded lazily by
  `scripts/lib/cluster.py` `k3s_config()` on `CLUSTER=k3s` paths only; a missing
  or malformed file is a hard error with a `cp` hint. `CLUSTER=k3s` remains the
  public target name. `deployments/k8s/dev/k3s/k3s.json.example` documents the
  schema and is what the unit tests load.
- The tracked k3s overlay uses the placeholder registry
  `k3s-registry.invalid:30500`; `deploy.apply_overlay()` substitutes the real
  registry from `.local/k3s.json` before `kubectl apply`.
- Machine-specific node setup notes live in `.local/k3s-node-setup.md`; the
  tracked README is generic.

## Consequences

- A new machine must create `.local/k3s.json` (and `.local/aws-deploy.json` for
  AWS deploys without `--api-cidr`) before using those targets.
- Git history still contains the old values; no history rewrite is planned.
