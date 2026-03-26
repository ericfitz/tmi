# Kubernetes Resources for TMI on OKE
# Manages Deployments, Services, ConfigMaps, and Secrets via Terraform kubernetes provider

# Namespace
resource "kubernetes_namespace_v1" "tmi" {
  metadata {
    name = "tmi"
    labels = {
      app        = "tmi"
      managed_by = "terraform"
    }
  }

  depends_on = [oci_containerengine_node_pool.tmi]
}

# ConfigMap (non-sensitive environment variables)
resource "kubernetes_config_map_v1" "tmi" {
  metadata {
    name      = "tmi-config"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  data = merge(
    {
      TMI_BUILD_MODE             = var.tmi_build_mode
      TMI_LOG_LEVEL              = var.log_level
      TMI_SERVER_ADDRESS         = "0.0.0.0:8080"
      TMI_ORACLE_WALLET_LOCATION = "/wallet"
      TMI_SECRETS_PROVIDER       = "oci"
      TMI_SECRETS_OCI_VAULT_OCID = var.vault_ocid
      TMI_LOG_DIR                = "/tmp"

      # Redis accessed via K8s ClusterIP service
      TMI_REDIS_URL = "redis://:${urlencode(var.redis_password)}@tmi-redis:6379"

      # OAuth provider configuration
      OAUTH_PROVIDERS_TMI_ENABLED       = "true"
      OAUTH_PROVIDERS_TMI_CLIENT_ID     = "tmi-oci-deployment"
      OAUTH_PROVIDERS_TMI_CLIENT_SECRET = var.oauth_client_secret != "" ? var.oauth_client_secret : var.jwt_secret
    },
    var.extra_environment_variables
  )
}

# Secret (sensitive values)
resource "kubernetes_secret_v1" "tmi" {
  metadata {
    name      = "tmi-secrets"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  data = {
    TMI_DATABASE_URL = "oracle://${var.db_username}:${urlencode(var.db_password)}@${var.oracle_connect_string}"
    TMI_JWT_SECRET   = var.jwt_secret
  }
}

# Secret for Oracle wallet (binary data)
resource "kubernetes_secret_v1" "wallet" {
  metadata {
    name      = "oracle-wallet"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  binary_data = {
    "wallet.zip" = var.wallet_base64
  }
}

# ServiceAccount for TMI API (enables OKE Workload Identity for OCI service access)
resource "kubernetes_service_account_v1" "tmi_api" {
  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app        = "tmi-api"
      managed_by = "terraform"
    }
  }

  automount_service_account_token = true
}

# TMI API Deployment
resource "kubernetes_deployment_v1" "tmi_api" {
  wait_for_rollout = false

  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-api"
      component = "api"
    }
  }

  spec {
    replicas = 1 # TMI apps are single-instance only; do not increase

    selector {
      match_labels = {
        app = "tmi-api"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app       = "tmi-api"
          component = "api"
        }
      }

      spec {
        service_account_name            = kubernetes_service_account_v1.tmi_api.metadata[0].name
        automount_service_account_token = true

        # Init container to extract Oracle wallet zip into a shared emptyDir
        init_container {
          name  = "wallet-extract"
          image = "container-registry.oracle.com/os/oraclelinux:9"
          command = [
            "sh", "-c",
            "dnf -y -q install unzip && cp /wallet-zip/wallet.zip /tmp/wallet.zip && cd /wallet-extracted && unzip -o /tmp/wallet.zip && sed -i 's|DIRECTORY=\"?/network/admin\"|DIRECTORY=\"/wallet\"|' /wallet-extracted/sqlnet.ora"
          ]

          volume_mount {
            name       = "wallet-zip"
            mount_path = "/wallet-zip"
            read_only  = true
          }

          volume_mount {
            name       = "wallet-extracted"
            mount_path = "/wallet-extracted"
          }
        }

        container {
          name  = "tmi-api"
          image = var.tmi_image_url

          port {
            name           = "http"
            container_port = 8080
            protocol       = "TCP"
          }

          env_from {
            config_map_ref {
              name = kubernetes_config_map_v1.tmi.metadata[0].name
            }
          }

          env_from {
            secret_ref {
              name = kubernetes_secret_v1.tmi.metadata[0].name
            }
          }

          volume_mount {
            name       = "wallet-extracted"
            mount_path = "/wallet"
            read_only  = true
          }

          volume_mount {
            name       = "tmp"
            mount_path = "/tmp"
          }

          liveness_probe {
            http_get {
              path = "/"
              port = "http"
            }
            initial_delay_seconds = 60
            period_seconds        = 30
            timeout_seconds       = 10
            failure_threshold     = 3
          }

          readiness_probe {
            http_get {
              path = "/"
              port = "http"
            }
            initial_delay_seconds = 10
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          resources {
            requests = {
              cpu    = var.tmi_cpu_request
              memory = var.tmi_memory_request
            }
            limits = {
              cpu    = var.tmi_cpu_limit
              memory = var.tmi_memory_limit
            }
          }
        }

        volume {
          name = "wallet-zip"
          secret {
            secret_name = kubernetes_secret_v1.wallet.metadata[0].name
          }
        }

        volume {
          name = "wallet-extracted"
          empty_dir {}
        }

        volume {
          name = "tmp"
          empty_dir {}
        }

        termination_grace_period_seconds = 60
        restart_policy                   = "Always"
      }
    }
  }
}

