# OCI Native Ingress Controller Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-service OCI Load Balancers with a single LB managed by the OCI Native Ingress Controller, enabling host-based routing within the Always Free tier.

**Architecture:** Add the OCI Native Ingress Controller as an OKE cluster add-on, change tmi-api/tmi-ux/tmi-tf-wh Services from LoadBalancer to ClusterIP, and create a Kubernetes Ingress resource with host-based routing rules. The ingress controller pod manages a single OCI Flexible LB (10 Mbps free tier) that routes traffic based on the `Host` header.

**Tech Stack:** Terraform (OCI provider, Kubernetes provider), OCI Native Ingress Controller, Kubernetes Ingress resources

**Spec:** `docs/superpowers/specs/2026-03-26-oci-public-post-deploy-setup-design.md` — Part 1

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `terraform/modules/kubernetes/oci/variables.tf` | Modify | Add `api_hostname`, `ux_hostname` variables |
| `terraform/modules/kubernetes/oci/main.tf` | Modify | Add `oci_containerengine_addon` for ingress controller |
| `terraform/modules/kubernetes/oci/k8s_resources.tf` | Modify | Change services to ClusterIP, add Ingress resource, add IngressClass |
| `terraform/modules/kubernetes/oci/outputs.tf` | Modify | Update outputs for ingress-based architecture |
| `terraform/environments/oci-public/main.tf` | Modify | Pass new variables to kubernetes module |
| `terraform/environments/oci-public/variables.tf` | Modify | Add `api_hostname`, `ux_hostname` variables |
| `terraform/environments/oci-public/terraform.tfvars.example` | Modify | Document new variables |

---

### Task 1: Add ingress hostname variables to the kubernetes module

**Files:**
- Modify: `terraform/modules/kubernetes/oci/variables.tf`

- [ ] **Step 1: Add `api_hostname` and `ux_hostname` variables**

Add at the end of `terraform/modules/kubernetes/oci/variables.tf`:

```hcl
# ---------------------------------------------------------------------------
# Ingress Configuration
# ---------------------------------------------------------------------------
variable "api_hostname" {
  description = "Hostname for API ingress rule (e.g., api.oci.tmi.dev). When set, enables ingress-based routing instead of per-service LoadBalancers."
  type        = string
  default     = null
}

variable "ux_hostname" {
  description = "Hostname for UX frontend ingress rule (e.g., app.oci.tmi.dev). Only used when api_hostname is also set and tmi_ux_enabled is true."
  type        = string
  default     = null
}
```

- [ ] **Step 2: Validate terraform syntax**

Run: `cd terraform/modules/kubernetes/oci && terraform validate`
Expected: Success (or warnings about missing provider config, which is fine for module-level validation)

- [ ] **Step 3: Commit**

```bash
git add terraform/modules/kubernetes/oci/variables.tf
git commit -m "feat(terraform): add ingress hostname variables to kubernetes module"
```

---

### Task 2: Add OCI Native Ingress Controller add-on

**Files:**
- Modify: `terraform/modules/kubernetes/oci/main.tf`

- [ ] **Step 1: Add the ingress controller add-on resource**

Add after the `oci_containerengine_node_pool` resource in `terraform/modules/kubernetes/oci/main.tf`:

```hcl
# OCI Native Ingress Controller Add-on (enabled when hostnames are configured)
resource "oci_containerengine_addon" "native_ingress_controller" {
  count = var.api_hostname != null ? 1 : 0

  addon_name                = "NativeIngressController"
  cluster_id                = oci_containerengine_cluster.tmi.id
  remove_addon_resources_on_delete = true

  configurations {
    key   = "lbSubnetId"
    value = var.public_subnet_ids[0]
  }

  configurations {
    key   = "isLBPublic"
    value = tostring(var.lb_public)
  }

  configurations {
    key   = "minBandwidthMbps"
    value = tostring(var.lb_min_bandwidth_mbps)
  }

  configurations {
    key   = "maxBandwidthMbps"
    value = tostring(var.lb_max_bandwidth_mbps)
  }

  configurations {
    key   = "nsgIds"
    value = join(",", var.lb_nsg_ids)
  }

  depends_on = [oci_containerengine_node_pool.tmi]
}
```

- [ ] **Step 2: Commit**

```bash
git add terraform/modules/kubernetes/oci/main.tf
git commit -m "feat(terraform): add OCI Native Ingress Controller add-on"
```

---

### Task 3: Change services from LoadBalancer to ClusterIP

**Files:**
- Modify: `terraform/modules/kubernetes/oci/k8s_resources.tf`

- [ ] **Step 1: Update tmi-api service**

Replace the `kubernetes_service_v1.tmi_api` resource (lines 348-394) with:

