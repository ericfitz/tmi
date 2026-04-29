# Remove tmi-tf-wh from Terraform Templates

**Date**: 2026-03-30
**Status**: Approved

## Summary

Remove the tmi-tf-wh (webhook analyzer) infrastructure from the TMI Terraform templates. The tmi-tf-wh service will be deployed separately going forward. Existing users who deployed tmi-tf-wh via these templates need a migration path to tear down the resources before updating to the new templates.

## Motivation

The tmi-tf-wh service is being decoupled from the TMI core infrastructure to allow independent deployment and lifecycle management. Keeping it in the TMI Terraform templates creates unnecessary coupling and complexity for users who don't need the webhook analyzer.

## Scope

tmi-tf-wh resources exist only in OCI environments (oci-public, oci-private) and the shared OCI kubernetes module. No AWS/GCP/Azure templates are affected.

## Approach: Two-Phase Removal

Both phases ship together in a single change. The migration README bridges the gap for existing deployers.

### Phase 1: Migration README

A `terraform/MIGRATE-TMI-TF-WH.md` file instructs existing deployers to:

1. Set `tmi_tf_wh_enabled = false` in `terraform.tfvars`
2. Run `terraform plan` to verify only tmi-tf-wh resources will be destroyed (container repo, queue, IAM policy statements, K8s service account/configmap/deployment/service)
3. Run `terraform apply`
4. Remove the `tmi_tf_wh_*` variables from `terraform.tfvars`
5. Update to the latest templates (which no longer contain tmi-tf-wh)

### Phase 2: Code Removal

#### Terraform Environments (oci-public & oci-private)

**main.tf** — remove:
- `oci_artifacts_container_repository.tmi_tf_wh` (container image repository)
- `oci_queue_queue.tmi_tf_wh` (job dispatch queue)
- IAM policy statements referencing tmi-tf-wh queue and generative-ai-family
- `tmi_tf_wh_*` pass-through variables in the kubernetes module call
- Resource sizing overrides for tmi-tf-wh (oci-public only)

**variables.tf** — remove:
- `tmi_tf_wh_enabled`
- `tmi_tf_wh_image_url`
- `tmi_tf_wh_extra_env_vars`

**outputs.tf** — remove:
- `tmi_tf_wh_load_balancer_ip`

**terraform.tfvars.example** — remove:
- Commented-out `tmi_tf_wh_enabled` and `tmi_tf_wh_image_url` examples

#### Kubernetes Module (kubernetes/oci/)

**k8s_resources.tf** — remove:
- `kubernetes_service_account_v1.tmi_tf_wh`
- `kubernetes_config_map_v1.tmi_tf_wh`
- `kubernetes_deployment_v1.tmi_tf_wh`
- `kubernetes_service_v1.tmi_tf_wh`

**variables.tf** — remove:
- All `tmi_tf_wh_*` variable declarations (enabled, image_url, queue_ocid, cpu_request, memory_request, cpu_limit, memory_limit, extra_env_vars)

**outputs.tf** — remove:
- `tmi_tf_wh_load_balancer_ip`

#### Scripts and Makefile

**scripts/setup-oci-public.py** — remove:
- Phase 4 entirely (webhook registration, client credentials creation, health checks)
- tmi-tf-wh mention from script description/comments
- tmi-tf-wh pod check from health/status section

**scripts/deploy-oci.sh** — remove:
- tmi-tf-wh OCIR push instructions from the push-oci-info function

**Makefile** — update:
- `push-oci-info` target description to remove tmi-tf-wh mention

## Resources Destroyed by Migration

When a user sets `tmi_tf_wh_enabled = false` and applies, Terraform will destroy:

| Resource | Type | Description |
|----------|------|-------------|
| `oci_artifacts_container_repository.tmi_tf_wh` | OCI | Container image repository |
| `oci_queue_queue.tmi_tf_wh` | OCI | Job dispatch queue |
| IAM policy statements | OCI | Queue access and generative-ai-family permissions |
| `kubernetes_service_account_v1.tmi_tf_wh` | K8s | Workload Identity service account |
| `kubernetes_config_map_v1.tmi_tf_wh` | K8s | Environment configuration |
| `kubernetes_deployment_v1.tmi_tf_wh` | K8s | Pod deployment |
| `kubernetes_service_v1.tmi_tf_wh` | K8s | ClusterIP service |

## What Is NOT Changed

- The TMI API's webhook endpoint handlers remain (webhooks are a core feature)
- Non-OCI Terraform environments are unaffected (they never had tmi-tf-wh)
- The tmi-tf-wh service itself continues to exist as a separate project