# Redis Deployment (separate pod, accessed via ClusterIP service)
resource "kubernetes_deployment_v1" "redis" {
  wait_for_rollout = false

  metadata {
    name      = "tmi-redis"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-redis"
      component = "cache"
    }
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app = "tmi-redis"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app       = "tmi-redis"
          component = "cache"
        }
      }

      spec {
        container {
          name  = "redis"
          image = var.redis_image_url

          # Official Redis image requires --requirepass flag for authentication
          command = ["redis-server", "--requirepass", var.redis_password, "--port", "6379"]

          port {
            container_port = 6379
            protocol       = "TCP"
          }

          liveness_probe {
            tcp_socket {
              port = 6379
            }
            initial_delay_seconds = 15
            period_seconds        = 30
            timeout_seconds       = 10
            failure_threshold     = 3
          }

          readiness_probe {
            tcp_socket {
              port = 6379
            }
            initial_delay_seconds = 5
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          resources {
            requests = {
              cpu    = var.redis_cpu_request
              memory = var.redis_memory_request
            }
            limits = {
              cpu    = var.redis_cpu_limit
              memory = var.redis_memory_limit
            }
          }
        }

        termination_grace_period_seconds = 30
        restart_policy                   = "Always"
      }
    }
  }
}

# Redis ClusterIP Service (internal only)
resource "kubernetes_service_v1" "redis" {
  metadata {
    name      = "tmi-redis"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-redis"
      component = "cache"
    }
  }

  spec {
    selector = {
      app = "tmi-redis"
    }

    port {
      name        = "redis"
      port        = 6379
      target_port = 6379
      protocol    = "TCP"
    }

    type = "ClusterIP"
  }
}

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

# TLS Secret for SSL certificate (when provided)
resource "kubernetes_secret_v1" "tls" {
  count = var.ssl_certificate_pem != null ? 1 : 0

  metadata {
    name      = "tmi-tls"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  type = "kubernetes.io/tls"

  data = {
    "tls.crt" = var.ssl_certificate_pem
    "tls.key" = var.ssl_private_key_pem
  }
}

# ============================================================================
# Optional: TMI-UX Frontend (when enabled)
# ============================================================================

# TMI-UX Deployment
resource "kubernetes_deployment_v1" "tmi_ux" {
  count = var.tmi_ux_enabled ? 1 : 0

  wait_for_rollout = false

  metadata {
    name      = "tmi-ux"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-ux"
      component = "frontend"
    }
  }

  spec {
    replicas = 1 # TMI apps are single-instance only; do not increase

    selector {
      match_labels = {
        app = "tmi-ux"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app       = "tmi-ux"
          component = "frontend"
        }
      }

      spec {
        container {
          name  = "tmi-ux"
          image = var.tmi_ux_image_url

          port {
            name           = "http"
            container_port = 8080
            protocol       = "TCP"
          }

          env {
            name  = "PORT"
            value = "8080"
          }

          env {
            name  = "NODE_ENV"
            value = "production"
          }

          liveness_probe {
            http_get {
              path = "/"
              port = "http"
            }
            initial_delay_seconds = 30
            period_seconds        = 30
            timeout_seconds       = 10
            failure_threshold     = 3
          }

          readiness_probe {
            http_get {
              path = "/"
              port = "http"
            }
            initial_delay_seconds = 5
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          resources {
            requests = {
              cpu    = "250m"
              memory = "512Mi"
            }
            limits = {
              cpu    = "1"
              memory = "2Gi"
            }
          }
        }

        termination_grace_period_seconds = 30
        restart_policy                   = "Always"
      }
    }
  }
}

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