```hcl
# TMI API Service
# When ingress is enabled (api_hostname set): ClusterIP for ingress routing
# When ingress is disabled: LoadBalancer with OCI LB annotations (legacy)
resource "kubernetes_service_v1" "tmi_api" {
  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-api"
      component = "api"
    }
    annotations = var.api_hostname != null ? {} : merge(
      {
        "oci.oraclecloud.com/load-balancer-type"                                     = "lb"
        "service.beta.kubernetes.io/oci-load-balancer-shape"                         = "flexible"
        "service.beta.kubernetes.io/oci-load-balancer-shape-flex-min"                = tostring(var.lb_min_bandwidth_mbps)
        "service.beta.kubernetes.io/oci-load-balancer-shape-flex-max"                = tostring(var.lb_max_bandwidth_mbps)
        "service.beta.kubernetes.io/oci-load-balancer-security-list-management-mode" = "None"
        "oci.oraclecloud.com/oci-network-security-groups"                            = join(",", var.lb_nsg_ids)
      },
      !var.lb_public ? {
        "service.beta.kubernetes.io/oci-load-balancer-internal" = "true"
      } : {},
      var.ssl_certificate_pem != null ? {
        "service.beta.kubernetes.io/oci-load-balancer-ssl-ports"               = "443"
        "service.beta.kubernetes.io/oci-load-balancer-tls-secret"              = "tmi-tls"
        "service.beta.kubernetes.io/oci-load-balancer-backend-protocol"        = "HTTP"
        "service.beta.kubernetes.io/oci-load-balancer-connection-idle-timeout" = "300"
      } : {}
    )
  }

  spec {
    selector = {
      app = "tmi-api"
    }

    port {
      name        = "http"
      port        = var.api_hostname != null ? 8080 : (var.ssl_certificate_pem != null ? 443 : 80)
      target_port = 8080
      protocol    = "TCP"
    }

    type = var.api_hostname != null ? "ClusterIP" : "LoadBalancer"
  }
}
```

- [ ] **Step 2: Update tmi-ux service**

Replace the `kubernetes_service_v1.tmi_ux` resource (lines 515-563) with:

```hcl
# TMI-UX Service
resource "kubernetes_service_v1" "tmi_ux" {
  count = var.tmi_ux_enabled ? 1 : 0

  metadata {
    name      = "tmi-ux"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-ux"
      component = "frontend"
    }
    annotations = var.api_hostname != null ? {} : merge(
      {
        "oci.oraclecloud.com/load-balancer-type"                                     = "lb"
        "service.beta.kubernetes.io/oci-load-balancer-shape"                         = "flexible"
        "service.beta.kubernetes.io/oci-load-balancer-shape-flex-min"                = tostring(var.lb_min_bandwidth_mbps)
        "service.beta.kubernetes.io/oci-load-balancer-shape-flex-max"                = tostring(var.lb_max_bandwidth_mbps)
        "service.beta.kubernetes.io/oci-load-balancer-security-list-management-mode" = "None"
        "oci.oraclecloud.com/oci-network-security-groups"                            = join(",", var.lb_nsg_ids)
      },
      !var.lb_public ? {
        "service.beta.kubernetes.io/oci-load-balancer-internal" = "true"
      } : {},
      var.ssl_certificate_pem != null ? {
        "service.beta.kubernetes.io/oci-load-balancer-ssl-ports"               = "443"
        "service.beta.kubernetes.io/oci-load-balancer-tls-secret"              = "tmi-ux-tls"
        "service.beta.kubernetes.io/oci-load-balancer-backend-protocol"        = "HTTP"
        "service.beta.kubernetes.io/oci-load-balancer-connection-idle-timeout" = "300"
      } : {}
    )
  }

  spec {
    selector = {
      app = "tmi-ux"
    }

    port {
      name        = "http"
      port        = var.api_hostname != null ? 8080 : (var.ssl_certificate_pem != null ? 443 : 80)
      target_port = 8080
      protocol    = "TCP"
    }

    type = var.api_hostname != null ? "ClusterIP" : "LoadBalancer"
  }
}
```

- [ ] **Step 3: Update tmi-tf-wh service to always be ClusterIP**

Replace the `kubernetes_service_v1.tmi_tf_wh` resource (lines 718-755) with:

```hcl
# tmi-tf-wh Service (always ClusterIP — accessed internally or via ingress)
resource "kubernetes_service_v1" "tmi_tf_wh" {
  count = var.tmi_tf_wh_enabled ? 1 : 0

  metadata {
    name      = "tmi-tf-wh"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-tf-wh"
      component = "webhook-analyzer"
    }
  }

  spec {
    selector = {
      app = "tmi-tf-wh"
    }

    port {
      name        = "http"
      port        = 8080
      target_port = 8080
      protocol    = "TCP"
    }

    type = "ClusterIP"
  }
}
```

