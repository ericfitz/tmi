# TMI AWS Terraform threat triage (proposal only, 2026-09-27)

Evidence: repo at HEAD a7d1b052 plus read-only `AWS_PROFILE=tmi` describes (EKS, RDS, SGs, ELBv2, WAF, ECR, Secrets Manager, flow logs). Paths are relative to `terraform/` unless noted; `pub` = `environments/aws-public/main.tf`, `net` = `modules/network/aws/main.tf`, `k8s` = `modules/kubernetes/aws/main.tf`, `log` = `modules/logging/aws/main.tf`, `ovl` = `deployments/k8s/dev/aws`.

Counts: fix 11, accept 14, false-positive 3, dup 14.

## 1. Per-threat table

| T# | Verdict | Change | Effort | Notes |
|---|---|---|---|---|
| T348 | dup-of-T366 | | | Same public-endpoint finding. |
| T349 | dup-of-T383 | | | |
| T350 | accept | (`modules/database/aws`, app) | L | Correct: `k8s_resources.tf:197` builds the URL from the RDS master user. A least-privilege role needs an in-VPC bootstrap step (RDS is not reachable from the deployer; `postgresql` provider unusable) plus ongoing GRANT upkeep for GORM AutoMigrate. Not worth it for single-tenant demo. |
| T351 | dup-of-T380 | | | Misstates deploys: app images use an immutable per-deploy git-SHA tag (`scripts/deploy-aws.sh:612-645`), only the build also pushes `:latest` (`container_build_helpers.py:565`). |
| T352 | dup-of-T381 | | | |
| T353 | dup-of-T367 | | | |
| T354 | dup-of-T370 | | | |
| T355 | dup-of-T371 | | | |
| T356 | fix | `deployments/k8s/platform/nats.yml` + `ovl` netpol | S | Correct: `nats.conf` has no `authorization`/`tls` (nats.yml:12-17). Cheapest real gain is a NetworkPolicy limiting 4222 to labelled TMI pods (see T373); NATS token auth via `TMI_NATS_CREDS` (process_env.go:60) is a follow-up. TLS: accept. |
| T357 | dup-of-T373 | | | |
| T358 | dup-of-T374 | | | |
| T359 | accept | `k8s:376-632` | - | Policy is the upstream AWS LB controller `iam_policy.json` verbatim; tag conditions already present on mutating ELB/SG actions (k8s:505-508, 545-549, 581-585). Tightening beyond upstream breaks the controller on upgrades. Single-account, single-cluster blast radius. |
| T360 | dup-of-T369 | | | |
| T361 | dup-of-T376 | | | multi_az part accepted (see T377). |
| T362 | fix | `pub:204-224` | S | Correct: `max_allocated_storage` defaults to 0 (`database/aws/variables.tf:27-30`), live `maxAlloc=null`. Pass `max_allocated_storage = 100`; add a `FreeStorageSpace` alarm to the existing SNS topic in `aws-persistent` (or `pub`). |
| T363 | dup-of-T387 | | | |
| T364 | dup-of-T379 | | | |
| T365 | dup-of-T380 | | | force_delete part. |
| T366 | fix | `pub:246-326`, `environments/aws-public/variables.tf`, `scripts/deploy-aws.sh` | S | Correct: `public_access_cidrs` default `["0.0.0.0/0"]` (`k8s/variables.tf:44-47`), live confirms. Fully private is not feasible (the `kubernetes`/`helm` providers run from the deployer laptop, `pub:71-91`). Add an `eks_public_access_cidrs` var and have `deploy-aws.sh` write the deployer's current `/32` into the generated `terraform.tfvars`. |
| T367 | false-positive | | - | `pub:256` sets `cluster_log_types = ["audit","authenticator"]`; `k8s:67-79` creates the log group and wires `enabled_cluster_log_types`; live EKS shows both enabled. `api`/`controllerManager`/`scheduler` add CloudWatch ingest cost for little security value: accept. |
| T368 | accept | `k8s:74-95` | S | Correct that `encryption_config` is absent (live `enc: null`). EKS 1.28+ already envelope-encrypts etcd with an AWS-owned key; a CMK adds ~$1/mo and key-policy upkeep for marginal gain. Cheap to add later if wanted. |
| T369 | fix | new `aws_wafv2_web_acl` in `pub`; annotation `alb.ingress.kubernetes.io/wafv2-acl-arn` in `ovl/ingress.yml` (placeholder filled by deploy-aws.sh) | M | Correct: `wafv2 get-web-acl-for-resource` returns none for both ALBs. A rate-based rule (~2000 req/5 min/IP) plus `AWSManagedRulesCommonRuleSet` costs ~$8-10/mo. Threat is wrong that access logs are missing: `ovl/ingress.yml:19` enables them to the persistent log bucket (live confirms). Shield Standard is automatic. |
| T370 | false-positive | | - | Port 80 is redirect-only: `ovl/ingress.yml:20-21` (`ssl-redirect: "443"`), live listener 80 = `redirect -> 443`. HSTS exists but only when the server itself terminates TLS (`api/middleware.go:93-99`); optional S follow-up: emit HSTS behind the ALB (app change, not Terraform). Keep `net:248` so the redirect keeps working. |
| T371 | accept | | - | Correct (target group `HTTP:8080`, type `ip`). Single-tenant VPC, no other workloads on the nodes; re-encryption needs cert-manager or a mesh. Cost/complexity not justified. |
| T372 | fix | `net:278-285`, `net:313-320` | S | Correct and unused: `ovl/ingress.yml:17` sets `target-type: ip` and both live target groups are `ip`, so the 30000-32767 rules serve nothing. Delete `alb_to_nodes_nodeport` and `nodes_from_alb_nodeport`. |
| T373 | fix (netpol) / accept (SG) | `ovl/` new `networkpolicy-*.yml` | M | The `-1` self rule (`net:323-366`) is the EKS-documented node SG baseline (CNI, kubelet, webhooks); per-port SG rules break upgrades and are not the right layer. Real fix is pod-level: `enableNetworkPolicy=true` is already on (`k8s:277-279`) and one policy exists (`ovl/networkpolicy-redis.yml`). Add default-deny ingress for `tmi-platform` plus allows for tmi-server 8080 (from ALB/any), redis 6379, nats 4222/8222 (server, controller, extractor, chunk-embed, KEDA in its namespace). Test on k3s first; egress default-deny is out of scope (DNS/AWS API breakage risk). |
| T374 | fix (partial) | `net:341-386` | S | Correct that egress is `0.0.0.0/0` on 80/443/53. Cheap: drop `nodes_to_internet_http` (`net:351`; ECR, OIDC, OpenAI, EKS bootstrap are all 443) and scope `nodes_dns_tcp/udp` (`net:369-386`) to `var.vpc_cidr` (CoreDNS forwards to the VPC resolver at CIDR+2). Accept the rest: Network Firewall/egress proxy ~$300/mo, interface endpoints ~$7/mo each. Optional free win: S3 gateway endpoint (cuts NAT data charges for ECR layers). |
| T375 | fix | `net:160-215` | S | Correct: DB subnets share the private RT with the NAT default route (`net:208-215`). Mitigated today because the RDS SG has zero egress rules (live `egress: []`), but a local-only `aws_route_table.database` plus re-pointed associations is free and removes the path by design. |
| T376 | fix | `pub:214-215` | S | Correct: `deletion_protection = false`, `skip_final_snapshot = true` set explicitly. Backups already 7 days (`database/aws/variables.tf:66-69`, live). Flip both; module already derives `final_snapshot_identifier` (`database/aws/main.tf:74`). Trade-off: `terraform destroy` becomes two-step. Skip `prevent_destroy` and IAM denies (over-engineering for one operator). |
| T377 | accept | | - | Single-AZ RDS is the cost design (doubles the instance). Enhanced Monitoring / PI on `db.t3.micro` adds cost/noise; PI support on t3.micro for PostgreSQL is uncertain. The useful part (storage alarm) lands with T362. |
| T378 | accept | | - | Inherent to Terraform-managed secrets: `random_password` + `kubernetes_secret_v1` (`k8s_resources.tf:190-217`) are in state by design; backend has `encrypt = true` (`pub:57-60`), bucket is per-deployer via gitignored `backend.hcl`. `manage_master_user_password` would break the DATABASE_URL pattern; ESO/CSI driver is a new controller for one secret. |
| T379 | accept | | L | Correct: no `aws_secretsmanager_secret_rotation`, default KMS key (live `rot: null`, `kms: null`). DB/Redis rotation needs a Lambda in the VPC plus pod restarts; JWT rotation needs `kid` support in the app; the settings key cannot rotate without an app re-encrypt path. Document manual rotation (taint `random_password`, re-apply) instead. |
| T380 | accept | `pub:132-151` | - | `MUTABLE` and `force_delete = true` are deliberate and documented (`pub:136-144`); the build pushes `:latest` alongside the SHA tag (`container_build_helpers.py:565`), so `IMMUTABLE` would break every rebuild. `IMMUTABLE_WITH_EXCLUSION` (excluding `latest`) is possible with the locked provider 6.56 but adds little since deploys already pin by SHA. |
| T381 | fix | `log:258` | S | Correct: `image = "amazon/aws-for-fluent-bit:latest"` from Docker Hub. Pin to a version tag from ECR Public (`public.ecr.aws/aws-observability/aws-for-fluent-bit:<ver>`), optionally `@sha256`. No admission controller (Kyverno/Gatekeeper is out of scope for this cluster). |
| T382 | fix | `log:228-330` | S | Correct: no `security_context` in the DaemonSet. Add `allow_privilege_escalation=false`, `capabilities.drop=["ALL"]`, `read_only_root_filesystem=true`, seccomp `RuntimeDefault`. Root stays (container logs under `/var/log` are root-readable only); `/var/log` mount is already `read_only` (`log:271`). |
| T383 | accept | `pub:297-300` | - | Deliberate and commented: reviewer capability is granted to every authenticated user on purpose. Confirm with the owner that this is still intended for a public demo; flipping is a one-line change if not. |
| T384 | false-positive | | - | `aws_flow_log.tmi` exists (`net:413-425`, `traffic_type = ALL`, parquet to the persistent log bucket, `pub:182`); live status `ACTIVE`. GuardDuty is not enabled (`list-detectors` empty); optional follow-up, ~$5-10/mo at this volume. |
| T385 | accept | `pub:336`, `log:177` | S | Correct: tail is `tmi-*.log`, retention 30d. Broadening ingests kube-system/CoreDNS/KEDA noise at $0.50/GB. Cheap optional: retention 90d. EKS audit/authenticator logs are already shipped (T367). |
| T386 | accept | `modules/certificates/aws/main.tf:47` | - | `allow_overwrite = true` on ACM validation records is the AWS-provider-documented pattern (cert re-create reuses the same `_acm-challenge` name). Scope is limited to those CNAMEs in `var.hosted_zone_id`; domain is a variable, not attacker-controlled. |
| T387 | accept | | - | Single NAT is the cost design; the EIP is permanent (`aws-persistent`, `eipalloc-07c325e51173c0bc9`) and must never be released or moved. Per-AZ NAT is +$32/mo plus a second EIP. Optional: S3 gateway endpoint (free) as in T374. |
| T388 | accept (TLS) / fix (netpol, via T356/T373) | | - | Redis already has a password (`ovl/patches/redis-auth.yaml:46-53`) and an ingress NetworkPolicy (`ovl/networkpolicy-redis.yml`). In-cluster TLS/mTLS needs cert-manager or a mesh; app has no Redis TLS toggle. NATS gap is handled in T356. |
| T389 | accept | `k8s:699-709` | - | Chart version is pinned (`1.17.1`, `k8s/variables.tf:259-262`) and a vendored-chart path already exists (`lb_controller_chart_local_path`). Helm `verify` needs a `.prov` the eks-charts repo does not publish. Pinning the controller image digest via values is an S follow-up if wanted. |

