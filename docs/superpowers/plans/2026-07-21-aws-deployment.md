# AWS Deployment (Hybrid Terraform + Kustomize) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy the full current TMI stack (server, Redis, NATS, KEDA, component-controller, extractor + chunk-embed workers) to AWS EKS at https://server.aws.tmi.dev, with Terraform owning infra only and workloads coming from the shared kustomize manifest base.

**Architecture:** Terraform (`terraform/environments/aws-public`) provisions VPC/EKS/RDS/ECR×5/Secrets/ACM/DNS/logging plus bootstrap k8s objects (namespace, config Secret/ConfigMap, IRSA SAs, ALB controller); a new `deployments/k8s/dev/aws/` kustomize overlay supplies all workloads from the same bases local dev uses; `scripts/deploy-aws.sh` orchestrates build→terraform→kubectl→verify. Dev-DB operational config is replicated to RDS via a new dbtool `--export-config` + existing `--import-config`.

**Tech Stack:** Terraform ≥1.5 (AWS provider ≥5.0), EKS, kustomize (`kubectl kustomize --load-restrictor LoadRestrictionsNone`), Go (dbtool), Python/uv (build scripts), bash (deploy script).

**Spec:** `docs/superpowers/specs/2026-07-21-aws-deployment-design.md`

## Global Constraints

- AWS account 967218005408, CLI profile `tmi`, region `us-east-1`. DNS zone `aws.tmi.dev` = `Z05646533D2YL1I678JXS` (already delegated; parent zone is in a different account — never needed by this plan).
- NEVER read `~/.aws/credentials`. Never interact with SSH keys or ssh-agent.
- Always use make targets, never raw `go test` / `go run` / `docker run`.
- All lint/build/test gates per CLAUDE.md Task Completion Workflow. `make lint` after any file change; `make build-server` + `make test-unit` after Go changes.
- Any DB-touching Go change requires the `oracle-db-admin` subagent review before task completion (Task 2).
- Frugal choices: db.t3.micro, single NAT, single node, no Multi-AZ.
- Nothing environment-specific (account IDs, zone IDs, ARNs, tmi.dev names) may be committed — those live in gitignored tfvars/backend/generated files. Committed code takes them as variables/flags.
- SEM markers: add/update `SEM@` markers on new/changed Go functions; run `/sem-annotate --update <files>` per task.
- Conventional commits; feature work on `feature/aws-deployment` branch (Task 1).

---

### Task 1: Branch setup

**Files:** none (git only)

**Interfaces:**
- Produces: branch `feature/aws-deployment` containing the two spec commits; local `main` reset to `origin/main`.

- [ ] **Step 1: Create the feature branch at current main (which holds the two spec commits)**

```bash
git checkout -b feature/aws-deployment
```

- [ ] **Step 2: Reset local main to origin (main is PR-only; spec commits ride the feature branch)**

```bash
git branch -f main origin/main
git log --oneline -3   # expect: spec commits on feature/aws-deployment, not on main
```

- [ ] **Step 3: Verify**

Run: `git branch --contains d9a358e0`
Expected: only `feature/aws-deployment` listed.

---

### Task 2: dbtool `--export-config`

**Files:**
- Create: `cmd/dbtool/config_export.go`
- Create: `cmd/dbtool/config_export_test.go`
- Modify: `cmd/dbtool/main.go` (flag wiring + usage text, near line 32 where `importConfig` is defined)

**Interfaces:**
- Consumes: `models.SystemSetting` (api/models), `testdb.TestDB` (test/testdb), `crypto.SettingsEncryptor` + `secrets.NewProvider` (same pattern as `runConfigSeed` in `cmd/dbtool/config.go:87-103`), `writeMigratedConfig`-style nested-YAML building (`cmd/dbtool/config.go:194-229`).
- Produces: `runConfigExport(db *testdb.TestDB, cfgPath, outputFile string, decryptSecrets bool) error` — reads all rows from `system_settings`, optionally decrypts secret values, writes nested YAML (dotted keys → nested maps, same shape `config.Load` + `--import-config` accepts). CLI: `tmi-dbtool --export-config -f <config-for-db-conn> --output <file> [--no-decrypt]`.

- [ ] **Step 1: Read the neighboring code**

Read `cmd/dbtool/config.go` fully (import path you are mirroring), `cmd/dbtool/main.go` flag block, and one existing test (`cmd/dbtool/config_strip_test.go`) for test conventions. Read `internal/config/migratable_settings.go` for how `Secret` settings are identified (needed to know which keys to decrypt: a DB row is a secret iff its key matches a `MigratableSetting` with `Secret: true`).

- [ ] **Step 2: Write the failing test** (`cmd/dbtool/config_export_test.go`)

Follow the package's existing test style. Core cases (table-driven where natural):

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// exercises buildNestedConfig: dotted keys become nested maps, values typed by setting_type
func TestBuildNestedConfig(t *testing.T) {
	rows := []exportRow{
		{Key: "operator.name", Value: "Eric", Type: "string"},
		{Key: "extraction.async_enabled", Value: "true", Type: "bool"},
		{Key: "websocket.inactivity_timeout_seconds", Value: "300", Type: "int"},
	}
	root := buildNestedConfig(rows)

	op, ok := root["operator"].(map[string]any)
	if !ok {
		t.Fatalf("operator not nested: %#v", root)
	}
	if op["name"] != "Eric" {
		t.Errorf("operator.name = %v", op["name"])
	}
	ex := root["extraction"].(map[string]any)
	if ex["async_enabled"] != true {
		t.Errorf("bool not coerced: %v", ex["async_enabled"])
	}
	ws := root["websocket"].(map[string]any)
	if ws["inactivity_timeout_seconds"] != 300 {
		t.Errorf("int not coerced: %v", ws["inactivity_timeout_seconds"])
	}
}

