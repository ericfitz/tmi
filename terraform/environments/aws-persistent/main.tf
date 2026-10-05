# TMI AWS Persistent Environment
#
# Account-level resources that must outlive any single deployment of
# aws-public. Nothing here is ever destroyed by a deploy/undeploy script.
#
# Human-made architectural decision (Eric, 2026-09-26): the NAT egress Elastic
# IP 34.232.165.1 (eipalloc-07c325e51173c0bc9) is NEVER released unless Eric
# explicitly instructs it, naming the address. An external system monitors
# traffic from this address; once released, AWS may hand it to someone else.
# See docs/superpowers/specs/2026-09-26-adr-fixed-nat-egress-eip.md.
#
# This stack creates:
# - The NAT egress EIP (adopted via import, prevent_destroy)
# - IAM Deny on ec2:ReleaseAddress for that EIP, attached to admin principals
# - Settings-key escrow secret (#1009, prevent_destroy; written only by the rotator)
# - S3 log bucket (CloudTrail, ALB access logs, VPC flow logs), 30 d retention
# - CloudTrail (multi-region) to S3 + CloudWatch Logs
# - SNS security alert topic + EventBridge rules for sensitive API calls
# - RDS FreeStorageSpace alarm on the aws-public database, to the same topic
# - IAM Access Analyzer (account)
#
# Run with: export AWS_PROFILE=tmi
#   terraform init -backend-config=../aws-public/backend.hcl
#   terraform plan -out=persistent.tfplan

terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0.0"
    }
  }

  backend "s3" {
    key     = "tmi/aws-persistent/terraform.tfstate"
    encrypt = true
  }
}

provider "aws" {
  region = "us-east-1"

  default_tags {
    tags = {
      Project     = "tmi"
      Environment = "persistent"
      ManagedBy   = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}

locals {
  account_id      = data.aws_caller_identity.current.account_id
  log_bucket      = "tmi-logs-${local.account_id}"
  tfstate_bucket  = "tmi-tfstate-${local.account_id}"
  trail_name      = "tmi-trail"
  trail_arn       = "arn:aws:cloudtrail:us-east-1:${local.account_id}:trail/${local.trail_name}"
  retention_days  = 30
  elb_account_id  = "127311923021" # us-east-1 ELB log delivery account
  nat_eip_arn     = "arn:aws:ec2:us-east-1:${local.account_id}:elastic-ip/${aws_eip.nat_egress.allocation_id}"
  deny_policy_arn = "arn:aws:iam::${local.account_id}:policy/deny-release-tmi-nat-eip"
}

################################################################################
# NAT egress EIP — NEVER RELEASE (see header)
################################################################################

import {
  to = aws_eip.nat_egress
  id = "eipalloc-07c325e51173c0bc9"
}

resource "aws_eip" "nat_egress" {
  domain = "vpc"

  tags = {
    # Name is the lookup key used by aws-public's data "aws_eip".
    Name          = "tmi-nat-eip"
    Environment   = "public"
    Retain        = "true"
    ReleasePolicy = "explicit-owner-approval-only"
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_iam_policy" "deny_release_nat_eip" {
  name        = "deny-release-tmi-nat-eip"
  description = "Deny releasing the TMI NAT egress EIP 34.232.165.1 - release only on explicit owner instruction"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid      = "DenyReleaseTmiNatEip"
      Effect   = "Deny"
      Action   = "ec2:ReleaseAddress"
      Resource = local.nat_eip_arn
    }]
  })
}

resource "aws_iam_user_policy_attachment" "deny_release_nat_eip" {
  for_each   = toset(var.admin_users)
  user       = each.value
  policy_arn = aws_iam_policy.deny_release_nat_eip.arn
}

