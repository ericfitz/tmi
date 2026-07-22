# Kubernetes Resources for TMI on EKS
# Bootstrap-only: namespace, ConfigMap, Secret, ServiceAccount (IRSA).
# Workload resources (Deployments/Services/Ingress) are NOT managed here —
# they are owned by the deployments/k8s/dev/aws kustomize overlay applied by
# the deploy script. This keeps terraform to infra + bootstrap, and lets the
# same workload manifests be reused across dev clusters and AWS.

# ============================================================================
# Namespace
#
# NOTE: named "tmi-platform" (not "tmi") to match the namespace convention
# used by every other TMI dev/deploy target (docker-desktop, k3s) and hardcoded
# by the deployments/k8s/dev/aws overlay (Task 5) and its NATS/Redis DNS names.
# ============================================================================

resource "kubernetes_namespace_v1" "tmi" {
  metadata {
    name = "tmi-platform"
    labels = {
      app        = "tmi"
      managed_by = "terraform"
    }
  }

  depends_on = [aws_eks_node_group.tmi]
}

# ============================================================================
# ConfigMap (non-sensitive environment variables)
#
# NOTE: named "tmi-server-config" to match the name deployments/k8s/dev/server.yml
# (the overlay's base manifest) already expects for the server's config.
# ============================================================================

resource "kubernetes_config_map_v1" "tmi" {
  metadata {
    name      = "tmi-server-config"
    namespace = kubernetes_namespace_v1.tmi.metadata[0].name
  }

  data = merge(
    {
      # deployments/k8s/dev/server.yml (the overlay's base manifest) mounts
      # this ConfigMap as a volume at /etc/tmi and runs the server with
      # --config=/etc/tmi/config.yml, so a "config.yml" key must exist.
      # internal/config/config.go Load() seeds defaults, then merges this
      # YAML file (missing/empty fields keep their defaults), then applies
      # environment variable overrides — so an intentionally-empty document
      # is valid: every AWS-specific value below is supplied via env vars
      # (TMI_* keys in this ConfigMap / patched directly on the Deployment),
      # not via this file.
      "config.yml" = <<-EOT
        # TMI AWS (EKS) configuration.
        # Intentionally minimal: all operational values are supplied via
        # environment variables (see the TMI_* keys in this ConfigMap and
        # the tmi-secrets Secret), not this file. See internal/config/config.go.
      EOT

      # NOTE: every key below is verified against internal/config/config.go's
      # `env:` struct tags (not guessed). A previous pass named several of
      # these after the YAML nesting (TMI_AUTH_*/TMI_LOGGING_*) instead of
      # the actual flattened env var name, which silently no-op'd them —
      # fixed here. This ConfigMap IS wired into the server via `envFrom`
      # in the deploy overlay's server-config.yaml patch, so these keys take
      # effect (an explicit `env:` entry in that patch still wins over the
      # same key here). TMI_SERVER_INTERFACE/TMI_SERVER_PORT/TMI_REDIS_HOST/
      # TMI_NATS_URL are also set explicitly on the container, so those four
      # are sourced from the patch; the rest (including
      # TMI_AUTH_AUTO_PROMOTE_FIRST_USER below) come from this ConfigMap.
      TMI_BUILD_MODE = var.tmi_build_mode # AuthConfig.BuildMode, config.go:149
      # Auto-promotion of the first authenticated user to admin is OFF: on an
      # internet-facing deployment it would hand admin to the first random
      # visitor who authenticates. Admin is instead seeded explicitly via the
      # `administrators` operational setting in the replicated database config
      # (the configured Google admin identity). AuthConfig.AutoPromoteFirstUser,
      # config.go:147.
      TMI_AUTH_AUTO_PROMOTE_FIRST_USER = "false"
      TMI_LOG_ALSO_LOG_TO_CONSOLE      = "true" # LoggingConfig.AlsoLogToConsole, config.go:276
      TMI_LOG_REDACT_AUTH_TOKENS       = "true" # LoggingConfig.RedactAuthTokens, config.go:287
      TMI_LOG_SUPPRESS_UNAUTH_LOGS     = "true" # LoggingConfig.SuppressUnauthenticatedLogs, config.go:288
      TMI_SERVER_INTERFACE             = "0.0.0.0"
      TMI_SERVER_PORT                  = "8080"

      # Redis accessed via the K8s ClusterIP service created by the deploy
      # overlay (deployments/k8s/dev/redis.yml -> Service "redis"). The
      # correct env var per internal/config/config.go is TMI_REDIS_HOST
      # (TMI_DATABASE_REDIS_HOST is not a recognized key).
      TMI_REDIS_HOST = "redis.tmi-platform.svc.cluster.local"

      # NATS runs in-cluster, applied by the deploy script ahead of the
      # workload overlay (see deployments/k8s/platform/nats.yml). Read
      # directly via os.Getenv/MustEnv in internal/worker/nats.go, not a
      # config.go struct tag — this name is already correct.
      TMI_NATS_URL = "nats://nats.tmi-platform.svc:4222"
    },
    # everyone_is_a_reviewer is decoupled from build_mode (a plain runtime flag)
    # so it stays on under production build mode. AuthConfig.EveryoneIsAReviewer,
    # config.go:148.
    var.everyone_is_a_reviewer ? {
      TMI_AUTH_EVERYONE_IS_A_REVIEWER = "true"
    } : {},
    # Verbose request/response logging is dev-only — off in production so the
    # public endpoint does not log full API/WS traffic.
    var.tmi_build_mode == "dev" ? {
      TMI_LOG_API_REQUESTS       = "true" # LoggingConfig.LogAPIRequests, config.go:284
      TMI_LOG_API_RESPONSES      = "true" # LoggingConfig.LogAPIResponses, config.go:285
      TMI_LOG_WEBSOCKET_MESSAGES = "true" # LoggingConfig.LogWebSocketMsg, config.go:286
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
    TMI_DATABASE_URL = "postgresql://${var.db_username}:${urlencode(var.db_password)}@${var.db_host}:${var.db_port}/${var.db_name}?sslmode=require"
    TMI_JWT_SECRET   = var.jwt_secret
    # Per internal/config/config.go the recognized key is TMI_REDIS_PASSWORD
    # (TMI_DATABASE_REDIS_PASSWORD is not a recognized key).
    TMI_REDIS_PASSWORD = var.redis_password
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

# NOTE: Workload resources (TMI API Deployment/Service, Redis Deployment/
# Service, ALB Ingress) previously lived here as kubernetes_deployment_v1 /
# kubernetes_service_v1 / kubernetes_ingress_v1 resources. They have been
# removed — the deployments/k8s/dev/aws kustomize overlay (applied by the
# deploy script, see Task 5/6 of the AWS deployment plan) now owns all
# workloads, reusing the same base manifests as the docker-desktop/k3s dev
# targets. Terraform's job here is infra + bootstrap only: namespace,
# ConfigMap, Secret, ServiceAccount (above), plus the EKS cluster, node
# group, IAM/IRSA roles, and the AWS Load Balancer Controller Helm release
# (all in main.tf), so the overlay's Ingress has a controller to bind to.