# ============================================================================
# Optional: tmi-tf-wh Webhook Analyzer (when enabled)
# ============================================================================

# ServiceAccount for tmi-tf-wh (enables OKE Workload Identity)
resource "kubernetes_service_account_v1" "tmi_tf_wh" {
  count = var.tmi_tf_wh_enabled ? 1 : 0

  metadata {
    name      = "tmi-tf-wh"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app        = "tmi-tf-wh"
      managed_by = "terraform"
    }
  }

  automount_service_account_token = true
}

# ConfigMap for tmi-tf-wh (non-sensitive environment variables)
resource "kubernetes_config_map_v1" "tmi_tf_wh" {
  count = var.tmi_tf_wh_enabled ? 1 : 0

  metadata {
    name      = "tmi-tf-wh-config"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  data = merge(
    {
      LLM_PROVIDER        = "oci"
      OCI_COMPARTMENT_ID  = var.compartment_id
      QUEUE_OCID          = var.tmi_tf_wh_queue_ocid
      VAULT_OCID          = var.vault_ocid
      TMI_SERVER_URL      = "http://tmi-api:8080"
      TMI_OAUTH_IDP       = "tmi"
      TMI_CLIENT_PATH     = "/opt/tmi-client"
      MAX_CONCURRENT_JOBS = "3"
      JOB_TIMEOUT         = "3600"
      SERVER_PORT         = "8080"
    },
    var.tmi_tf_wh_extra_env_vars
  )
}

# tmi-tf-wh Deployment
resource "kubernetes_deployment_v1" "tmi_tf_wh" {
  count = var.tmi_tf_wh_enabled ? 1 : 0

  wait_for_rollout = false

  metadata {
    name      = "tmi-tf-wh"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-tf-wh"
      component = "webhook-analyzer"
    }
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app = "tmi-tf-wh"
      }
    }

    strategy {
      type = "Recreate"
    }

    template {
      metadata {
        labels = {
          app       = "tmi-tf-wh"
          component = "webhook-analyzer"
        }
      }

      spec {
        service_account_name            = kubernetes_service_account_v1.tmi_tf_wh[0].metadata[0].name
        automount_service_account_token = true

        container {
          name  = "tmi-tf-wh"
          image = var.tmi_tf_wh_image_url

          port {
            name           = "http"
            container_port = 8080
            protocol       = "TCP"
          }

          env_from {
            config_map_ref {
              name = kubernetes_config_map_v1.tmi_tf_wh[0].metadata[0].name
            }
          }

          volume_mount {
            name       = "tmp"
            mount_path = "/tmp"
          }

          liveness_probe {
            http_get {
              path = "/health"
              port = "http"
            }
            initial_delay_seconds = 60
            period_seconds        = 30
            timeout_seconds       = 10
            failure_threshold     = 3
          }

          readiness_probe {
            http_get {
              path = "/health"
              port = "http"
            }
            initial_delay_seconds = 10
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 3
          }

          resources {
            requests = {
              cpu    = var.tmi_tf_wh_cpu_request
              memory = var.tmi_tf_wh_memory_request
            }
            limits = {
              cpu    = var.tmi_tf_wh_cpu_limit
              memory = var.tmi_tf_wh_memory_limit
            }
          }
        }

        volume {
          name = "tmp"
          empty_dir {}
        }

        termination_grace_period_seconds = 60
        restart_policy                   = "Always"
      }
    }
  }
}

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