# Off-cluster escrow of the rotated settings-encryption key (#1009). Lives
# here, not in aws-public, so destroying the deployment never schedules the
# only off-cluster key copy for deletion (human decision, Eric, 2026-10-02).
# The rotator writes a new version before every promotion; Terraform never
# writes a version, so applies never clobber escrowed keys. aws-public looks
# it up by name (data "aws_secretsmanager_secret"); keep the name stable.
resource "aws_secretsmanager_secret" "settings_key_escrow" {
  name        = "tmi-settings-key-escrow"
  description = "TMI settings-encryption key escrow written by the rotator"

  tags = {
    Environment = "public"
    Retain      = "true"
  }

  lifecycle {
    prevent_destroy = true
  }
}

# Terraform's prevent_destroy guards the escrow secret only against Terraform.
# This Deny covers the console and CLI for the admin users too (#1030): deleting
# the secret (even with a recovery window) or attaching a resource policy that
# could expose the escrowed keys to another principal requires removing this
# attachment first, a deliberate two-step change.
resource "aws_iam_policy" "deny_delete_settings_key_escrow" {
  name        = "deny-delete-tmi-settings-key-escrow"
  description = "Deny deleting or re-permissioning the TMI settings-key escrow secret - only on explicit owner instruction"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid    = "DenyDeleteTmiSettingsKeyEscrow"
      Effect = "Deny"
      Action = [
        "secretsmanager:DeleteSecret",
        "secretsmanager:PutResourcePolicy",
      ]
      Resource = aws_secretsmanager_secret.settings_key_escrow.arn
    }]
  })
}

resource "aws_iam_user_policy_attachment" "deny_delete_settings_key_escrow" {
  for_each   = toset(var.admin_users)
  user       = each.value
  policy_arn = aws_iam_policy.deny_delete_settings_key_escrow.arn
}

################################################################################
# Log bucket
################################################################################

resource "aws_s3_bucket" "logs" {
  bucket              = local.log_bucket
  object_lock_enabled = true
}