// exercises writeExportedConfig: file round-trips through YAML
func TestWriteExportedConfig(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "export.yml")
	rows := []exportRow{{Key: "operator.name", Value: "Eric", Type: "string"}}
	if err := writeExportedConfig(rows, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}
	if parsed["operator"].(map[string]any)["name"] != "Eric" {
		t.Errorf("round-trip failed: %#v", parsed)
	}
}
```

- [ ] **Step 3: Run the test, verify it fails**

Run: `make test-unit name=TestBuildNestedConfig`
Expected: FAIL — `undefined: exportRow`, `undefined: buildNestedConfig`.

- [ ] **Step 4: Implement `cmd/dbtool/config_export.go`**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/secrets"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"gopkg.in/yaml.v3"
)

// exportRow is one system_settings row prepared for YAML export.
// SEM: add marker via /sem-annotate --update after implementation.
type exportRow struct {
	Key   string
	Value string
	Type  string
}

// buildNestedConfig converts dotted setting keys into a nested map, coercing
// values per setting_type so the YAML round-trips through config.Load.
func buildNestedConfig(rows []exportRow) map[string]any {
	root := make(map[string]any)
	for _, r := range rows {
		var val any = r.Value
		switch r.Type {
		case "bool":
			if b, err := strconv.ParseBool(r.Value); err == nil {
				val = b
			}
		case "int":
			if n, err := strconv.Atoi(r.Value); err == nil {
				val = n
			}
		}
		parts := strings.Split(r.Key, ".")
		current := root
		for i, part := range parts {
			if i == len(parts)-1 {
				current[part] = val
				continue
			}
			if _, ok := current[part]; !ok {
				current[part] = make(map[string]any)
			}
			if next, ok := current[part].(map[string]any); ok {
				current = next
			}
		}
	}
	return root
}

// writeExportedConfig serializes rows as nested YAML with a provenance header.
func writeExportedConfig(rows []exportRow, outputPath string) error {
	header := fmt.Sprintf("# TMI operational settings exported from database\n"+
		"# Generated by tmi-dbtool --export-config on %s\n"+
		"# Import into a target database with: tmi-dbtool --import-config -f <this file>\n\n",
		time.Now().UTC().Format(time.RFC3339))
	yamlData, err := yaml.Marshal(buildNestedConfig(rows))
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}
	return os.WriteFile(outputPath, append([]byte(header), yamlData...), 0o600)
}

// runConfigExport reads all system_settings rows and writes them as a nested
// YAML config file suitable for --import-config into another database.
// Secret settings are decrypted when an encryptor is available and
// decryptSecrets is true; otherwise they are skipped with a warning (an
// encrypted blob is useless across databases with different keys).
func runConfigExport(db *testdb.TestDB, cfgPath, outputFile string, decryptSecrets bool) error {
	log := slogging.Get()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config %s: %w", cfgPath, err)
	}

	// Identify which keys are secret (same source of truth the import uses).
	secretKeys := make(map[string]bool)
	for _, s := range cfg.GetMigratableSettings() {
		if s.Secret {
			secretKeys[s.Key] = true
		}
	}

	var encryptor *crypto.SettingsEncryptor
	if decryptSecrets {
		if provider, perr := secrets.NewProvider(context.Background(), &cfg.Secrets); perr == nil {
			if enc, eerr := crypto.NewSettingsEncryptor(context.Background(), provider); eerr == nil {
				encryptor = enc
			}
			_ = provider.Close()
		}
	}

	var settings []models.SystemSetting
	if err := db.DB().Order("setting_key").Find(&settings).Error; err != nil {
		return fmt.Errorf("failed to read system_settings: %w", err)
	}

	var rows []exportRow
	var skippedSecrets int
	for _, s := range settings {
		key := string(s.SettingKey)
		value := string(s.Value)
		if secretKeys[key] {
			if encryptor == nil {
				skippedSecrets++
				log.Warn("Skipping secret setting %s (no decryptor available)", key)
				continue
			}
			plain, derr := encryptor.Decrypt(value)
			if derr != nil {
				return fmt.Errorf("failed to decrypt setting %s: %w", key, derr)
			}
			value = plain
		}
		rows = append(rows, exportRow{Key: key, Value: value, Type: string(s.SettingType)})
	}

	if err := writeExportedConfig(rows, outputFile); err != nil {
		return fmt.Errorf("failed to write export: %w", err)
	}
	log.Info("Exported %d settings to %s (%d secrets skipped)", len(rows), outputFile, skippedSecrets)
	return nil
}
```

Note: verify `crypto.SettingsEncryptor` has a `Decrypt` method before relying on it (`rg -n 'func .*SettingsEncryptor.*Decrypt' internal/crypto/`). If the method has a different name/signature, adapt this code and the flag docs to match — do not add new crypto code.

