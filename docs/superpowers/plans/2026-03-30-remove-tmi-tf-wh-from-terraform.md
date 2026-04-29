# Remove tmi-tf-wh from Terraform Templates — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove tmi-tf-wh webhook analyzer infrastructure from TMI Terraform templates, with a migration README for existing deployers.

**Architecture:** Two-phase removal — a migration README tells existing deployers to disable before updating, then all tmi-tf-wh code is deleted from OCI environments, the shared K8s module, scripts, and Makefile.

**Tech Stack:** Terraform (HCL), Python (setup script), Bash (deploy script), Make

---

### Task 1: Create Migration README

**Files:**
- Create: `terraform/MIGRATE-TMI-TF-WH.md`

- [ ] **Step 1: Write the migration README**

Create `terraform/MIGRATE-TMI-TF-WH.md` with the following content:

```markdown
# Migrating tmi-tf-wh Out of TMI Terraform Templates

The tmi-tf-wh webhook analyzer is no longer managed by the TMI Terraform templates.
It is now deployed separately. If you previously had `tmi_tf_wh_enabled = true`,
follow these steps to cleanly remove the resources before updating to the latest templates.

## Steps

1. **Set `tmi_tf_wh_enabled = false`** in your `terraform.tfvars`:

   ```hcl
   tmi_tf_wh_enabled = false
   ```

2. **Preview the changes:**

   ```bash
   terraform plan
   ```

   Verify only tmi-tf-wh resources will be destroyed:
   - `oci_artifacts_container_repository.tmi_tf_wh`
   - `oci_queue_queue.tmi_tf_wh`
   - IAM policy statement updates (queue and generative-ai-family permissions removed)
   - `kubernetes_service_account_v1.tmi_tf_wh`
   - `kubernetes_config_map_v1.tmi_tf_wh`
   - `kubernetes_deployment_v1.tmi_tf_wh`
   - `kubernetes_service_v1.tmi_tf_wh`

3. **Apply:**

   ```bash
   terraform apply
   ```

4. **Remove tmi-tf-wh variables** from your `terraform.tfvars`:

   Delete any lines starting with `tmi_tf_wh_`:
   ```
   tmi_tf_wh_enabled
   tmi_tf_wh_image_url
   tmi_tf_wh_extra_env_vars
   ```

5. **Update to the latest TMI Terraform templates** (which no longer contain tmi-tf-wh).

## If You Never Enabled tmi-tf-wh

No action needed. Simply update to the latest templates.
```

- [ ] **Step 2: Commit**

```bash
git add terraform/MIGRATE-TMI-TF-WH.md
git commit -m "docs(terraform): add tmi-tf-wh migration README for existing deployers"
```

---

### Task 2: Remove tmi-tf-wh from oci-public environment

**Files:**
- Modify: `terraform/environments/oci-public/main.tf:138-143` (container repo)
- Modify: `terraform/environments/oci-public/main.tf:242-252` (queue)
- Modify: `terraform/environments/oci-public/main.tf:296-312` (module pass-through)
- Modify: `terraform/environments/oci-public/main.tf:408-412` (IAM policy)
- Modify: `terraform/environments/oci-public/variables.tf:284-303`
- Modify: `terraform/environments/oci-public/outputs.tf:56-59`
- Modify: `terraform/environments/oci-public/terraform.tfvars.example:109-113`

- [ ] **Step 1: Remove container repository resource from main.tf**

Delete lines 138-143 of `terraform/environments/oci-public/main.tf`:

```hcl
resource "oci_artifacts_container_repository" "tmi_tf_wh" {
  count          = var.tmi_tf_wh_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-tf-wh"
  is_public      = true
}
```

- [ ] **Step 2: Remove queue resource and its section comment from main.tf**

Delete lines 242-252 (the comment block and resource):