resource "aws_s3_bucket_ownership_controls" "logs" {
  bucket = aws_s3_bucket.logs.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_public_access_block" "logs" {
  bucket                  = aws_s3_bucket.logs.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ALB access logs only support SSE-S3.
resource "aws_s3_bucket_server_side_encryption_configuration" "logs" {
  bucket = aws_s3_bucket.logs.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_versioning" "logs" {
  bucket = aws_s3_bucket.logs.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_object_lock_configuration" "logs" {
  bucket = aws_s3_bucket.logs.id
  rule {
    default_retention {
      mode = "GOVERNANCE"
      days = local.retention_days
    }
  }
  depends_on = [aws_s3_bucket_versioning.logs]
}

resource "aws_s3_bucket_lifecycle_configuration" "logs" {
  bucket = aws_s3_bucket.logs.id
  rule {
    id     = "expire-logs"
    status = "Enabled"
    filter {}
    expiration {
      days = local.retention_days + 1
    }
    noncurrent_version_expiration {
      noncurrent_days = 1
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
  depends_on = [aws_s3_bucket_versioning.logs]
}

resource "aws_s3_bucket_policy" "logs" {
  bucket = aws_s3_bucket.logs.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyInsecureTransport"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = [aws_s3_bucket.logs.arn, "${aws_s3_bucket.logs.arn}/*"]
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
      },
      {
        Sid       = "CloudTrailAclCheck"
        Effect    = "Allow"
        Principal = { Service = "cloudtrail.amazonaws.com" }
        Action    = "s3:GetBucketAcl"
        Resource  = aws_s3_bucket.logs.arn
        Condition = { StringEquals = { "aws:SourceArn" = local.trail_arn } }
      },
      {
        Sid       = "CloudTrailWrite"
        Effect    = "Allow"
        Principal = { Service = "cloudtrail.amazonaws.com" }
        Action    = "s3:PutObject"
        Resource  = "${aws_s3_bucket.logs.arn}/cloudtrail/AWSLogs/${local.account_id}/*"
        Condition = { StringEquals = { "aws:SourceArn" = local.trail_arn } }
      },
      {
        Sid       = "ElbAccessLogsWrite"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${local.elb_account_id}:root" }
        Action    = "s3:PutObject"
        Resource = [
          "${aws_s3_bucket.logs.arn}/alb/tmi-server/AWSLogs/${local.account_id}/*",
          "${aws_s3_bucket.logs.arn}/alb/tmi-webhooks/AWSLogs/${local.account_id}/*",
        ]
      },
      {
        Sid       = "FlowLogsAclCheck"
        Effect    = "Allow"
        Principal = { Service = "delivery.logs.amazonaws.com" }
        Action    = "s3:GetBucketAcl"
        Resource  = aws_s3_bucket.logs.arn
        Condition = { StringEquals = { "aws:SourceAccount" = local.account_id } }
      },
      {
        Sid       = "FlowLogsWrite"
        Effect    = "Allow"
        Principal = { Service = "delivery.logs.amazonaws.com" }
        Action    = "s3:PutObject"
        Resource  = "${aws_s3_bucket.logs.arn}/vpc-flow/AWSLogs/${local.account_id}/*"
        Condition = { StringEquals = { "aws:SourceAccount" = local.account_id } }
      },
    ]
  })
  depends_on = [aws_s3_bucket_public_access_block.logs]
}

################################################################################
# CloudTrail
################################################################################

resource "aws_cloudwatch_log_group" "cloudtrail" {
  name              = "/aws/cloudtrail/tmi"
  retention_in_days = local.retention_days
}

resource "aws_iam_role" "cloudtrail_logs" {
  name = "tmi-cloudtrail-logs"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "cloudtrail.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "cloudtrail_logs" {
  name = "write-cloudtrail-log-group"
  role = aws_iam_role.cloudtrail_logs.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
      Resource = "${aws_cloudwatch_log_group.cloudtrail.arn}:log-stream:*"
    }]
  })
}

resource "aws_cloudtrail" "tmi" {
  name                          = local.trail_name
  s3_bucket_name                = aws_s3_bucket.logs.id
  s3_key_prefix                 = "cloudtrail"
  is_multi_region_trail         = true
  include_global_service_events = true
  enable_log_file_validation    = true
  cloud_watch_logs_group_arn    = "${aws_cloudwatch_log_group.cloudtrail.arn}:*"
  cloud_watch_logs_role_arn     = aws_iam_role.cloudtrail_logs.arn

  advanced_event_selector {
    name = "Management events"
    field_selector {
      field  = "eventCategory"
      equals = ["Management"]
    }
  }

  advanced_event_selector {
    name = "Terraform state bucket writes"
    field_selector {
      field  = "eventCategory"
      equals = ["Data"]
    }
    field_selector {
      field  = "resources.type"
      equals = ["AWS::S3::Object"]
    }
    field_selector {
      field  = "readOnly"
      equals = ["false"]
    }
    field_selector {
      field       = "resources.ARN"
      starts_with = ["arn:aws:s3:::${local.tfstate_bucket}/"]
    }
  }

  depends_on = [aws_s3_bucket_policy.logs, aws_iam_role_policy.cloudtrail_logs]
}

################################################################################
# Security alerts: SNS + EventBridge
################################################################################

resource "aws_sns_topic" "security_alerts" {
  name = "tmi-security-alerts"
}

resource "aws_sns_topic_policy" "security_alerts" {
  arn = aws_sns_topic.security_alerts.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "EventBridgePublish"
        Effect    = "Allow"
        Principal = { Service = "events.amazonaws.com" }
        Action    = "sns:Publish"
        Resource  = aws_sns_topic.security_alerts.arn
        Condition = { StringEquals = { "aws:SourceAccount" = local.account_id } }
      },
      {
        Sid       = "CloudWatchAlarmsPublish"
        Effect    = "Allow"
        Principal = { Service = "cloudwatch.amazonaws.com" }
        Action    = "sns:Publish"
        Resource  = aws_sns_topic.security_alerts.arn
        Condition = { StringEquals = { "aws:SourceAccount" = local.account_id } }
      },
    ]
  })
}

# The recipient must click the confirmation link before alerts are delivered.
resource "aws_sns_topic_subscription" "security_email" {
  topic_arn = aws_sns_topic.security_alerts.arn
  protocol  = "email"
  endpoint  = var.security_alert_email
}

locals {
  cloudtrail_api = "AWS API Call via CloudTrail"

  alert_rules = {
    eip-nat-changes = {
      description = "EIP release/disassociate or NAT gateway deletion"
      pattern = {
        source        = ["aws.ec2"]
        "detail-type" = [local.cloudtrail_api]
        detail = {
          eventSource = ["ec2.amazonaws.com"]
          eventName   = ["ReleaseAddress", "DisassociateAddress", "DeleteNatGateway"]
        }
      }
    }
    cloudtrail-tampering = {
      description = "CloudTrail stopped, deleted, or reconfigured"
      pattern = {
        source        = ["aws.cloudtrail"]
        "detail-type" = [local.cloudtrail_api]
        detail = {
          eventSource = ["cloudtrail.amazonaws.com"]
          eventName   = ["StopLogging", "DeleteTrail", "UpdateTrail", "PutEventSelectors"]
        }
      }
    }
    eip-deny-policy-changes = {
      description = "EIP deny policy detached, deleted, or re-versioned"
      pattern = {
        source        = ["aws.iam"]
        "detail-type" = [local.cloudtrail_api]
        detail = {
          eventSource = ["iam.amazonaws.com"]
          eventName = [
            "DetachUserPolicy", "DetachGroupPolicy", "DetachRolePolicy", "DeletePolicy",
            "CreatePolicyVersion", "DeletePolicyVersion", "SetDefaultPolicyVersion",
          ]
          requestParameters = { policyArn = [local.deny_policy_arn] }
        }
      }
    }
    root-activity = {
      description = "Any root user activity"
      pattern = {
        "detail-type" = [local.cloudtrail_api, "AWS Console Sign In via CloudTrail"]
        detail        = { userIdentity = { type = ["Root"] } }
      }
    }
    console-login-no-mfa = {
      description = "Console sign-in without MFA"
      pattern = {
        "detail-type" = ["AWS Console Sign In via CloudTrail"]
        detail = {
          eventName           = ["ConsoleLogin"]
          additionalEventData = { MFAUsed = ["No"] }
        }
      }
    }
    access-analyzer-findings = {
      description = "IAM Access Analyzer active findings"
      pattern = {
        source        = ["aws.access-analyzer"]
        "detail-type" = ["Access Analyzer Finding"]
        detail        = { status = ["ACTIVE"] }
      }
    }
  }
}

resource "aws_cloudwatch_event_rule" "security" {
  for_each      = local.alert_rules
  name          = "tmi-security-${each.key}"
  description   = each.value.description
  event_pattern = jsonencode(each.value.pattern)
}

resource "aws_cloudwatch_event_target" "security" {
  for_each = local.alert_rules
  rule     = aws_cloudwatch_event_rule.security[each.key].name
  arn      = aws_sns_topic.security_alerts.arn
}

################################################################################
# RDS storage alarm (T362)
################################################################################

# Early warning before max_allocated_storage autoscaling (aws-public) would
# even need to kick in. 2 GiB threshold, two consecutive 5-minute breaches so
# a single sampling blip does not page. treat_missing_data = "missing" (the
# default, set explicitly): this stack outlives the RDS instance, so a
# destroyed/not-yet-created aws-public database must not be treated as a
# breach.
resource "aws_cloudwatch_metric_alarm" "rds_free_storage_space" {
  alarm_name          = "tmi-rds-free-storage-space-low"
  alarm_description   = "RDS free storage space on ${var.rds_instance_identifier} is below 2 GiB"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "FreeStorageSpace"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 2 * 1024 * 1024 * 1024 # 2 GiB, in bytes (the metric's unit)
  treat_missing_data  = "missing"

  dimensions = {
    DBInstanceIdentifier = var.rds_instance_identifier
  }

  alarm_actions = [aws_sns_topic.security_alerts.arn]
  ok_actions    = [aws_sns_topic.security_alerts.arn]
}

################################################################################
# IAM Access Analyzer
################################################################################

resource "aws_accessanalyzer_analyzer" "account" {
  analyzer_name = "tmi-account-analyzer"
  type          = "ACCOUNT"
}
