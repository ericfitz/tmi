# AWS Logging Module for TMI
# Creates CloudWatch Log Groups, S3 archive bucket, SNS alerts, and CloudWatch alarms

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  all_tags = merge(var.tags, {
    ManagedBy = "terraform"
    Module    = "tmi-logging-aws"
  })
}

# -----------------------------------------------------------------------------
# CloudWatch Log Groups
# -----------------------------------------------------------------------------

resource "aws_cloudwatch_log_group" "app" {
  name              = "/tmi/${var.name_prefix}/app"
  retention_in_days = var.retention_days
  kms_key_id        = var.kms_key_arn

  tags = local.all_tags
}

resource "aws_cloudwatch_log_group" "containers" {
  name              = "/tmi/${var.name_prefix}/containers"
  retention_in_days = var.retention_days
  kms_key_id        = var.kms_key_arn

  tags = local.all_tags
}

# -----------------------------------------------------------------------------
# S3 Bucket for Log Archival (optional)
# -----------------------------------------------------------------------------

resource "aws_s3_bucket" "log_archive" {
  count  = var.create_archive_bucket ? 1 : 0
  bucket = "${var.name_prefix}-log-archive-${data.aws_caller_identity.current.account_id}"

  tags = local.all_tags
}

resource "aws_s3_bucket_versioning" "log_archive" {
  count  = var.create_archive_bucket ? 1 : 0
  bucket = aws_s3_bucket.log_archive[0].id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "log_archive" {
  count  = var.create_archive_bucket ? 1 : 0
  bucket = aws_s3_bucket.log_archive[0].id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "log_archive" {
  count  = var.create_archive_bucket ? 1 : 0
  bucket = aws_s3_bucket.log_archive[0].id

  rule {
    id     = "archive-lifecycle"
    status = "Enabled"

    transition {
      days          = var.archive_transition_days
      storage_class = "GLACIER"
    }

    expiration {
      days = var.archive_retention_days
    }
  }
}

resource "aws_s3_bucket_public_access_block" "log_archive" {
  count  = var.create_archive_bucket ? 1 : 0
  bucket = aws_s3_bucket.log_archive[0].id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# -----------------------------------------------------------------------------
# SNS Topic for Alerts (optional)
# -----------------------------------------------------------------------------

resource "aws_sns_topic" "alerts" {
  count = var.create_alert_topic ? 1 : 0
  name  = "${var.name_prefix}-logging-alerts"

  tags = local.all_tags
}

resource "aws_sns_topic_subscription" "email" {
  count     = var.create_alert_topic && var.alert_email != null ? 1 : 0
  topic_arn = aws_sns_topic.alerts[0].arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# -----------------------------------------------------------------------------
# CloudWatch Metric Alarms (optional)
# -----------------------------------------------------------------------------

# Metric filter for ERROR level logs in the app log group
resource "aws_cloudwatch_log_metric_filter" "error_count" {
  count          = var.create_alarms ? 1 : 0
  name           = "${var.name_prefix}-error-count"
  log_group_name = aws_cloudwatch_log_group.app.name
  pattern        = "ERROR"

  metric_transformation {
    name      = "${var.name_prefix}-error-count"
    namespace = "TMI/${var.name_prefix}"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "high_error_rate" {
  count               = var.create_alarms ? 1 : 0
  alarm_name          = "${var.name_prefix}-high-error-rate"
  alarm_description   = "TMI error rate exceeded threshold. Please investigate."
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "${var.name_prefix}-error-count"
  namespace           = "TMI/${var.name_prefix}"
  period              = 300
  statistic           = "Sum"
  threshold           = var.error_threshold
  treat_missing_data  = "notBreaching"

  alarm_actions = var.create_alert_topic ? [aws_sns_topic.alerts[0].arn] : []
  ok_actions    = var.create_alert_topic ? [aws_sns_topic.alerts[0].arn] : []

  tags = local.all_tags
}

# Metric filter for 5xx HTTP responses in the app log group
resource "aws_cloudwatch_log_metric_filter" "http_5xx" {
  count          = var.create_alarms ? 1 : 0
  name           = "${var.name_prefix}-http-5xx"
  log_group_name = aws_cloudwatch_log_group.app.name
  pattern        = "\"status\":5"

  metric_transformation {
    name      = "${var.name_prefix}-http-5xx"
    namespace = "TMI/${var.name_prefix}"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "http_5xx" {
  count               = var.create_alarms ? 1 : 0
  alarm_name          = "${var.name_prefix}-http-5xx-responses"
  alarm_description   = "TMI 5xx response rate exceeded threshold. Please investigate."
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "${var.name_prefix}-http-5xx"
  namespace           = "TMI/${var.name_prefix}"
  period              = 300
  statistic           = "Sum"
  threshold           = var.error_threshold
  treat_missing_data  = "notBreaching"

  alarm_actions = var.create_alert_topic ? [aws_sns_topic.alerts[0].arn] : []
  ok_actions    = var.create_alert_topic ? [aws_sns_topic.alerts[0].arn] : []

  tags = local.all_tags
}

# -----------------------------------------------------------------------------
# IAM Policy for EKS Pods to Write Logs
# -----------------------------------------------------------------------------

resource "aws_iam_policy" "logging" {
  name        = "${var.name_prefix}-logging-policy"
  description = "Allow EKS pods to write TMI logs to CloudWatch"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "CloudWatchLogs"
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:DescribeLogGroups",
          "logs:DescribeLogStreams",
        ]
        Resource = [
          aws_cloudwatch_log_group.app.arn,
          "${aws_cloudwatch_log_group.app.arn}:*",
          aws_cloudwatch_log_group.containers.arn,
          "${aws_cloudwatch_log_group.containers.arn}:*",
        ]
      },
    ]
  })

  tags = local.all_tags
}