- [ ] **Step 5: Wire the flag in `cmd/dbtool/main.go`**

Next to the `importConfig` flag (line ~32):

```go
exportConfig := flag.Bool("export-config", false, "Export database settings to a config YAML file")
noDecrypt := flag.Bool("no-decrypt", false, "With --export-config: skip secret settings instead of decrypting")
```

In the dispatch section (mirror the `--import-config` arm, including its `--input-file` requirement check — for export, require `--output`):

```go
case *exportConfig:
	if *outputFile == "" {
		runErr = fmt.Errorf("--output is required for --export-config")
	} else {
		runErr = runConfigExport(db, *inputFile, *outputFile, !*noDecrypt)
	}
```

(Adapt to main.go's actual dispatch structure — it may use if/else rather than switch; check whether an `--output` flag already exists and reuse it.) Update the usage/help text block (line ~227 area) with the new flags.

- [ ] **Step 6: Run tests, lint, build**

Run: `make test-unit name=TestBuildNestedConfig` then `make test-unit name=TestWriteExportedConfig` — expect PASS.
Run: `make lint` — fix any issues. Run: `make build-server` — must succeed (dbtool builds under the same module; also run `go vet ./cmd/dbtool/` via lint).

- [ ] **Step 7: SEM markers + Oracle review**

Run `/sem-annotate --update cmd/dbtool/config_export.go cmd/dbtool/main.go`.
Dispatch the `oracle-db-admin` subagent (skill `oracle-db-admin`) on this change (reads system_settings via GORM — CLOB/VARCHAR semantics on Oracle are in scope). Address BLOCKING findings before commit.

- [ ] **Step 8: Commit**

```bash
git add cmd/dbtool/config_export.go cmd/dbtool/config_export_test.go cmd/dbtool/main.go
git commit -m "feat(dbtool): add --export-config for DB-to-DB settings replication"
```

---

### Task 3: Add component-controller to the container build

**Files:**
- Modify: `scripts/build-app-containers.py` (lines 32, 38, 41: component tuples)
- Modify: `scripts/container_build_helpers.py` (`_get_dockerfile_map`, line ~176; check `image_name_map` usages for the aws target, line ~247-280)

**Interfaces:**
- Consumes: existing `Dockerfile.controller` (repo root — already exists).
- Produces: `--component controller` builds/pushes image `tmi-component-controller` to ECR repo `tmi-component-controller` under the aws target; `--component all` includes it.

- [ ] **Step 1: Wire the component**

In `scripts/build-app-containers.py`:

```python
VALID_COMPONENTS = ("server", "redis", "extractor", "chunkembed", "controller", "all")
...
ALL_COMPONENTS = ("server", "redis", "extractor", "chunkembed", "controller")
```

(`controller` is NOT client-dependent — leave `CLIENT_DEPENDENT_COMPONENTS` unchanged.)

In `scripts/container_build_helpers.py` `_get_dockerfile_map`:

```python
        "chunkembed": "Dockerfile.chunkembed",
        "controller": "Dockerfile.controller",
```

Check each target's `image_name_map` in `get_target_config`: the image must be named `tmi-component-controller` (matching the manifests), not `tmi-controller`. If the default naming is `{prefix}{component}` (see `scan_component`, line ~315), add an explicit map entry for every target that pushes:

```python
image_name_map={..., "controller": "tmi-component-controller"},
```

Read the local/dev target config too and make the same mapping there so `make dev-up` and AWS agree on the name.

- [ ] **Step 2: Test the build locally**

Run: `uv run scripts/build-app-containers.py --component controller`
Expected: builds `tmi-component-controller:latest` (or :dev per tag logic) with exit 0. Then `docker images | rg tmi-component-controller` shows the image.

- [ ] **Step 3: Lint + script tests**

Run: `make lint` (covers Python if configured). If `scripts/lib/tests/` has tests for the build helpers, run them per the repo's script-test make target (`make list-targets | rg -i 'script|py'` to find it); otherwise the build in Step 2 is the test.

- [ ] **Step 4: Commit**

```bash
git add scripts/build-app-containers.py scripts/container_build_helpers.py
git commit -m "feat(build): add component-controller image to app container builds"
```

---

### Task 4: Terraform — infra-only refactor + TLS/DNS + 5 ECR repos

**Files:**
- Modify: `terraform/environments/aws-public/main.tf`
- Modify: `terraform/environments/aws-public/variables.tf`
- Modify: `terraform/environments/aws-public/outputs.tf`
- Modify: `terraform/environments/aws-public/terraform.tfvars.example`
- Modify: `terraform/modules/certificates/aws/main.tf` (+ its variables.tf/outputs.tf)
- Modify: `terraform/modules/kubernetes/aws/k8s_resources.tf` (strip workloads, keep bootstrap)
- Modify: `terraform/modules/kubernetes/aws/variables.tf`, `outputs.tf` (drop workload-only vars)

**Interfaces:**
- Consumes: hosted zone id + domain name as variables; module.network / module.secrets / module.database unchanged.
- Produces: terraform outputs consumed by Task 5/6: `ecr_repository_urls` (map keyed `server|redis|extractor|chunkembed|controller`), `certificate_arn`, `rds_endpoint`, `cluster_name`, `namespace`. Bootstrap k8s objects that the overlay references by name: namespace `tmi-platform`, Secret `tmi-secrets`, ConfigMap `tmi-server-config`, IRSA ServiceAccount for the server (keep existing names from k8s_resources.tf — read it first and preserve them).

- [ ] **Step 1: Read the module before cutting**

Read `terraform/modules/kubernetes/aws/k8s_resources.tf` fully. Inventory: which resources are bootstrap (namespace, ConfigMap `kubernetes_config_map_v1.tmi` line ~24, service account `kubernetes_service_account_v1.tmi_api` line ~75, any Secret resources) vs workload (Deployments `tmi_api` line ~103 / `redis` line ~218, Services lines ~302/332, Ingress line ~362). Record the exact names/labels the ConfigMap/Secret/SA use — the overlay (Task 5) must reference the same names.

- [ ] **Step 2: ECR repos → for_each**

Replace the two `aws_ecr_repository` resources in `environments/aws-public/main.tf:100-120` with:

```hcl
locals {
  ecr_components = ["server", "redis", "extractor", "chunkembed", "controller"]
}

resource "aws_ecr_repository" "tmi" {
  for_each             = toset(local.ecr_components)
  name                 = each.key == "controller" ? "tmi-component-controller" : "${var.name_prefix}-${each.key}"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = local.common_tags
}
```

Note: repo names must match what `container_build_helpers.py` pushes (Task 3): `tmi-server`, `tmi-redis`, `tmi-extractor`, `tmi-chunkembed`, `tmi-component-controller`. Verify the extractor/chunkembed push names in `container_build_helpers.py` aws target and align (`tmi-chunkembed` vs `tmi-chunk-embed` — the manifests use image `tmi-chunk-embed`; pick the manifest spelling and make repo + build map agree).

- [ ] **Step 3: Certificates module — add validation**

`terraform/modules/certificates/aws/main.tf` currently only creates the cert. Append:

```hcl
resource "aws_route53_record" "validation" {
  for_each = {
    for dvo in aws_acm_certificate.tmi.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  }

  zone_id         = var.hosted_zone_id
  name            = each.value.name
  type            = each.value.type
  ttl             = 60
  records         = [each.value.record]
  allow_overwrite = true
}

resource "aws_acm_certificate_validation" "tmi" {
  certificate_arn         = aws_acm_certificate.tmi.arn
  validation_record_fqdns = [for r in aws_route53_record.validation : r.fqdn]
}
```

Add `variable "hosted_zone_id" { type = string }` to the module's variables.tf and output the **validated** ARN:

```hcl
output "certificate_arn" {
  value = aws_acm_certificate_validation.tmi.certificate_arn
}
```

- [ ] **Step 4: Wire certificates + dns modules into aws-public**

In `environments/aws-public/main.tf` add:

```hcl
module "certificates" {
  source = "../../modules/certificates/aws"

  name_prefix    = var.name_prefix
  domain_name    = var.domain_name
  hosted_zone_id = var.hosted_zone_id
  subject_alternative_names = []
  tags           = local.common_tags
}
```

DNS CNAME to the ALB: the ALB is created by the AWS Load Balancer Controller from the overlay's Ingress (Task 5), so its DNS name is not known to terraform. Do NOT wire `modules/dns/aws` here. Instead the deploy script creates/updates the CNAME after the Ingress reports its hostname (Task 6 Step 5). Delete nothing from `modules/dns/aws` (other environments may use it).

New variables in `environments/aws-public/variables.tf`:

```hcl
variable "domain_name" {
  description = "FQDN for the TMI server (e.g. server.aws.example.com)"
  type        = string
}

variable "hosted_zone_id" {
  description = "Route 53 hosted zone ID that domain_name lives in (zone must be in this account)"
  type        = string
}
```

Add both to `terraform.tfvars.example` with placeholder values.

- [ ] **Step 5: Strip workloads from the kubernetes module**

In `terraform/modules/kubernetes/aws/k8s_resources.tf`: delete the Deployments (`tmi_api`, `redis`), Services (`tmi_api`, `redis`), and Ingress (`tmi_api`) resources. KEEP: namespace, ConfigMap, Secret(s), ServiceAccount(s), and everything in `main.tf` (EKS cluster, node group, IAM/IRSA, ALB controller Helm release). Remove now-unused variables (`tmi_image_url`, `redis_image_url`, `tmi_replicas`, `alb_scheme`, `alb_subnet_ids`, `certificate_arn`, probe/resource vars if any) from the module's variables.tf and the corresponding arguments from `environments/aws-public/main.tf:174-216` — keep `tmi_build_mode`, db/redis/jwt secret vars (they feed the kept ConfigMap/Secret), and `extra_environment_variables` if the ConfigMap consumes it. `terraform validate` is the arbiter: zero unused-variable warnings, zero undeclared references.

The kept ConfigMap must gain the AWS-shape server config the overlay expects (see Task 5 Step 1): ensure it carries `TMI_NATS_URL = "nats://nats.tmi-platform.svc:4222"` and the RDS/Redis endpoints it already templates.

- [ ] **Step 6: Outputs**

`environments/aws-public/outputs.tf` — ensure these exist (add if missing):

```hcl
output "ecr_repository_urls" {
  value = { for k, r in aws_ecr_repository.tmi : k => r.repository_url }
}

output "certificate_arn" {
  value = module.certificates.certificate_arn
}

output "rds_endpoint" {
  value = module.database.host
}

output "cluster_name" {
  value = module.kubernetes.cluster_name
}
```

- [ ] **Step 7: Validate**

```bash
cd terraform/environments/aws-public
terraform init -backend=false
terraform validate
```

Expected: `Success! The configuration is valid.` Also run `terraform fmt -recursive` from `terraform/`.

- [ ] **Step 8: Commit**

```bash
git add terraform/
git commit -m "feat(terraform): aws-public infra-only refactor — 5 ECR repos, ACM validation, workload resources removed"
```

---

### Task 5: AWS kustomize overlay

**Files:**
- Create: `deployments/k8s/dev/aws/kustomization.yaml`
- Create: `deployments/k8s/dev/aws/ingress.yml`
- Create: `deployments/k8s/dev/aws/patches/server-config.yaml`
- Create: `deployments/k8s/dev/aws/patches/nats-storageclass.yaml`
- Create: `deployments/k8s/dev/aws/patches/extractor-image.yaml`
- Create: `deployments/k8s/dev/aws/patches/chunkembed-image.yaml`
- Create: `deployments/k8s/dev/aws/README.md`
- Modify: `.gitignore` (add `deployments/k8s/dev/aws/generated-*.yaml`)

**Interfaces:**
- Consumes: bases `../controller.yml`, `../redis.yml`, `../server.yml`, `../../platform/components/tmi-extractor.yml`, `../../platform/components/tmi-chunk-embed.yml`; bootstrap objects created by terraform (namespace `tmi-platform`, ConfigMap `tmi-server-config`, Secret + SA names recorded in Task 4 Step 1); terraform outputs (ECR registry URI, certificate ARN) injected by the deploy script as a generated kustomize component.
- Produces: `kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/aws` renders the full workload set; the deploy script writes `deployments/k8s/dev/aws/generated-images.yaml` (gitignored) to pin ECR URIs.

- [ ] **Step 1: Compare bases against terraform bootstrap**

Diff what `../server.yml` expects (ConfigMap `tmi-server-config` mounted at `/etc/tmi`, env vars at lines 29-52) against what the terraform ConfigMap provides (Task 4 Step 1 inventory). The dev manifest's local-only env (`TMI_WEBHOOK_ALLOW_HTTP_TARGETS`, `TMI_SSRF_WEBHOOK_ALLOWLIST=host.docker.internal`) must be REMOVED for AWS; `TMI_NATS_URL` stays (NATS runs in-cluster). Record the deltas — they drive `patches/server-config.yaml`.

- [ ] **Step 2: Write `kustomization.yaml`**

```yaml
# AWS (EKS) overlay. Terraform owns infra + bootstrap objects (namespace,
# tmi-server-config ConfigMap, secrets, IRSA service accounts); this overlay
# owns every workload. Image URIs are pinned by the deploy script via
# generated-images.yaml (gitignored) because the ECR registry is
# account-specific. NATS + KEDA + the TMIComponent CRD are applied by
# scripts/deploy-aws.sh before this overlay (same order as scripts/lib/
# deploy.py apply_platform_base). Postgres is RDS — no in-cluster DB.
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: tmi-platform
resources:
  - ../controller.yml
  - ../redis.yml
  - ../server.yml
  - ../../platform/components/tmi-extractor.yml
  - ../../platform/components/tmi-chunk-embed.yml
  - ingress.yml
patches:
  - path: patches/server-config.yaml
    target:
      kind: Deployment
      name: tmi-server
  - path: patches/nats-storageclass.yaml
    target:
      kind: StatefulSet
      name: nats
  - path: patches/extractor-image.yaml
    target:
      group: tmi.dev
      version: v1alpha1
      kind: TMIComponent
      name: tmi-extractor
  - path: patches/chunkembed-image.yaml
    target:
      group: tmi.dev
      version: v1alpha1
      kind: TMIComponent
      name: tmi-chunk-embed
```

IMPORTANT caveat: `nats.yml` is applied by the deploy script, not this overlay (matching local dev), so the `nats-storageclass` patch cannot live here — kustomize can only patch resources it renders. Instead the deploy script applies NATS with a storage-class override (Task 6 Step 4). Remove the nats patch entry from this kustomization and delete `patches/nats-storageclass.yaml` from the file list IF `platform/nats.yml`'s PVC template already uses the cluster default storage class (check `rg -n 'storageClassName' deployments/k8s/platform/nats.yml`); EKS's default `gp2`/`gp3` will then bind with no patch at all. Only if nats.yml pins a storage class does the deploy script need a sed-style override. Resolve this during implementation and document the outcome in the overlay README.

- [ ] **Step 3: Write `ingress.yml`**

```yaml
# ALB Ingress for the TMI server on EKS. The certificate ARN is injected by
# the deploy script (generated-ingress-patch.yaml) because ARNs are
# account-specific. 3600s idle timeout keeps WebSocket sessions alive.
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: tmi-server
  namespace: tmi-platform
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/healthcheck-path: /
    alb.ingress.kubernetes.io/load-balancer-attributes: idle_timeout.timeout_seconds=3600
    alb.ingress.kubernetes.io/listen-ports: '[{"HTTP": 80}, {"HTTPS": 443}]'
    alb.ingress.kubernetes.io/ssl-redirect: "443"
    alb.ingress.kubernetes.io/certificate-arn: CERT_ARN_PLACEHOLDER
spec:
  rules:
    - http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: tmi-server
                port:
                  number: 8080
```

(Mirror the annotation set from the deleted `kubernetes_ingress_v1.tmi_api` in the old `k8s_resources.tf` — Task 4 Step 1 inventory — including any subnet annotations it set; prefer letting the LB controller auto-discover tagged public subnets if the network module tags them `kubernetes.io/role/elb=1`; check `terraform/modules/network/aws/main.tf` and add the explicit `alb.ingress.kubernetes.io/subnets` annotation via the generated patch only if tags are absent.)

- [ ] **Step 4: Write `patches/server-config.yaml`**

Strategic-merge patch. `$patch: replace` on the env list swaps the entire dev env block for the AWS one (delete local-only entries, keep the rest):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tmi-server
spec:
  template:
    spec:
      containers:
        - name: server
          env:
            - { name: TMI_SERVER_INTERFACE, value: "0.0.0.0" }
            - { name: TMI_SERVER_PORT, value: "8080" }
            - { name: TMI_REDIS_HOST, value: "redis" }
            - { name: TMI_NATS_URL, value: "nats://nats.tmi-platform.svc:4222" }
            - { name: TMI_LOG_DIR, value: "/tmp/logs" }
            - { name: OAUTH_PROVIDERS_TMI_ENABLED, value: "true" }
            - { name: TMI_AUTH_AUTO_PROMOTE_FIRST_USER, value: "true" }
```

Also patch the image pull policy back to `IfNotPresent` (ECR tags are immutable per deploy, unlike the local `:dev` churn) and attach the IRSA service account name recorded in Task 4 Step 1 (`serviceAccountName`).

- [ ] **Step 5: Write the TMIComponent image patches**

Copy `deployments/k8s/dev/docker-desktop/patches/extractor-image.yaml` and `chunkembed-image.yaml` as the starting point, changing image to the placeholder form the deploy script rewrites (open the docker-desktop versions for the exact patch structure — TMIComponent CRs patch `.image`):

```yaml
- op: replace
  path: /image
  value: ECR_REGISTRY_PLACEHOLDER/tmi-extractor:latest
```

(match the actual JSON-patch vs strategic-merge style used by the docker-desktop patches).

- [ ] **Step 6: Render test**

```bash
kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/aws >/dev/null && echo RENDER-OK
```

Expected: `RENDER-OK` (placeholders are fine at render time). Then `make lint`.

- [ ] **Step 7: Commit**

```bash
git add deployments/k8s/dev/aws/ .gitignore
git commit -m "feat(deploy): AWS EKS kustomize overlay for full platform shape"
```

---

### Task 6: Rewrite `scripts/deploy-aws.sh`

**Files:**
- Modify: `scripts/deploy-aws.sh`
- Modify: `Makefile` (deploy-aws targets' help text if flags change)

**Interfaces:**
- Consumes: terraform outputs (`ecr_repository_urls`, `certificate_arn`, `cluster_name`, `rds_endpoint`); overlay from Task 5; build script from Task 3; dbtool from Task 2.
- Produces: `./scripts/deploy-aws.sh --domain <fqdn> --zone-id <zone> [--profile tmi] [--config-export <file>]` end-to-end deploy; `--dry-run`, `--destroy`, `--skip-build` preserved.

- [ ] **Step 1: Fix the terraform dir and add profile/backend handling**

Line 52: `TF_DIR="${PROJECT_ROOT}/terraform/environments/aws-public"` (and the log line 213). Add `--profile` option (default `tmi`) exported as `AWS_PROFILE`. Terraform init becomes:

```bash
terraform -chdir="${TF_DIR}" init -backend-config="${BACKEND_CONFIG:-${TF_DIR}/backend.hcl}"
```

with a preflight error if the backend file is missing (message tells the user to create it from the example in main.tf's comment block).

- [ ] **Step 2: Build/push all five images**

Replace the existing build section with:

```bash
uv run "${SCRIPT_DIR}/build-app-containers.py" --target aws --component all --push --scan
```

(Task 3 makes `all` include controller. `--skip-build` bypasses this.)

- [ ] **Step 3: Terraform apply with new vars**

Generate tfvars (existing pattern) now including `domain_name` and `hosted_zone_id`. After apply, capture outputs:

```bash
ECR_URLS_JSON=$(terraform -chdir="${TF_DIR}" output -json ecr_repository_urls)
CERT_ARN=$(terraform -chdir="${TF_DIR}" output -raw certificate_arn)
CLUSTER_NAME=$(terraform -chdir="${TF_DIR}" output -raw cluster_name)
RDS_ENDPOINT=$(terraform -chdir="${TF_DIR}" output -raw rds_endpoint)
aws eks update-kubeconfig --name "${CLUSTER_NAME}" --region "${REGION}"
```

- [ ] **Step 4: Apply platform base + overlay (mirrors `scripts/lib/deploy.py:428-432,537-550`)**

```bash
kubectl apply -f "${PROJECT_ROOT}/deployments/k8s/platform/nats.yml"
kubectl apply --server-side -f "${PROJECT_ROOT}/deployments/k8s/platform/keda.yml"
kubectl apply -f "${PROJECT_ROOT}/config/crd/bases/tmi.dev_tmicomponents.yaml"

# Rewrite account-specific placeholders into the gitignored generated patch,
# then render + apply the overlay.
OVERLAY="${PROJECT_ROOT}/deployments/k8s/dev/aws"
ECR_REGISTRY=$(echo "${ECR_URLS_JSON}" | jq -r '.server' | sed 's|/[^/]*$||')
sed -e "s|CERT_ARN_PLACEHOLDER|${CERT_ARN}|" \
    -e "s|ECR_REGISTRY_PLACEHOLDER|${ECR_REGISTRY}|" \
    <(kubectl kustomize --load-restrictor LoadRestrictionsNone "${OVERLAY}") \
  | kubectl apply -f -
```

(sed on the rendered stream avoids mutating tracked files; keep the per-file backup rule out of scope since nothing on disk is edited.)

- [ ] **Step 5: Create/refresh the server CNAME once the ALB exists**

```bash
log_info "Waiting for ALB hostname..."
for _ in $(seq 1 60); do
  ALB_HOST=$(kubectl get ingress tmi-server -n tmi-platform \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
  [[ -n "${ALB_HOST}" ]] && break
  sleep 10
done
[[ -n "${ALB_HOST}" ]] || { log_error "Ingress never got an ALB hostname"; exit 1; }

aws route53 change-resource-record-sets --hosted-zone-id "${ZONE_ID}" \
  --change-batch "{\"Changes\":[{\"Action\":\"UPSERT\",\"ResourceRecordSet\":{
    \"Name\":\"${DOMAIN}\",\"Type\":\"CNAME\",\"TTL\":300,
    \"ResourceRecords\":[{\"Value\":\"${ALB_HOST}\"}]}}]}"
```

- [ ] **Step 6: Optional config import (`--config-export <file>`)**

When the flag is given, replicate config into RDS through a temporary in-cluster TCP proxy (RDS is in private subnets — unreachable from the deployer directly):

```bash
kubectl run rds-proxy --restart=Never -n tmi-platform \
  --image=alpine/socat -- tcp-listen:5432,fork,reuseaddr "tcp:${RDS_ENDPOINT}:5432"
kubectl wait --for=condition=Ready pod/rds-proxy -n tmi-platform --timeout=120s
kubectl port-forward -n tmi-platform pod/rds-proxy 15432:5432 &
PF_PID=$!
trap 'kill ${PF_PID} 2>/dev/null; kubectl delete pod rds-proxy -n tmi-platform --ignore-not-found' EXIT

# dbtool connects to localhost:15432; DB credentials come from Secrets Manager
# via the aws CLI at runtime (never echoed): the script builds a temporary
# config file in a mktemp dir referencing them, runs the import, removes it.
go run ./cmd/dbtool --import-config -f "${CONFIG_EXPORT_FILE}" ...
```

NOTE: dbtool invocation must follow the repo's "make targets only" rule — check `scripts/run-dbtool.py` (used by Makefile line ~448) and reuse it rather than `go run` if it supports host/port overrides; adapt this block to whatever `run-dbtool.py` accepts. Getting the DB password without leaking it into logs: fetch with `aws secretsmanager get-secret-value` into a shell variable, write the temp config with `umask 077`, never `set -x` in this section. [Deviation from the CLAUDE.md AWS secret-safety rule (`asm-exec`/resolve-syntax) is acceptable here because dbtool reads a config file, not CloudFormation templates — flag this in the PR description.]

- [ ] **Step 7: Verify section**

Replace the old verify logic:

```bash
log_step "Verification"
for _ in $(seq 1 30); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "https://${DOMAIN}/" || true)
  [[ "${code}" == "200" ]] && break
  sleep 10
done
[[ "${code}" == "200" ]] || { log_error "https://${DOMAIN}/ returned ${code}"; exit 1; }
curl -s "https://${DOMAIN}/" | jq .
kubectl get pods -n tmi-platform
```

- [ ] **Step 8: Shellcheck + lint + commit**

Run: `shellcheck scripts/deploy-aws.sh` (fix findings), `make lint`.

```bash
git add scripts/deploy-aws.sh Makefile
git commit -m "feat(deploy): rewrite deploy-aws.sh for hybrid terraform+kustomize flow"
```

---

### Task 7: One-time prep, first deploy, config replication, smoke test (operational)

**Files:**
- Create (local only, never committed): `terraform/environments/aws-public/backend.hcl`, `terraform/environments/aws-public/terraform.tfvars` (generated by script), config export file.

**Interfaces:**
- Consumes: everything above.
- Produces: running service at https://server.aws.tmi.dev with replicated config.

- [ ] **Step 1: Profile region + state backend (idempotent one-timers)**

```bash
aws configure set region us-east-1 --profile tmi
aws s3api create-bucket --bucket tmi-tfstate-967218005408 --profile tmi
aws s3api put-bucket-versioning --bucket tmi-tfstate-967218005408 \
  --versioning-configuration Status=Enabled --profile tmi
aws s3api put-bucket-encryption --bucket tmi-tfstate-967218005408 \
  --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}' --profile tmi