## 2. Duplicate groups (canonical first)

- T366 <- T348 (public EKS endpoint)
- T367 <- T353 (control-plane logging) [false-positive]
- T369 <- T360 (WAF / rate limiting)
- T370 <- T354 (port 80) [false-positive]
- T371 <- T355 (ALB->pod plaintext)
- T373 <- T357 (node SG / NetworkPolicy)
- T374 <- T358 (node egress)
- T376 <- T361 (RDS deletion protection; multi_az part -> T377)
- T379 <- T364 (secret rotation)
- T380 <- T351, T365 (ECR mutability / force_delete)
- T381 <- T352 (Fluent Bit image); T382 is the sibling securityContext finding
- T383 <- T349 (everyone_is_a_reviewer)
- T387 <- T363 (single NAT)
- T388 and T356 overlap on NATS; T356 kept as the NATS fix, T388 accept

## 3. Proposed fix order

1. **Terraform-only, no runtime risk** (one PR, `terraform plan` then apply): T372 delete NodePort rules; T375 database route table; T376 deletion protection + final snapshot; T362 `max_allocated_storage` + storage alarm; T366 `eks_public_access_cidrs` (needs the `deploy-aws.sh` tfvars change in the same PR).
2. **Logging module** (one PR): T381 pin Fluent Bit image, T382 securityContext. Roll the DaemonSet and confirm logs still land in `/tmi/tmi`.
3. **Node egress** (own PR, verify a node roll and image pull afterwards): T374 drop port 80 egress, scope DNS to VPC CIDR.
4. **NetworkPolicies** (overlay PR, test on k3s/docker-desktop first): T373 default-deny ingress + allows; covers T356 (NATS 4222) and T388. Depends on nothing above; do after 1-3 so failures are attributable.
5. **WAF** (Terraform ACL + overlay annotation + deploy-aws placeholder): T369. Depends on step 1 only for PR ordering; verify with a rate test against api.tmi.dev.
6. Follow-ups to file, not fix now: HSTS behind ALB (T370), NATS token auth (T356), GuardDuty (T384), retention 90d (T385), owner confirmation on `everyone_is_a_reviewer` (T383).

