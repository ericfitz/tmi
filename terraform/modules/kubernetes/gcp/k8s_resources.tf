# Kubernetes Resources for TMI on GKE Autopilot
# Manages Deployments, Services, ConfigMaps, Secrets, and BackendConfig

# Namespace
resource "kubernetes_namespace_v1" "tmi" {
  metadata {
    name = "tmi"
    labels = {
      app        = "tmi"
      managed_by = "terraform"
    }
  }

  depends_on = [google_container_cluster.tmi]
}

# ConfigMap (non-sensitive environment variables)
resource "kubernetes_config_map_v1" "tmi" {
  metadata {
    name      = "tmi-config"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  data = merge(
    {
      TMI_AUTH_BUILD_MODE                       = var.tmi_build_mode
      TMI_AUTH_AUTO_PROMOTE_FIRST_USER          = "true"
      TMI_LOGGING_ALSO_LOG_TO_CONSOLE           = "true"
      TMI_LOGGING_REDACT_AUTH_TOKENS            = "true"
      TMI_LOGGING_SUPPRESS_UNAUTHENTICATED_LOGS = "true"
      TMI_SERVER_INTERFACE                      = "0.0.0.0"
      TMI_SERVER_PORT                           = "8080"
      TMI_DATABASE_REDIS_HOST                   = "tmi-redis.tmi.svc.cluster.local"
    },
    # Public template (dev mode) gets verbose logging
    var.tmi_build_mode == "dev" ? {
      TMI_AUTH_EVERYONE_IS_A_REVIEWER    = "true"
      TMI_LOGGING_LOG_API_REQUESTS       = "true"
      TMI_LOGGING_LOG_API_RESPONSES      = "true"
      TMI_LOGGING_LOG_WEBSOCKET_MESSAGES = "true"
    } : {},
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
    TMI_DATABASE_URL            = "postgres://${var.db_username}:${urlencode(var.db_password)}@${var.db_host}:5432/${var.db_name}?sslmode=disable"
    TMI_JWT_SECRET              = var.jwt_secret
    TMI_DATABASE_REDIS_PASSWORD = var.redis_password
  }
}

# ServiceAccount for TMI API (enables GKE Workload Identity)
resource "kubernetes_service_account_v1" "tmi_api" {
  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app        = "tmi-api"
      managed_by = "terraform"
    }
    annotations = {
      "iam.gke.io/gcp-service-account" = google_service_account.tmi_workload.email
    }
  }

  automount_service_account_token = true
}

# BackendConfig for WebSocket timeout (GKE Ingress uses Google Cloud L7 LB)
resource "kubernetes_manifest" "backend_config" {
  manifest = {
    apiVersion = "cloud.google.com/v1"
    kind       = "BackendConfig"
    metadata = {
      name      = "tmi-backend-config"
      namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    }
    spec = {
      timeoutSec = 3600
      connectionDraining = {
        drainingTimeoutSec = 60
      }
    }
  }
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
    replicas = var.tmi_replicas

    selector {
      match_labels = {
        app = "tmi-api"
      }
    }

    strategy {
      type = "RollingUpdate"
      rolling_update {
        max_unavailable = "1"
        max_surge       = "1"
      }
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

# TMI API Service (LoadBalancer or ClusterIP depending on template)
resource "kubernetes_service_v1" "tmi_api" {
  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-api"
      component = "api"
    }
    annotations = merge(
      {
        "cloud.google.com/backend-config" = "{\"default\": \"tmi-backend-config\"}"
      },
      var.enable_internal_lb ? {
        "cloud.google.com/load-balancer-type" = "Internal"
      } : {}
    )
  }

  spec {
    selector = {
      app = "tmi-api"
    }

    port {
      name        = "http"
      port        = 80
      target_port = 8080
      protocol    = "TCP"
    }

    type = "LoadBalancer"
  }
}