aws dynamodb create-table --table-name tmi-tf-locks \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST --profile tmi
```

Write `backend.hcl` (bucket, region, dynamodb_table) per the comment block in main.tf.

- [ ] **Step 2: Dry run**

```bash
make deploy-aws-dry-run ARGS="--domain server.aws.tmi.dev --zone-id Z05646533D2YL1I678JXS"
```

Review the plan: expect VPC/EKS/RDS/5 ECR/ACM/secrets/logging, no Deployment/Service/Ingress resources in terraform.

- [ ] **Step 3: Deploy**

```bash
make deploy-aws ARGS="--domain server.aws.tmi.dev --zone-id Z05646533D2YL1I678JXS"
```

(zone-id is the **aws.tmi.dev** zone in the tmi account — never the parent `tmi.dev` zone, which lives in a different account.) Expect ~20 min. All pods Running/Completed in `tmi-platform`; TMIComponent CRs reconciled by the controller.

- [ ] **Step 4: Config replication**

```bash
# export from local dev DB (dev cluster must be up: make dev-up).
# Use the repo's dbtool wrapper — check `uv run scripts/run-dbtool.py --help`
# and the Makefile dbtool targets for the exact invocation; NEVER `go run`.
uv run scripts/run-dbtool.py -- --export-config -f config-development.yaml --output /tmp/tmi-config-export.yml
```

(adapt to run-dbtool.py's actual argument pass-through, discovered in Task 6). Review the export and adjust before importing:

- **Hostnames/URLs:** anything referencing localhost, `*.svc` names that differ, or dev callback URLs → `https://server.aws.tmi.dev`. In particular the OAuth provider `callback_url`/redirect settings for the Google provider must point at the AWS host, and the Google OAuth app's authorized redirect URIs must include it.
- **Admin seeding (REQUIRED — auto-promote is OFF):** this deployment sets `TMI_AUTH_AUTO_PROMOTE_FIRST_USER=false`, so the ONLY path to admin is the `administrators` operational setting. Confirm the exported config's `administrators` list contains the fixed admin: `{provider: google, subject_type: user, email: hobobarbarian@gmail.com}` (the Google provider id lives in the dev DB and replicates). If absent, add it before importing — otherwise the deployment will have no administrator. `everyone_is_a_reviewer` stays true (build_mode=dev in the ConfigMap), so every authenticated user gets reviewer capability by design.

