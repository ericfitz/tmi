# ADR: Supported deploy targets are AWS, k3s-rp, docker-desktop

Date: 2026-09-28. Status: accepted.

## Human decision (Eric, 2026-09-28)

Supported deployment/IaC targets for TMI, going forward:

1. **AWS** — `terraform/environments/aws-public` (live) and `aws-persistent` (long-lived NAT
   EIP + account logging, see the 2026-09-26 ADR), plus `terraform/modules/*/aws` and
   `scripts/deploy-aws.sh`.
2. **k3s-rp** — `deployments/k8s/dev/k3s`.
3. **docker-desktop** (local dev) — `deployments/k8s/dev/docker-desktop*`.

All OCI, GCP, and Azure Terraform environments and modules, the `aws-private` Terraform
environment, and all Heroku deployment tooling are removed as unsupported. This includes:
`terraform/environments/{oci-private,oci-public,gcp-private,gcp-public,azure-private,
azure-public,aws-private}`; `terraform/modules/*/{oci,gcp,azure}` and
`terraform/modules/compute` (OCI-only); the OCI Functions certificate manager
(`functions/certmgr`) and its build/deploy scripts; and all `deploy-oci*`, `build-app-{oci,
azure,gcp,heroku}`, `fn-*-certmgr`, `setup-heroku*`, `deploy-heroku`, `reset-db-heroku`,
`drop-db-heroku` Make targets and the scripts behind them.

**Oracle Database support is retained and unaffected.** TMI still develops and tests against
Oracle Autonomous Database (`scripts/oci-env.sh`, `make dev-up DB=oracle`,
`test-integration-oci`, `cats-fuzz-oci`, `build-dbtool-oci`, the `oracle-*` probe/check
scripts, and the `oracle-db-admin` review gate). Only OCI as a *deployment target* (compute,
Kubernetes, container registry, functions) is removed — Oracle as a *database* target is a
separate concern and stays fully supported.

**Go server runtime code that only served a removed target was also removed (2026-09-28,
follow-up decision).** The OCI Vault secrets provider (`internal/secrets/oci_provider.go`) and
the unimplemented Azure/GCP secrets-provider stubs in `internal/secrets/provider.go`; the OCI
cloud log writer (`internal/slogging/oci_cloud_writer.go`) and its wiring in
`cmd/server/main.go`; and Heroku `PORT`/`DATABASE_URL` env-var compatibility in
`internal/config/config.go` and `auth/config.go`, plus `SOURCE_VERSION`/`HEROKU_SLUG_COMMIT`
handling in `api/version.go`, are all deleted. `github.com/oracle/oci-go-sdk/v65` was dropped
from `go.mod` via `go mod tidy` once nothing imported it; `godror` (Oracle DB driver) and the
`mattn/go-sqlite3` v1.x pin are unaffected. The `env`, `aws`, and (still-unimplemented)
HashiCorp `vault` secrets providers, and the generic `CloudLogWriter`/`NoopCloudWriter`
logging extension point, remain.

**Single-instance topology (human decision, Eric, session 15, recorded 2026-10-06).** On every
supported target, TMI runs single-instance stateful pods (server, Redis, PostgreSQL, NATS), with no HA
replicas. Adding HA is a new architectural decision.

## Rationale

Maintaining five cloud Terraform environments and a Heroku deployment path added review and
CI surface with no corresponding production usage — AWS is the only cloud deployment TMI
actually runs, alongside the two local/lab Kubernetes dev targets.

## Consequences

- `TF_ENV` defaults to `aws-public` (was `oci-public`) in `scripts/manage-terraform.py` and
  the `Makefile`.
- Dependabot, CodeQL, and the security/deps-bump workflows no longer reference
  `functions/certmgr` or the removed scripts.
- Any future non-AWS cloud target is a new architectural decision, not a revert of this one.
