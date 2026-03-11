# Kubernetes Resources for TMI on AWS EKS
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

  depends_on = [aws_eks_fargate_profile.tmi]
}

# ConfigMap (non-sensitive environment variables)
resource "kubernetes_config_map_v1" "tmi" {
  metadata {
    name      = "tmi-config"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  data = merge(
    {
      TMI_BUILD_MODE              = var.tmi_build_mode
      TMI_LOG_LEVEL               = var.log_level
      TMI_SERVER_ADDRESS          = "0.0.0.0:8080"
      TMI_SECRETS_PROVIDER        = "aws"
      TMI_SECRETS_AWS_REGION      = var.aws_region
      TMI_SECRETS_AWS_SECRET_NAME = var.secrets_secret_name
      TMI_LOG_DIR                 = "/tmp"

      # Redis accessed via K8s ClusterIP service
      TMI_REDIS_URL = "redis://:${urlencode(var.redis_password)}@tmi-redis:6379"

      # OAuth provider configuration
      OAUTH_PROVIDERS_TMI_ENABLED       = "true"
      OAUTH_PROVIDERS_TMI_CLIENT_ID     = "tmi-aws-deployment"
      OAUTH_PROVIDERS_TMI_CLIENT_SECRET = var.jwt_secret
    },
    # CloudWatch logging configuration (only added if cloudwatch_log_group is set)
    var.cloudwatch_log_group != null ? {
      TMI_CLOUD_LOG_ENABLED    = "true"
      TMI_CLOUD_LOG_PROVIDER   = "aws"
      TMI_CLOUDWATCH_LOG_GROUP = var.cloudwatch_log_group
      TMI_CLOUD_LOG_LEVEL      = var.cloud_log_level != null ? var.cloud_log_level : var.log_level
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
    TMI_DATABASE_URL = "postgresql://${var.db_username}:${urlencode(var.db_password)}@${var.db_endpoint}/${var.db_name}?sslmode=require"
    TMI_JWT_SECRET   = var.jwt_secret
  }
}

# TMI API Deployment
resource "kubernetes_deployment_v1" "tmi_api" {
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

          port {
            container_port = 6379
            protocol       = "TCP"
          }

          env {
            name  = "REDIS_PASSWORD"
            value = var.redis_password
          }

          env {
            name  = "REDIS_PORT"
            value = "6379"
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

# TMI API LoadBalancer Service (auto-provisions AWS NLB)
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
        "service.beta.kubernetes.io/aws-load-balancer-type"            = "external"
        "service.beta.kubernetes.io/aws-load-balancer-nlb-target-type" = "ip"
        "service.beta.kubernetes.io/aws-load-balancer-scheme"          = "internet-facing"
      },
      # SSL annotations when ACM certificate ARN is provided
      var.ssl_certificate_arn != null ? {
        "service.beta.kubernetes.io/aws-load-balancer-ssl-cert"               = var.ssl_certificate_arn
        "service.beta.kubernetes.io/aws-load-balancer-ssl-ports"              = "443"
        "service.beta.kubernetes.io/aws-load-balancer-backend-protocol"       = "tcp"
        "service.beta.kubernetes.io/aws-load-balancer-ssl-negotiation-policy" = "ELBSecurityPolicy-TLS13-1-2-2021-06"
      } : {}
    )
  }

  spec {
    selector = {
      app = "tmi-api"
    }

    port {
      name        = "http"
      port        = var.ssl_certificate_arn != null ? 443 : 80
      target_port = 8080
      protocol    = "TCP"
    }

    type = "LoadBalancer"
  }
}

# TLS Secret for SSL certificate (when PEM provided)
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

# =============================================================================
# Optional: TMI-UX Frontend (when enabled)
# =============================================================================

# TMI-UX Deployment
resource "kubernetes_deployment_v1" "tmi_ux" {
  count = var.tmi_ux_enabled ? 1 : 0

  metadata {
    name      = "tmi-ux"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-ux"
      component = "frontend"
    }
  }

  spec {
    replicas = var.tmi_ux_replicas

    selector {
      match_labels = {
        app = "tmi-ux"
      }
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

# TMI-UX ClusterIP Service
resource "kubernetes_service_v1" "tmi_ux" {
  count = var.tmi_ux_enabled ? 1 : 0

  metadata {
    name      = "tmi-ux"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-ux"
      component = "frontend"
    }
  }

  spec {
    selector = {
      app = "tmi-ux"
    }

    port {
      name        = "http"
      port        = 80
      target_port = 8080
      protocol    = "TCP"
    }

    type = "ClusterIP"
  }
}
