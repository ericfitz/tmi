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

      # is_dev MUST be false on an internet-facing deployment. When true
      # (the built-in default), api/middleware.go CORS (line 123) and
      # api/websocket.go CheckOrigin (line 238) REFLECT ANY Origin with
      # access-control-allow-credentials: true — so TMI_CORS_ALLOWED_ORIGINS
      # above is inert and the endpoint is open to credentialed cross-origin
      # requests from any site. false makes CORS/WS enforce the allowlist and
      # switches logs to production format. Consistent with TMI_BUILD_MODE=
      # production. LoggingConfig.IsDev, config.go:257.
      TMI_LOG_IS_DEV = "false"

      TMI_SERVER_INTERFACE = "0.0.0.0"
      TMI_SERVER_PORT      = "8080"

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

      # OAuth client_callback allowlist for the tmi-ux web UI (S3+CloudFront at
      # www.tmi.dev). The transitional app.aws.tmi.dev entry is gone: the
      # aws.tmi.dev delegated subdomain was retired when the tmi.dev zone moved
      # into this account, and the client now lives only at www.tmi.dev.
      # config.go:183 (TMI_OAUTH_CLIENT_CALLBACK_ALLOWLIST);
      # auth/client_callback_allowlist.go is FAIL-CLOSED — an unset/empty value
      # rejects EVERY client_callback, so OAuth login fails outright. A pattern
      # ending in "*" is a strict string PREFIX match: the trailing slash after
      # the host is load-bearing — it confines matches to this exact origin
      # (e.g. "www.tmi.dev.evil.com/" does NOT match "https://www.tmi.dev/").
      # The UI uses three callbacks (/oauth2/callback, /oauth2/link/callback,
      # /oauth2/content-callback?return_to=...), one with a dynamic query
      # string, so a single origin-prefix wildcard covers them all. Comma-
      # separated list.
      TMI_OAUTH_CLIENT_CALLBACK_ALLOWLIST = "https://www.tmi.dev/*"

      # CORS: pin the allowed browser origin(s). config.go:107
      # (TMI_CORS_ALLOWED_ORIGINS). Unset, the server reflects any Origin back
      # with access-control-allow-credentials: true — setting this switches to
      # allowlist-only. NO wildcard allowed here (config.go:1182 rejects "*"
      # with credentials); list exact origins comma-separated.
      #
      # This is the BROWSER origin (the tmi-ux client), not the API's own
      # hostname — moving the API to api.tmi.dev does not change it. Same-origin
      # requests carry no Origin header and are never subject to this list.
      TMI_CORS_ALLOWED_ORIGINS = "https://www.tmi.dev"

      # ---- Timmy AI assistant (non-secret half) --------------------------
      # The two API keys live in the out-of-band `tmi-timmy` Secret
      # (scripts/set-timmy-secret.sh), referenced from the overlay's
      # server-config.yaml patch — they are deliberately not here, so the
      # provider credential never lands in Terraform state.
      #
      # These are environment variables rather than database settings for two
      # reasons. The config layer only defers to the database for values
      # nobody configured (#415, api/settings_service.go getConfigSetting), so
      # a database row alone would not switch Timmy on. And
      # TMI_TIMMY_TEXT_EMBEDDING_MODEL must stay in lockstep with
      # TMI_EMBEDDING_MODEL on the tmi-chunk-embed worker
      # (deployments/k8s/platform/components/tmi-chunk-embed.yml): the worker
      # embeds documents at ingest and the server embeds the query, and
      # vectors of different dimension score 0 against each other — retrieval
      # fails silently, with no error anywhere.
      #
      # embedding_dimension is 3072 because that is what text-embedding-3-large
      # actually returns (measured, not assumed). It must be > 0 or
      # EmbeddingProfile.Validate rejects every job envelope the monolith
      # stamps for the workers.
      TMI_TIMMY_ENABLED                 = "true"
      TMI_TIMMY_LLM_PROVIDER            = "openai"
      TMI_TIMMY_LLM_MODEL               = "gpt-5.5"
      TMI_TIMMY_LLM_BASE_URL            = "https://api.openai.com/v1"
      TMI_TIMMY_TEXT_EMBEDDING_PROVIDER = "openai"
      TMI_TIMMY_TEXT_EMBEDDING_MODEL    = "text-embedding-3-large"
      TMI_TIMMY_TEXT_EMBEDDING_BASE_URL = "https://api.openai.com/v1"
      TMI_TIMMY_EMBEDDING_DIMENSION     = "3072"
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

    # Settings-at-rest encryption (#547). The name is not arbitrary: no
    # secrets provider is configured for this deployment, so
    # internal/secrets/provider.go falls back to the EnvProvider, which maps
    # the secret key "settings_encryption_key" to the environment variable
    # TMI_SECRET_<KEY> — i.e. exactly TMI_SECRET_SETTINGS_ENCRYPTION_KEY.
    # Without it crypto.NewSettingsEncryptor returns a disabled encryptor and
    # every Secret-classified setting (the OAuth client secrets in the
    # replicated DB config among them) is written to RDS in plaintext, which
    # cmd/server/startup_checks.go reports as a production-mode ERROR.
    #
    # Setting this only encrypts values written from here on. Rows already
    # stored in plaintext stay that way until POST /admin/settings/reencrypt
    # is called — see the deploy notes in scripts/deploy-aws.sh.
    TMI_SECRET_SETTINGS_ENCRYPTION_KEY = var.settings_encryption_key
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
