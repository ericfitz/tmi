# ADR: Escrow the rotated settings-encryption key to AWS Secrets Manager

- Status: Accepted
- Date: 2026-10-01
- Decision maker: Eric Fitzgerald (human decision)
- Issue: #1009 (follow-up to #965 PR 1, #1011)

## Context

#965 PR 1 made the `tmi-secrets` Kubernetes Secret the only live home of the settings-encryption key, and removed the Secrets Manager copy. After the first rotation, the key would exist only in the cluster. Losing the namespace or the Secret would make every `ENC:` setting row permanently unreadable.

## Options considered

- A: escrow to Secrets Manager after each rotation.
- B: a separate protected in-cluster Secret. It doesn't survive cluster or namespace loss.
- C: accept the risk.
- D: keep deferring rotation.

## Decision

**A.** On AWS the rotator escrows the current and previous settings key and its id to a Terraform-created Secrets Manager secret before it promotes a new key. It authenticates through an IRSA role scoped to `secretsmanager:PutSecretValue` on that one ARN.

**Escrow location (human decision, Eric Fitzgerald, 2026-10-02):** the escrow secret lives in `aws-persistent` with `prevent_destroy`, not in `aws-public`, so a deployment destroy cannot delete it.

**IRSA wiring (human decision, Eric Fitzgerald, 2026-10-02):** follow the tmi-api precedent. Terraform creates the IRSA ServiceAccount `tmi-rotator-aws` and the ConfigMap `tmi-rotator-config` (escrow ARN). The aws overlay switches the CronJob to that SA and adds it to the `tmi-rotator` RoleBinding. Rejected: a deploy-script placeholder annotation on the base SA.

**Payload format:** the secret is named `tmi-settings-key-escrow` and holds JSON `{"rotation":"settings-key","escrowed_at":"<time>","current":{"id","key_hex"},"previous":{"id","key_hex"}}`. The roles are the post-promotion roles: `current` is the staged key and id (about to become current), `previous` is today's current. The rotator escrows in the `staged` phase, after the server rollout and pair-consistency check and strictly before the staged-to-promoted transition. A rerun after a failure escrows again, which only adds a version. The rotator owns the secret's versions; Terraform creates no secret version.

**Deploy ordering:** apply `terraform/environments/aws-persistent` before `aws-public`. `aws-public` reads the secret through a data lookup by name and fails if it does not exist, and `scripts/deploy-aws.sh` never touches `aws-persistent`.

## Consequences

- If the escrow write fails, the rotation does not promote and stays in `staged` (fails closed), so the stale-rotation alarm fires.
- Escrow is a no-op when no escrow ARN is configured, as on the docker-desktop and k3s dev clusters.
- A restore runbook lives on the wiki Secret-Rotation page.
- The JWT keyring (#965 PR 2) reuses the mechanism.
- Until escrow ships, the AWS `tmi-secrets` Secret carries a `tmi.dev/rotated-at.settings-key` annotation set to the key's creation date (2026-07-26). That defers the first rotation to about 2026-10-24. Remove the annotation once escrow is live.