- [ ] **Step 4: Commit**

```bash
git add terraform/modules/kubernetes/oci/k8s_resources.tf
git commit -m "refactor(terraform): make services ClusterIP when ingress is enabled"
```

---

### Task 4: Add IngressClass and Ingress resources

**Files:**
- Modify: `terraform/modules/kubernetes/oci/k8s_resources.tf`

- [ ] **Step 1: Add IngressClass and Ingress resources**

Add at the end of `terraform/modules/kubernetes/oci/k8s_resources.tf`:

```hcl
# ============================================================================
# Ingress Resources (when hostname-based routing is enabled)
# ============================================================================

# IngressClass for OCI Native Ingress Controller
resource "kubernetes_ingress_class_v1" "oci" {
  count = var.api_hostname != null ? 1 : 0

  metadata {
    name = "oci-native-ic"
    annotations = {
      "ingressclass.kubernetes.io/is-default-class" = "true"
    }
  }

  spec {
    controller = "oci.oraclecloud.com/native-ingress-controller"
  }
}

# Ingress resource with host-based routing
resource "kubernetes_ingress_v1" "tmi" {
  count = var.api_hostname != null ? 1 : 0

  metadata {
    name      = "tmi-ingress"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    annotations = {
      "oci-native-ingress.oraclecloud.com/id" = "" # Let controller create a new LB
    }
  }

  spec {
    ingress_class_name = kubernetes_ingress_class_v1.oci[0].metadata[0].name

    # TLS configuration — references K8s secret created by setup script
    tls {
      hosts       = compact([var.api_hostname, var.ux_hostname])
      secret_name = "tmi-tls"
    }

    # API routing rule
    rule {
      host = var.api_hostname

      http {
        path {
          path      = "/"
          path_type = "Prefix"

          backend {
            service {
              name = kubernetes_service_v1.tmi_api.metadata[0].name
              port {
                number = 8080
              }
            }
          }
        }
      }
    }

    # UX routing rule (when enabled)
    dynamic "rule" {
      for_each = var.tmi_ux_enabled && var.ux_hostname != null ? [1] : []

      content {
        host = var.ux_hostname

        http {
          path {
            path      = "/"
            path_type = "Prefix"

            backend {
              service {
                name = kubernetes_service_v1.tmi_ux[0].metadata[0].name
                port {
                  number = 8080
                }
              }
            }
          }
        }
      }
    }
  }

  depends_on = [oci_containerengine_addon.native_ingress_controller]
}
```

- [ ] **Step 2: Commit**

```bash
git add terraform/modules/kubernetes/oci/k8s_resources.tf
git commit -m "feat(terraform): add IngressClass and Ingress for host-based routing"
```

---

### Task 5: Update module outputs

**Files:**
- Modify: `terraform/modules/kubernetes/oci/outputs.tf`

- [ ] **Step 1: Replace the outputs file**

The existing outputs reference `kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip` which won't exist when the service is ClusterIP. Replace the entire file with:

```hcl
# Outputs for OCI Kubernetes (OKE) Module

# OKE Cluster
output "cluster_id" {
  description = "OCID of the OKE cluster"
  value       = oci_containerengine_cluster.tmi.id
}

output "cluster_name" {
  description = "Name of the OKE cluster"
  value       = oci_containerengine_cluster.tmi.name
}

output "cluster_endpoint" {
  description = "Kubernetes API endpoint of the OKE cluster"
  value       = var.oke_public_endpoint ? "https://${oci_containerengine_cluster.tmi.endpoints[0].public_endpoint}" : "https://${oci_containerengine_cluster.tmi.endpoints[0].private_endpoint}"
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate for the OKE cluster"
  value       = base64decode(yamldecode(data.oci_containerengine_cluster_kube_config.tmi.content)["clusters"][0]["cluster"]["certificate-authority-data"])
  sensitive   = true
}

output "kubeconfig" {
  description = "Kubeconfig content for kubectl access"
  value       = data.oci_containerengine_cluster_kube_config.tmi.content
  sensitive   = true
}

# Node Pool
output "node_pool_id" {
  description = "OCID of the managed node pool"
  value       = oci_containerengine_node_pool.tmi.id
}

# Load Balancer IP — from ingress LB (discovered via setup script) or legacy per-service LB
output "load_balancer_ip" {
  description = "Public IP of the load balancer. When using ingress, this is null (LB IP discovered post-deploy via kubectl)."
  value       = var.api_hostname != null ? null : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip : null)
}

# Application URLs
output "http_url" {
  description = "HTTP URL for the application"
  value       = var.api_hostname != null ? "http://${var.api_hostname}" : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null)
}

output "https_url" {
  description = "HTTPS URL for the application"
  sensitive   = true
  value       = var.api_hostname != null ? "https://${var.api_hostname}" : (var.ssl_certificate_pem != null && length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "https://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null)
}

output "service_endpoint" {
  description = "Service endpoint URL (standard interface)"
  sensitive   = true
  value       = var.api_hostname != null ? "https://${var.api_hostname}" : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? (var.ssl_certificate_pem != null ? "https://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}") : null)
}

output "load_balancer_dns" {
  description = "Load balancer DNS/IP (standard interface)"
  value       = var.api_hostname != null ? var.api_hostname : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip : null)
}

# TMI-UX
output "tmi_ux_load_balancer_ip" {
  description = "IP of the TMI-UX load balancer (null when using ingress)"
  value       = var.api_hostname != null ? null : (var.tmi_ux_enabled && length(kubernetes_service_v1.tmi_ux) > 0 && length(kubernetes_service_v1.tmi_ux[0].status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_ux[0].status[0].load_balancer[0].ingress[0].ip : null)
}

# tmi-tf-wh (always ClusterIP now, no LB IP)
output "tmi_tf_wh_load_balancer_ip" {
  description = "Deprecated: tmi-tf-wh is always ClusterIP. Returns null."
  value       = null
}

# Namespace
output "namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = kubernetes_namespace_v1.tmi.metadata[0].name
}

# Ingress
output "ingress_enabled" {
  description = "Whether ingress-based routing is enabled"
  value       = var.api_hostname != null
}

output "api_hostname" {
  description = "API hostname (when ingress is enabled)"
  value       = var.api_hostname
}

output "ux_hostname" {
  description = "UX hostname (when ingress is enabled)"
  value       = var.ux_hostname
}
```