```hcl
# ---------------------------------------------------------------------------
# tmi-tf-wh Queue (optional — enabled when tmi_tf_wh_enabled is true)
# ---------------------------------------------------------------------------
resource "oci_queue_queue" "tmi_tf_wh" {
  count                            = var.tmi_tf_wh_enabled ? 1 : 0
  compartment_id                   = var.compartment_id
  display_name                     = "${var.name_prefix}-tf-wh-queue"
  visibility_in_seconds            = 3600
  retention_in_seconds             = 86400
  dead_letter_queue_delivery_count = 3
}
```

- [ ] **Step 3: Remove tmi-tf-wh resource sizing and module pass-through from kubernetes module call**

In the `module "kubernetes"` block, delete lines 296-299 (resource sizing):

```hcl
  tmi_tf_wh_cpu_request    = "200m"
  tmi_tf_wh_memory_request = "512Mi"
  tmi_tf_wh_cpu_limit      = "500m"
  tmi_tf_wh_memory_limit   = "1Gi"
```

And delete lines 308-312 (the comment and pass-through variables):

```hcl
  # tmi-tf-wh Webhook Analyzer configuration (optional)
  tmi_tf_wh_enabled        = var.tmi_tf_wh_enabled
  tmi_tf_wh_image_url      = var.tmi_tf_wh_image_url
  tmi_tf_wh_queue_ocid     = var.tmi_tf_wh_enabled ? oci_queue_queue.tmi_tf_wh[0].id : ""
  tmi_tf_wh_extra_env_vars = var.tmi_tf_wh_extra_env_vars
```

- [ ] **Step 4: Simplify IAM policy statements in main.tf**

Replace the `concat(...)` in `oci_identity_policy.vault_access` (lines 403-413) with a simple list:

Before:
```hcl
  statements = concat(
    [
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to read secret-family in compartment id ${var.compartment_id}",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use keys in compartment id ${var.compartment_id}",
    ],
    var.tmi_tf_wh_enabled ? [
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use queues in compartment id ${var.compartment_id} where target.queue.id = '${oci_queue_queue.tmi_tf_wh[0].id}'",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to manage queues in compartment id ${var.compartment_id} where target.queue.id = '${oci_queue_queue.tmi_tf_wh[0].id}'",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use generative-ai-family in compartment id ${var.compartment_id}",
    ] : []
  )
```

After:
```hcl
  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to read secret-family in compartment id ${var.compartment_id}",
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use keys in compartment id ${var.compartment_id}",
  ]
```

- [ ] **Step 5: Remove tmi-tf-wh variables from variables.tf**

Delete lines 284-303 of `terraform/environments/oci-public/variables.tf`:

```hcl
# ---------------------------------------------------------------------------
# tmi-tf-wh Webhook Analyzer (optional)
# ---------------------------------------------------------------------------
variable "tmi_tf_wh_enabled" {
  description = "Enable tmi-tf-wh webhook analyzer deployment"
  type        = bool
  default     = false
}

variable "tmi_tf_wh_image_url" {
  description = "Container image URL for tmi-tf-wh"
  type        = string
  default     = null
}

variable "tmi_tf_wh_extra_env_vars" {
  description = "Additional environment variables for tmi-tf-wh"
  type        = map(string)
  default     = {}
}
```

- [ ] **Step 6: Remove tmi-tf-wh output from outputs.tf**

Delete lines 56-59 of `terraform/environments/oci-public/outputs.tf`:

```hcl
output "tmi_tf_wh_load_balancer_ip" {
  description = "Public IP address of the tmi-tf-wh webhook load balancer"
  value       = module.kubernetes.tmi_tf_wh_load_balancer_ip
}
```

- [ ] **Step 7: Remove tmi-tf-wh section from terraform.tfvars.example**

Delete lines 109-113 of `terraform/environments/oci-public/terraform.tfvars.example`:

