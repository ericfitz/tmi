# TMI AWS threat remediation plan

Threat model: "Security Review - TMI Terraform Templates" (`02909291-90f9-4dec-bea0-4309dd8fbc59`, api.tmi.dev), 42 threats.
Prepared 2026-09-27 by tmi-mcp with the tmi agent. The evidence behind each verdict (file:line and live AWS) is in `threat-triage.md`.
Human decisions are recorded in the ADR in ericfitz/tmi PR #967 (`docs/superpowers/specs/2026-09-27-adr-aws-threat-model-acceptances.md`).
Scope: this is a plan only. It changes nothing in TMI (threats or threat model); updating threat statuses is a separate step the user must approve.

Totals: fix 12, accept 12, backlog 1, false positive 3, duplicate 14.

## Fix, in PR order

### PR 1: Terraform-only changes, low runtime risk
- **T372**: delete the unused NodePort SG rules `alb_to_nodes_nodeport` and `nodes_from_alb_nodeport` (`modules/network/aws/main.tf:278-285, 313-320`). Targets are pod IPs.
- **T375**: create a local-only `aws_route_table.database` and re-point the DB subnet associations to it (`net:160-215`).
- **T376** (<- T361): set `deletion_protection = true` and `skip_final_snapshot = false` (`environments/aws-public/main.tf:214-215`). `terraform destroy` then takes two steps.
- **T362**: set `max_allocated_storage = 100`, and add a `FreeStorageSpace` alarm on `aws_sns_topic.security_alerts` (aws-persistent).
- **S3 gateway endpoint** (user-added): add it on `aws_route_table.private` (`net:160`), not on the DB route table. It also saves NAT charges on ECR layer pulls.
- **T366** (<- T348): limit the EKS public endpoint to the user's dynamic-DNS IP (`home.efitz.net`), set at deploy time. This is option 2 and needs the user's confirmation.
  1. `deploy-aws.sh` resolves `home.efitz.net` to a /32. It supports an `--api-cidr` override and warns when the /32 differs from `curl checkip.amazonaws.com`, for deploys away from home.
  2. Before any terraform command or kubectl preflight: `describe-cluster` and compare the CIDRs. If they differ, run `aws eks update-cluster-config --resources-vpc-config publicAccessCidrs=<ip>/32` and then `aws eks wait cluster-active`. Skip the update when nothing changed, because EKS rejects no-op updates.
  3. First deploy (no cluster yet): pass the /32 into `public_access_cidrs` (aws-public doesn't set it today; the module defaults to 0.0.0.0/0). Add `lifecycle { ignore_changes = [vpc_config[0].public_access_cidrs] }` so Terraform doesn't fight the script.
  4. The IP stays allowed after the deploy by default so kubectl works from home. An optional `--close-api` removes it.
  5. No CI path touches EKS.

### PR 2: logging module
- **T381** (<- T352): pin Fluent Bit to `public.ecr.aws/aws-observability/aws-for-fluent-bit:<version>`, optionally with `@sha256` (`modules/logging/aws/main.tf:258`).
- **T382**: give the DaemonSet a securityContext with `allowPrivilegeEscalation=false`, drop ALL capabilities, a read-only root filesystem, and seccomp `RuntimeDefault`. It stays root, because it has to read `/var/log`.
- Verify that logs still arrive in `/tmi/tmi`.

### PR 3: node egress
- **T374** (<- T358), partial: limit `nodes_dns_tcp/udp` to `var.vpc_cidr` (`net:369-386`). Port 80 egress is accepted (user decision).
- Verify with a node roll and an image pull.

### PR 4: NetworkPolicies (overlay; test on k3s first)
- **T373** (<- T357): default-deny ingress in `tmi-platform`, then allow:
  - tmi-server 8080
  - redis 6379
  - NATS 4222/8222, from the server, controller, extractor, chunk-embed, and KEDA
- This also covers the NetworkPolicy part of **T356** and **T388**.
- The node SG self-rule stays as the EKS baseline.

### PR 5: HTTPS from the ALB to the pods, plus HSTS (user-added)
- **T371** (<- T355): self-signed cert on the pod, `TMI_SERVER_TLS_ENABLED=true`, and the ingress annotations `backend-protocol: HTTPS` and `healthcheck-protocol: HTTPS`.
  - First check for in-cluster callers of `http://tmi-server:8080` (workers, tmi-tf-wh, addons). They break once the server listens on TLS only.
  - The redirect logic and auth base-URL logic already trust `X-Forwarded-Proto`.
- **HSTS** (T370 follow-up): no code needed. `HSTSMiddleware` emits the header once `TLSEnabled` is set.

### PR 6: in-cluster TLS for Redis and NATS (user-added; needs Go changes)
- **T388** and **T356** (TLS part).
- CA: a Terraform `tls_private_key` / `tls_self_signed_cert` CA, stored as a Secret. There's no cert-manager, and the CA key lives in encrypted state like the other secrets.
- Redis: `auth/db/redis.go:76` needs a TLSConfig plus `tls_enabled` and a CA-file setting. Check the worker components that also use Redis.
- NATS: the `nats.Connect` calls in `internal/worker/nats.go:55`, `cmd/worker-probe`, and `internal/platform/controller/jetstream_provisioner.go:44` need `nats.RootCAs(path)`. A lazier alternative is to mount the CA and set `SSL_CERT_FILE`.
- Add manual CA rotation to the runbook in #965.

## Backlog
- **T379** (<- T364), secret rotation: ericfitz/tmi#965, in Backlog with labels security and size:L.
- **Proposed new issue** (the user's approval is needed to file it): security alarms. Add metric filters and alarms in aws-persistent that alert through the existing `tmi-security-alerts` topic, which emails security@tmi.dev. Cost is about $0.10 per alarm per month. They cover:
  - CloudTrail, CIS-style: root use, IAM/SG/NACL changes, console auth failures, KMS and CloudTrail changes.
  - EKS audit: `pods/exec`, and spikes in anonymous or forbidden requests.
  - Server auth-failure spikes. For now this is a filter on the WARN `"Authentication failed"` log text in `/tmi/tmi`. Optionally, a small server change adds a stable `event=auth_failure` field.

## Accepted
| T# | Reason |
|---|---|
| T383 (<- T349) | Public demo that is deliberately ungated; everyone is a reviewer (not admin). TMI is open source, and its threat models are public and restorable from JSON exports. |
| T350 | The service runs schema migrations. Elevated DB credentials were judged the lesser evil compared with giving operators remote DB access. |
| T369 (<- T360) | No WAF, for cost reasons. We rely on CATS fuzzing. |
| T385 | Hard accept: 30-day retention is a cost decision, and GuardDuty isn't wanted either (cost). |
| T387 (<- T363) | A single NAT is the cost design. The EIP is protected. |
| T377 | Single-AZ RDS is the cost design. The storage alarm lands with T362. |
| T380 (<- T351, T365) | Mutable ECR tags are deliberate. Deploys pin the git SHA. |
| T386 | `allow_overwrite` on ACM validation records is the documented AWS pattern. |
| T368 | AWS already envelope-encrypts EKS secrets; a customer-managed key adds little. |
| T378 | Secrets in Terraform state are inherent to the design; the state is encrypted. |
| T359 | The Load Balancer Controller IAM policy is upstream verbatim. |
| T389 | The Helm chart is version-pinned, and a vendored-chart option exists. |

## False positives (already handled)
- **T367** (<- T353): audit and authenticator control-plane logs are enabled.
- **T370** (<- T354): port 80 only redirects to 443. HSTS lands in PR 5.
- **T384**: VPC Flow Logs are active.