- [ ] **Step 2: Commit**

```bash
git add terraform/modules/kubernetes/oci/outputs.tf
git commit -m "refactor(terraform): update outputs for ingress-based architecture"
```

---

### Task 6: Wire up variables in the oci-public environment

**Files:**
- Modify: `terraform/environments/oci-public/variables.tf`
- Modify: `terraform/environments/oci-public/main.tf`
- Modify: `terraform/environments/oci-public/terraform.tfvars.example`

- [ ] **Step 1: Add hostname variables to oci-public/variables.tf**

Add in the "Certificate Automation" section of `terraform/environments/oci-public/variables.tf`, after the existing `domain_name` variable:

```hcl
variable "api_hostname" {
  description = "Hostname for API ingress (e.g., api.oci.tmi.dev). Enables single-LB ingress routing."
  type        = string
  default     = null
}

variable "ux_hostname" {
  description = "Hostname for UX frontend ingress (e.g., app.oci.tmi.dev)"
  type        = string
  default     = null
}
```

- [ ] **Step 2: Pass variables to kubernetes module in oci-public/main.tf**

Add to the `module "kubernetes"` block in `terraform/environments/oci-public/main.tf`, after the `extra_environment_variables` line:

```hcl
  # Ingress configuration (single LB with host-based routing)
  api_hostname = var.api_hostname
  ux_hostname  = var.ux_hostname
```

- [ ] **Step 3: Update terraform.tfvars.example**

Add after the TLS section:

```hcl
# ---------------------------------------------------------------------------
# Optional: Ingress (single LB with host-based routing, recommended for Always Free)
# ---------------------------------------------------------------------------
# When set, replaces per-service LoadBalancers with a single OCI LB managed
# by the OCI Native Ingress Controller. Both DNS names point to the same LB IP.
# api_hostname = "api.oci.tmi.dev"
# ux_hostname  = "app.oci.tmi.dev"
```

- [ ] **Step 4: Validate terraform**

Run: `cd terraform/environments/oci-public && terraform validate`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add terraform/environments/oci-public/variables.tf terraform/environments/oci-public/main.tf terraform/environments/oci-public/terraform.tfvars.example
git commit -m "feat(terraform): wire ingress hostname variables in oci-public environment"
```

---

### Task 7: Verify the complete terraform plan (manual)

This task is a manual verification step after deploying to OCI.

- [ ] **Step 1: Run terraform plan with ingress enabled**

Update your `terraform.tfvars` to include:
```hcl
api_hostname = "api.oci.tmi.dev"
ux_hostname  = "app.oci.tmi.dev"
```

Run: `make deploy-oci-plan`
Expected: Plan shows:
- OCI Native Ingress Controller add-on created
- tmi-api service changed to ClusterIP
- tmi-ux service changed to ClusterIP
- tmi-tf-wh service simplified to always ClusterIP
- IngressClass and Ingress resources created
- No unexpected resource deletions

- [ ] **Step 2: Note any issues for follow-up**

Review the plan output carefully. The ingress controller add-on may require specific OKE cluster version compatibility. Document any adjustments needed.
