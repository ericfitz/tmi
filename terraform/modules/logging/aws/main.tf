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

  # Two things here are load-bearing and were previously wrong (see #567):
  #
  # 1. The tail input's DB (its file-offset bookkeeping) lives on a WRITABLE
  #    volume. It used to sit at /var/log/flb_kube.db, but /var/log is mounted
  #    read-only below, so fluent-bit could not create it — tail.0 failed to
  #    initialize, and since it is the only input, the whole backend exited and
  #    the DaemonSet crashlooped forever without shipping a single line.
  #
  # 2. The parser is `cri`, not `docker`. EKS nodes run containerd, which
  #    writes CRI-format lines ("<time> <stream> <logtag> <message>"), not
  #    Docker's JSON-per-line. With the docker parser every line fails to
  #    parse. This was masked by (1) — nothing ever got far enough to parse —
  #    so fixing only the DB path would have swapped a crashloop for silently
  #    malformed logs.
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
          Parser            cri
          DB                /var/fluent-bit/state/flb_kube.db
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

      # The payload capture MUST be named "log", not "message": the kubernetes
      # filter's `Merge_Log On` looks for a "log" key to parse application JSON
      # out of, and silently does nothing when the field is named anything else.
      # Time_Format likewise uses fluent-bit's strptime extensions (%L for
      # fractional seconds, %z for the offset) — %N/%:z are glibc/date(1)
      # spellings that fluent-bit does not implement, so timestamps failed to
      # parse and every record fell back to ingest time.
      [PARSER]
          Name        cri
          Format      regex
          Regex       ^(?<time>[^ ]+) (?<stream>stdout|stderr) (?<logtag>[^ ]*) (?<log>.*)$
          Time_Key    time
          Time_Format %Y-%m-%dT%H:%M:%S.%L%z
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

          # /var/log stays READ-ONLY: it is only ever tailed. The tail input's
          # state DB goes on the separate writable volume below rather than
          # relaxing this mount, which is why the read_only flag can stay.
          volume_mount {
            name       = "varlog"
            mount_path = "/var/log"
            read_only  = true
          }

          # Writable home for the tail input's file-offset DB. Deliberately a
          # hostPath and not an emptyDir: the DB records how far into each log
          # file fluent-bit has read, so keeping it on the node means a pod
          # restart resumes where it left off instead of re-shipping every
          # existing log line as duplicates.
          volume_mount {
            name       = "fluentbitstate"
            mount_path = "/var/fluent-bit/state"
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

        # type = DirectoryOrCreate so the first pod on a fresh node creates the
        # directory instead of failing to mount it.
        volume {
          name = "fluentbitstate"
          host_path {
            path = "/var/fluent-bit/state"
            type = "DirectoryOrCreate"
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