Then re-run deploy with import only:

```bash
make deploy-aws ARGS="--skip-build --domain server.aws.tmi.dev --zone-id Z05646533D2YL1I678JXS --config-export /tmp/tmi-config-export.yml"
```

Restart the server deployment so it re-reads DB settings: `kubectl rollout restart deployment/tmi-server -n tmi-platform`.

- [ ] **Step 5: Smoke test**

```bash
curl -s https://server.aws.tmi.dev/ | jq .            # version banner
```

Auth verification uses the **real Google provider** (the tmi stub is disabled and not compiled in — there is no `login_hint` shortcut on this deployment). Complete an interactive Google OAuth sign-in against `https://server.aws.tmi.dev/oauth2/authorize?idp=google` (idp is now REQUIRED — production build has no default provider), obtain a bearer token, then `GET /threat_models`. Confirm the seeded admin (hobobarbarian@gmail.com) has admin capabilities and that a second, non-admin Google account is a reviewer but not admin. Optional `wstest` against `wss://server.aws.tmi.dev` (using a real token). Extraction smoke: submit a document extraction and verify the extractor TMIComponent scales via KEDA and drains the NATS queue (`kubectl logs -l app=tmi-extractor -n tmi-platform`); this exercises the `tmi-embedding` secret path (Step 1 must have set `TMI_EMBEDDING_API_KEY`).