## 4. Where the threats misread the code

- **T367/T353**: audit + authenticator control-plane logs are enabled (`pub:256`, `k8s:67-79`, live).
- **T384**: VPC Flow Logs exist (`net:413-425`) and are ACTIVE, delivered to the persistent log bucket.
- **T370/T354**: port 80 is an ALB redirect to 443 (`ovl/ingress.yml:20-21`, live listener), not forwarded; the ALB listener is overlay-managed by design, not "unguaranteed".
- **T369**: ALB access logs are already on (`ovl/ingress.yml:19`, live attributes).
- **T372**: the NodePort SG rules are dead code; targets are pod IPs (`ovl/ingress.yml:17`, live target groups `type: ip`).
- **T351**: deploys pin an immutable git-SHA tag (`deploy-aws.sh:612-645`); `:latest` is only an extra build tag.
- **T376/T361**: automated backups are already 7 days, not absent.
- **T375**: the RDS SG has no egress rules at all (live), so the NAT route is unreachable from the DB today.
- **T389**: the chart is version-pinned and a vendored-chart path exists (`k8s/variables.tf:265-268`).
- **T388**: Redis is password-protected and already has a NetworkPolicy; only NATS is open.
- **T383/T349, T380/T365**: these are deliberate, commented decisions (`pub:136-144`, `pub:297-300`), not oversights.
- **T373/T357, T388**: NetworkPolicies are not "out-of-band": the overlay is the workload IaC and already carries one; the gap is coverage, not tooling.