```
# ---------------------------------------------------------------------------
# Optional: TMI-TF-WH Webhook Analyzer
# ---------------------------------------------------------------------------
# tmi_tf_wh_enabled   = true
# tmi_tf_wh_image_url = "<region>.ocir.io/<namespace>/tmi/tmi-tf-wh:latest"
```

- [ ] **Step 8: Commit**

```bash
git add terraform/environments/oci-public/
git commit -m "refactor(terraform): remove tmi-tf-wh from oci-public environment"
```

---

### Task 3: Remove tmi-tf-wh from oci-private environment

**Files:**
- Modify: `terraform/environments/oci-private/main.tf:157-162` (container repo)
- Modify: `terraform/environments/oci-private/main.tf:290-300` (queue)
- Modify: `terraform/environments/oci-private/main.tf:342-346` (module pass-through)
- Modify: `terraform/environments/oci-private/main.tf:470-474` (IAM policy)
- Modify: `terraform/environments/oci-private/variables.tf:302-321`
- Modify: `terraform/environments/oci-private/outputs.tf:64-67`
- Modify: `terraform/environments/oci-private/terraform.tfvars.example:130-134`

- [ ] **Step 1: Remove container repository resource from main.tf**

Delete lines 157-162 of `terraform/environments/oci-private/main.tf`:

```hcl
resource "oci_artifacts_container_repository" "tmi_tf_wh" {
  count          = var.tmi_tf_wh_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-tf-wh"
  is_public      = false
}
```

- [ ] **Step 2: Remove queue resource and its section comment from main.tf**

Delete lines 290-300:

```hcl
# ---------------------------------------------------------------------------
# tmi-tf-wh Queue (optional — enabled when tmi_tf_wh_enabled is true)
# ---------------------------------------------------------------------------
resource "oci_queue_queue" "tmi_tf_wh" {
  count                            = var.tmi_tf_wh_enabled ? 1 : 0
  compartment_id                   = var.compartment_id
  display_name                     = "${var.name_prefix}-tf-wh-queue"
  visibility_in_seconds            = 3600
  retention_in_seconds             = 86400
  dead_letter_queue_delivery_count = 3
}
```

- [ ] **Step 3: Remove tmi-tf-wh module pass-through from kubernetes module call**

In the `module "kubernetes"` block, delete lines 342-346:

```hcl
  # tmi-tf-wh Webhook Analyzer configuration (optional)
  tmi_tf_wh_enabled        = var.tmi_tf_wh_enabled
  tmi_tf_wh_image_url      = var.tmi_tf_wh_image_url
  tmi_tf_wh_queue_ocid     = var.tmi_tf_wh_enabled ? oci_queue_queue.tmi_tf_wh[0].id : ""
  tmi_tf_wh_extra_env_vars = var.tmi_tf_wh_extra_env_vars
```

- [ ] **Step 4: Simplify IAM policy statements in main.tf**

Replace the `concat(...)` in `oci_identity_policy.vault_access` (lines 465-475) with a simple list:

Before:
```hcl
  statements = concat(
    [
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to read secret-family in compartment id ${var.compartment_id}",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use keys in compartment id ${var.compartment_id}",
    ],
    var.tmi_tf_wh_enabled ? [
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use queues in compartment id ${var.compartment_id} where target.queue.id = '${oci_queue_queue.tmi_tf_wh[0].id}'",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to manage queues in compartment id ${var.compartment_id} where target.queue.id = '${oci_queue_queue.tmi_tf_wh[0].id}'",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use generative-ai-family in compartment id ${var.compartment_id}",
    ] : []
  )
```

After:
```hcl
  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to read secret-family in compartment id ${var.compartment_id}",
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use keys in compartment id ${var.compartment_id}",
  ]
```

- [ ] **Step 5: Remove tmi-tf-wh variables from variables.tf**

Delete lines 302-321 of `terraform/environments/oci-private/variables.tf`:

```hcl
# ---------------------------------------------------------------------------
# tmi-tf-wh Webhook Analyzer (optional)
# ---------------------------------------------------------------------------
variable "tmi_tf_wh_enabled" {
  description = "Enable tmi-tf-wh webhook analyzer deployment"
  type        = bool
  default     = false
}

variable "tmi_tf_wh_image_url" {
  description = "Container image URL for tmi-tf-wh"
  type        = string
  default     = null
}

variable "tmi_tf_wh_extra_env_vars" {
  description = "Additional environment variables for tmi-tf-wh"
  type        = map(string)
  default     = {}
}
```

- [ ] **Step 6: Remove tmi-tf-wh output from outputs.tf**

Delete lines 64-67 of `terraform/environments/oci-private/outputs.tf`:

```hcl
output "tmi_tf_wh_load_balancer_ip" {
  description = "Internal IP address of the tmi-tf-wh webhook load balancer (null when ClusterIP)"
  value       = module.kubernetes.tmi_tf_wh_load_balancer_ip
}
```

- [ ] **Step 7: Remove tmi-tf-wh section from terraform.tfvars.example**

Delete lines 130-134 of `terraform/environments/oci-private/terraform.tfvars.example`:

```
# ---------------------------------------------------------------------------
# Optional: TMI-TF-WH Webhook Analyzer
# ---------------------------------------------------------------------------
# tmi_tf_wh_enabled   = true
# tmi_tf_wh_image_url = "<region>.ocir.io/<namespace>/tmi/tmi-tf-wh:latest"
```

- [ ] **Step 8: Commit**

```bash
git add terraform/environments/oci-private/
git commit -m "refactor(terraform): remove tmi-tf-wh from oci-private environment"
```

---

### Task 4: Remove tmi-tf-wh from Kubernetes OCI module

**Files:**
- Modify: `terraform/modules/kubernetes/oci/k8s_resources.tf:563-743`
- Modify: `terraform/modules/kubernetes/oci/variables.tf:320-369`
- Modify: `terraform/modules/kubernetes/oci/outputs.tf:72-76`

- [ ] **Step 1: Remove all tmi-tf-wh K8s resources from k8s_resources.tf**

Delete lines 563-743 of `terraform/modules/kubernetes/oci/k8s_resources.tf` — the entire block from the section comment through the service resource:

```hcl
# ============================================================================
# Optional: tmi-tf-wh Webhook Analyzer (when enabled)
# ============================================================================

# ServiceAccount for tmi-tf-wh (enables OKE Workload Identity)
resource "kubernetes_service_account_v1" "tmi_tf_wh" {
  ...
}

# ConfigMap for tmi-tf-wh (non-sensitive environment variables)
resource "kubernetes_config_map_v1" "tmi_tf_wh" {
  ...
}

# tmi-tf-wh Deployment
resource "kubernetes_deployment_v1" "tmi_tf_wh" {
  ...
}

# tmi-tf-wh Service (always ClusterIP — accessed internally or via ingress)
resource "kubernetes_service_v1" "tmi_tf_wh" {
  ...
}
```

(Full content shown in the spec — lines 563-743 inclusive.)

- [ ] **Step 2: Remove tmi-tf-wh variables from variables.tf**

Delete lines 320-369 of `terraform/modules/kubernetes/oci/variables.tf`:

```hcl
# ---------------------------------------------------------------------------
# tmi-tf-wh Webhook Analyzer (optional)
# ---------------------------------------------------------------------------
variable "tmi_tf_wh_enabled" { ... }
variable "tmi_tf_wh_image_url" { ... }
variable "tmi_tf_wh_queue_ocid" { ... }
variable "tmi_tf_wh_cpu_request" { ... }
variable "tmi_tf_wh_memory_request" { ... }
variable "tmi_tf_wh_cpu_limit" { ... }
variable "tmi_tf_wh_memory_limit" { ... }
variable "tmi_tf_wh_extra_env_vars" { ... }
```

- [ ] **Step 3: Remove tmi-tf-wh output from outputs.tf**