- [ ] **Step 6: Land the branch**

Use superpowers:finishing-a-development-branch — security-review skill, then PR `feature/aws-deployment` → main per Session Completion workflow (issues, push, verify).

---

## Self-Review (completed)

- **Spec coverage:** hybrid split (T4/T5/T6), full shape incl. NATS/KEDA/CRD (T5/T6 Step 4), 5 images (T3/T4), HTTPS+ACM (T4 S3-4, T5 S3), delegated-zone DNS (T6 S5), frugal RDS (unchanged db.t3.micro), config replication (T2, T6 S6, T7 S4), one-time prep (T7 S1), verification (T7 S5). Node sizing: t3.medium retained — summed requests of dev manifests are well under 2 vCPU/4 GiB (server requests 100m/128Mi; workers are scale-to-zero via KEDA); bump to t3.large only if pods Pending on first deploy.
- **Placeholder scan:** intentional runtime placeholders (`CERT_ARN_PLACEHOLDER`, `ECR_REGISTRY_PLACEHOLDER`) are deploy-script rewrite tokens, not plan gaps. Two verify-before-use notes (SettingsEncryptor.Decrypt signature, run-dbtool.py flags) are explicit implementer checks with commands.
- **Type consistency:** `exportRow`/`buildNestedConfig`/`writeExportedConfig`/`runConfigExport` consistent across T2 steps; output names (`ecr_repository_urls`, `certificate_arn`, `cluster_name`, `rds_endpoint`) consistent between T4 S6 and T6 S3.
