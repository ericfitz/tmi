# AWS Logging Module for TMI
# Creates CloudWatch Log Group and Fluent Bit DaemonSet for pod log collection

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
  }
}

data "aws_region" "current" {}

# ============================================================================
# CloudWatch Log Group
# ============================================================================

resource "aws_cloudwatch_log_group" "tmi" {
  name              = "/tmi/${var.name_prefix}"
  retention_in_days = var.retention_days

  tags = var.tags
}

# ============================================================================
# Fluent Bit IAM Role (IRSA)
# ============================================================================

resource "aws_iam_role" "fluent_bit" {
  name = "${var.name_prefix}-fluent-bit"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRoleWithWebIdentity"
        Effect = "Allow"
        Principal = {
          Federated = var.oidc_provider_arn
        }
        Condition = {
          StringEquals = {
            "${var.oidc_provider_url}:aud" = "sts.amazonaws.com"
            "${var.oidc_provider_url}:sub" = "system:serviceaccount:amazon-cloudwatch:fluent-bit"
          }
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_policy" "fluent_bit" {
  name        = "${var.name_prefix}-fluent-bit-logs"
  description = "Allow Fluent Bit to write logs to CloudWatch"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:DescribeLogGroups",
          "logs:DescribeLogStreams"
        ]
        Resource = [
          aws_cloudwatch_log_group.tmi.arn,
          "${aws_cloudwatch_log_group.tmi.arn}:*"
        ]
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "fluent_bit" {
  policy_arn = aws_iam_policy.fluent_bit.arn
  role       = aws_iam_role.fluent_bit.name
}

# ============================================================================
# Fluent Bit Kubernetes Resources
# ============================================================================

resource "kubernetes_namespace_v1" "cloudwatch" {
  metadata {
    name = "amazon-cloudwatch"
    labels = {
      app        = "fluent-bit"
      managed_by = "terraform"
    }
  }
}

resource "kubernetes_service_account_v1" "fluent_bit" {
  metadata {
    name      = "fluent-bit"
    namespace = kubernetes_namespace_v1.cloudwatch.metadata[0].name
    annotations = {
      "eks.amazonaws.com/role-arn" = aws_iam_role.fluent_bit.arn
    }
  }

  automount_service_account_token = true
}

resource "kubernetes_cluster_role_v1" "fluent_bit" {
  metadata {
    name = "fluent-bit-read"
  }

  rule {
    api_groups = [""]
    resources  = ["namespaces", "pods"]
    verbs      = ["get", "list", "watch"]
  }
}

resource "kubernetes_cluster_role_binding_v1" "fluent_bit" {
  metadata {
    name = "fluent-bit-read"
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = kubernetes_cluster_role_v1.fluent_bit.metadata[0].name
  }

  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account_v1.fluent_bit.metadata[0].name
    namespace = kubernetes_namespace_v1.cloudwatch.metadata[0].name
  }
}

resource "kubernetes_config_map_v1" "fluent_bit" {
  metadata {
    name      = "fluent-bit-config"
    namespace = kubernetes_namespace_v1.cloudwatch.metadata[0].name
  }

  data = {
    "fluent-bit.conf" = <<-EOT
      [SERVICE]
          Flush         5
          Log_Level     info
          Daemon        off
          Parsers_File  parsers.conf

      [INPUT]
          Name              tail
          Tag               kube.*
          Path              /var/log/containers/tmi-*.log
          Parser            docker
          DB                /var/log/flb_kube.db
          Mem_Buf_Limit     50MB
          Skip_Long_Lines   On
          Refresh_Interval  10

      [FILTER]
          Name                kubernetes
          Match               kube.*
          Kube_URL            https://kubernetes.default.svc:443
          Kube_CA_File        /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
          Kube_Token_File     /var/run/secrets/kubernetes.io/serviceaccount/token
          Merge_Log           On
          K8S-Logging.Parser  On
          K8S-Logging.Exclude Off

      [OUTPUT]
          Name                cloudwatch_logs
          Match               kube.*
          region              ${data.aws_region.current.name}
          log_group_name      ${aws_cloudwatch_log_group.tmi.name}
          log_stream_prefix   pod/
          auto_create_group   false
    EOT

    "parsers.conf" = <<-EOT
      [PARSER]
          Name        docker
          Format      json
          Time_Key    time
          Time_Format %Y-%m-%dT%H:%M:%S.%L
          Time_Keep   On

      [PARSER]
          Name        cri
          Format      regex
          Regex       ^(?<time>[^ ]+) (?<stream>stdout|stderr) (?<logtag>[^ ]*) (?<message>.*)$
          Time_Key    time
          Time_Format %Y-%m-%dT%H:%M:%S.%N%:z
    EOT
  }
}

resource "kubernetes_daemon_set_v1" "fluent_bit" {
  metadata {
    name      = "fluent-bit"
    namespace = kubernetes_namespace_v1.cloudwatch.metadata[0].name
    labels = {
      app        = "fluent-bit"
      managed_by = "terraform"
    }
  }

  spec {
    selector {
      match_labels = {
        app = "fluent-bit"
      }
    }

    template {
      metadata {
        labels = {
          app = "fluent-bit"
        }
      }

      spec {
        service_account_name            = kubernetes_service_account_v1.fluent_bit.metadata[0].name
        automount_service_account_token = true

        container {
          name  = "fluent-bit"
          image = "amazon/aws-for-fluent-bit:latest"

          volume_mount {
            name       = "config"
            mount_path = "/fluent-bit/etc/"
          }

          volume_mount {
            name       = "varlog"
            mount_path = "/var/log"
            read_only  = true
          }

          volume_mount {
            name       = "varlibdockercontainers"
            mount_path = "/var/lib/docker/containers"
            read_only  = true
          }

          resources {
            requests = {
              cpu    = "100m"
              memory = "128Mi"
            }
            limits = {
              cpu    = "500m"
              memory = "256Mi"
            }
          }
        }

        volume {
          name = "config"
          config_map {
            name = kubernetes_config_map_v1.fluent_bit.metadata[0].name
          }
        }

        volume {
          name = "varlog"
          host_path {
            path = "/var/log"
          }
        }

        volume {
          name = "varlibdockercontainers"
          host_path {
            path = "/var/lib/docker/containers"
          }
        }

        toleration {
          key      = "node-role.kubernetes.io/master"
          operator = "Exists"
          effect   = "NoSchedule"
        }

        termination_grace_period_seconds = 30
      }
    }
  }
}