Delete lines 72-76 of `terraform/modules/kubernetes/oci/outputs.tf`:

```hcl
# tmi-tf-wh (always ClusterIP now, no LB IP)
output "tmi_tf_wh_load_balancer_ip" {
  description = "Deprecated: tmi-tf-wh is always ClusterIP. Returns null."
  value       = null
}
```

- [ ] **Step 4: Commit**

```bash
git add terraform/modules/kubernetes/oci/
git commit -m "refactor(terraform): remove tmi-tf-wh from OCI kubernetes module"
```

---

### Task 5: Remove webhook phase from setup-oci-public.py

**Files:**
- Modify: `scripts/setup-oci-public.py:16` (description comment)
- Modify: `scripts/setup-oci-public.py:24` (usage line)
- Modify: `scripts/setup-oci-public.py:400-423` (verify phase tmi-tf-wh pod check)
- Modify: `scripts/setup-oci-public.py:1033-1061` (webhook helper functions)
- Modify: `scripts/setup-oci-public.py:1063-1222` (webhook command)
- Modify: `scripts/setup-oci-public.py:1233-1257` (run_all references)

- [ ] **Step 1: Remove tmi-tf-wh from script docstring**

In the docstring at the top of the file, remove the tmi-tf-wh line and the webhook usage line.

Change line 16 from:
```python
  - tmi-tf-wh webhook registration and client credentials
```
Remove this line entirely.

Change line 24 from:
```python
    uv run scripts/setup-oci-public.py webhook
```
Remove this line entirely.

- [ ] **Step 2: Remove tmi-tf-wh pod check from verify command**

Delete lines 400-423 from the `verify` command — the block that checks tmi-tf-wh pods:

```python
    # Check tmi-tf-wh pods (optional)
    result = kubectl_cmd(
        cfg, ["get", "deployment", "tmi-tf-wh", "-o", "name"], check=False
    )
    if result.returncode == 0:
        result2 = kubectl_cmd(
            cfg,
            [
                "get",
                "pods",
                "-l",
                "app=tmi-tf-wh",
                "-o",
                "jsonpath={.items[*].status.phase}",
            ],
            check=False,
        )
        if result2.returncode == 0 and "Running" in (result2.stdout or ""):
            click.echo("  [OK] tmi-tf-wh pods running")
        else:
            errors.append("tmi-tf-wh deployment exists but pods are not running")
            click.echo("  [FAIL] tmi-tf-wh pods not running")
    else:
        click.echo("  [SKIP] tmi-tf-wh not deployed")
```

- [ ] **Step 3: Remove webhook helper functions**

Delete lines 1030-1061 — the `find_webhook_subscription` and `wait_for_webhook_active` functions plus their preceding comment:

```python
# ---------------------------------------------------------------------------


def find_webhook_subscription(token: str, api_url: str, name: str) -> dict | None:
    ...

def wait_for_webhook_active(
    token: str, api_url: str, webhook_id: str, timeout: int = 180, interval: int = 15
) -> bool:
    ...
```

- [ ] **Step 4: Remove the webhook command**

Delete lines 1063-1222 — the entire `webhook` command function:

```python
@cli.command()
@click.pass_context
def webhook(ctx):
    """Phase 4: Register tmi-tf-wh webhook and provision client credentials."""
    ...
    click.echo("\nWebhook registration complete.")
```

- [ ] **Step 5: Update run_all to remove webhook phase**

In the `run_all` function, change the phases list (line 1234) from:

```python
    phases = [
        ("verify", verify),
        ("dns", dns),
        ("certs", certs),
        ("configure", configure),
        ("webhook", webhook),
    ]
```

To:

```python
    phases = [
        ("verify", verify),
        ("dns", dns),
        ("certs", certs),
        ("configure", configure),
    ]
```

Also update the docstring (line 1233) from:

```python
    """Run all phases in order: verify -> dns -> certs -> configure -> webhook."""
```

To:

```python
    """Run all phases in order: verify -> dns -> certs -> configure."""
```

And remove the tmi-tf-wh line from the completion message (line 1257):

```python
        click.echo("  tmi-tf-wh webhook registered and active.")
```

Remove this line entirely.

- [ ] **Step 6: Commit**

```bash
git add scripts/setup-oci-public.py
git commit -m "refactor(scripts): remove tmi-tf-wh webhook phase from setup-oci-public.py"
```

---

### Task 6: Remove tmi-tf-wh from deploy-oci.sh and Makefile

**Files:**
- Modify: `scripts/deploy-oci.sh:298-332` (print_external_container_info)
- Modify: `scripts/deploy-oci.sh:368` (print_push_env)
- Modify: `Makefile:1180`

- [ ] **Step 1: Remove tmi-tf-wh from print_external_container_info function**

In `scripts/deploy-oci.sh`, update the `print_external_container_info` function.

Delete lines 299 (update comment) and 312-314 (tmi_tf_wh_enabled variable) and 326-332 (the if block):

Change the function comment (line 299) from:
```bash
# Print OCIR push instructions for external containers (tmi-ux, tmi-tf-wh)
```
To:
```bash
# Print OCIR push instructions for external containers (tmi-ux)
```

Delete lines 312-314:
```bash
    local tmi_tf_wh_enabled
    tmi_tf_wh_enabled=$(grep '^tmi_tf_wh_enabled' "$TF_DIR/terraform.tfvars" 2>/dev/null \
        | sed 's/.*= *//' | tr -d ' ' || echo "false")
```

Delete lines 326-332:
```bash
    if [[ "$tmi_tf_wh_enabled" == "true" ]]; then
        has_external=true
        echo ""
        log_info "tmi-tf-wh is enabled. Push the tmi-tf-wh container image to:"
        echo -e "  ${BOLD}${registry}/${namespace}/${name_prefix}/tmi-tf-wh:latest${NC}"
        echo -e "  From the tmi-tf-wh repo: docker buildx build --platform linux/arm64 --push -t ${registry}/${namespace}/${name_prefix}/tmi-tf-wh:latest ."
    fi
```

- [ ] **Step 2: Remove tmi-tf-wh from print_push_env function**

Delete line 368 from `scripts/deploy-oci.sh`:

```bash
    echo "export TMI_TF_WH_IMAGE_URL=${registry}/${namespace}/${name_prefix}/tmi-tf-wh:latest"
```

- [ ] **Step 3: Update Makefile push-oci-info target description**

Change line 1180 of `Makefile` from:

```makefile
push-oci-info:  ## Show OCIR push instructions for external containers (tmi-ux, tmi-tf-wh)
```

To:

```makefile
push-oci-info:  ## Show OCIR push instructions for external containers (tmi-ux)
```

- [ ] **Step 4: Commit**

```bash
git add scripts/deploy-oci.sh Makefile
git commit -m "refactor(scripts): remove tmi-tf-wh from deploy-oci.sh and Makefile"
```

---

### Task 7: Verify and final validation

- [ ] **Step 1: Grep for any remaining tmi-tf-wh references**

Run:
```bash
grep -r "tmi.tf.wh\|tmi-tf-wh\|tf_wh" terraform/ scripts/ Makefile
```

Expected: No matches.

- [ ] **Step 2: Run Terraform validate on both environments**

Run:
```bash
cd terraform/environments/oci-public && terraform validate
cd terraform/environments/oci-private && terraform validate
```

Expected: Both pass with "Success! The configuration is valid."

Note: This requires Terraform to be installed and providers to be initialized. If providers are not initialized, this step can be skipped — the structural correctness is verified by the grep in Step 1.

- [ ] **Step 3: Commit (if any fixes were needed)**

Only if steps 1-2 revealed issues that needed fixing:

```bash
git add -A
git commit -m "fix(terraform): address tmi-tf-wh removal validation issues"
```
