# Kubernetes Resources for TMI on EKS
# Manages Deployments, Services, ConfigMaps, Secrets, and Ingress

# ============================================================================
# Namespace
# ============================================================================

resource "kubernetes_namespace_v1" "tmi" {
  metadata {
    name = "tmi"
    labels = {
      app        = "tmi"
      managed_by = "terraform"
    }
  }

  depends_on = [aws_eks_node_group.tmi]
}

# ============================================================================
# ConfigMap (non-sensitive environment variables)
# ============================================================================

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

      # Redis accessed via K8s ClusterIP service
      TMI_DATABASE_REDIS_HOST = "tmi-redis.tmi.svc.cluster.local"
    },
    # Public mode adds verbose logging
    var.tmi_build_mode == "dev" ? {
      TMI_AUTH_EVERYONE_IS_A_REVIEWER    = "true"
      TMI_LOGGING_LOG_API_REQUESTS       = "true"
      TMI_LOGGING_LOG_API_RESPONSES      = "true"
      TMI_LOGGING_LOG_WEBSOCKET_MESSAGES = "true"
    } : {},
    var.extra_environment_variables
  )
}

# ============================================================================
# Secret (sensitive values)
# ============================================================================

resource "kubernetes_secret_v1" "tmi" {
  metadata {
    name      = "tmi-secrets"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  data = {
    TMI_DATABASE_URL            = "postgresql://${var.db_username}:${urlencode(var.db_password)}@${var.db_host}:${var.db_port}/${var.db_name}?sslmode=require"
    TMI_JWT_SECRET              = var.jwt_secret
    TMI_DATABASE_REDIS_PASSWORD = var.redis_password
  }
}

# ============================================================================
# ServiceAccount for TMI API (enables IRSA for AWS service access)
# ============================================================================

resource "kubernetes_service_account_v1" "tmi_api" {
  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app        = "tmi-api"
      managed_by = "terraform"
    }
    annotations = {
      "eks.amazonaws.com/role-arn" = aws_iam_role.tmi_pod.arn
    }
  }

  # T10 (#348): TMI on EKS uses IRSA to fetch secrets from Secrets Manager
  # (see internal/secrets/aws_provider.go). IRSA depends on the projected
  # SA token volume, so automount must stay TRUE here. The risk this would
  # otherwise mitigate (lateral movement via the auto-mounted token) is
  # countered by scoping the IAM policy below to var.secret_arns: the
  # token only lets the pod assume the tmi_pod role, which can ONLY read
  # the specific secret ARNs the deployer passes in. Verify scope at
  # deploy time with `aws iam simulate-principal-policy` against the role.
  automount_service_account_token = true
}

# ============================================================================
# TMI API Deployment
# ============================================================================

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
        service_account_name = kubernetes_service_account_v1.tmi_api.metadata[0].name
        # T10 (#348): see ServiceAccount comment above. IRSA needs this true.
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

# ============================================================================
# Redis Deployment (separate pod, accessed via ClusterIP service)
# ============================================================================

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

# ============================================================================
# Redis ClusterIP Service (internal only)
# ============================================================================

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

# ============================================================================
# TMI API Service (NodePort, targeted by ALB Ingress)
# ============================================================================

resource "kubernetes_service_v1" "tmi_api" {
  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    labels = {
      app       = "tmi-api"
      component = "api"
    }
  }

  spec {
    selector = {
      app = "tmi-api"
    }

    port {
      name        = "http"
      port        = 8080
      target_port = 8080
      protocol    = "TCP"
    }

    type = "NodePort"
  }
}

# ============================================================================
# ALB Ingress (via AWS Load Balancer Controller)
# ============================================================================

resource "kubernetes_ingress_v1" "tmi_api" {
  metadata {
    name      = "tmi-api"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
    annotations = merge(
      {
        "kubernetes.io/ingress.class"                        = "alb"
        "alb.ingress.kubernetes.io/scheme"                   = var.alb_scheme
        "alb.ingress.kubernetes.io/target-type"              = "ip"
        "alb.ingress.kubernetes.io/healthcheck-path"         = "/"
        "alb.ingress.kubernetes.io/load-balancer-attributes" = "idle_timeout.timeout_seconds=3600"
        "alb.ingress.kubernetes.io/listen-ports"             = "[{\"HTTP\": 80}]"
      },
      # Add certificate ARN annotation if provided
      var.certificate_arn != null ? {
        "alb.ingress.kubernetes.io/certificate-arn" = var.certificate_arn
        "alb.ingress.kubernetes.io/listen-ports"    = "[{\"HTTP\": 80}, {\"HTTPS\": 443}]"
        "alb.ingress.kubernetes.io/ssl-redirect"    = "443"
      } : {},
      # Subnets annotation
      length(var.alb_subnet_ids) > 0 ? {
        "alb.ingress.kubernetes.io/subnets" = join(",", var.alb_subnet_ids)
      } : {}
    )
  }

  spec {
    rule {
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
  }

  depends_on = [helm_release.aws_lb_controller]
}
