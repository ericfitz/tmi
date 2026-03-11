# AWS Secrets Module for TMI
# Creates AWS Secrets Manager secrets with optional KMS encryption and IAM access policy

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
  region_name = data.aws_region.current.id
}

# KMS Key for encrypting secrets (optional - uses aws/secretsmanager default key if not created)
resource "aws_kms_key" "tmi" {
  count = var.create_kms_key ? 1 : 0

  description             = "KMS key for TMI secrets encryption"
  deletion_window_in_days = var.recovery_window_in_days == 0 ? 7 : var.recovery_window_in_days
  enable_key_rotation     = true

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-secrets-key"
  })
}

resource "aws_kms_alias" "tmi" {
  count = var.create_kms_key ? 1 : 0

  name          = "alias/${var.name_prefix}-secrets"
  target_key_id = aws_kms_key.tmi[0].key_id
}

# Database Password Secret
resource "aws_secretsmanager_secret" "db_password" {
  name                    = "${var.name_prefix}-db-password"
  kms_key_id              = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-db-password"
  })
}

resource "aws_secretsmanager_secret_version" "db_password" {
  secret_id     = aws_secretsmanager_secret.db_password.id
  secret_string = var.db_password
}

# Database Username Secret
resource "aws_secretsmanager_secret" "db_username" {
  name                    = "${var.name_prefix}-db-username"
  kms_key_id              = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-db-username"
  })
}

resource "aws_secretsmanager_secret_version" "db_username" {
  secret_id     = aws_secretsmanager_secret.db_username.id
  secret_string = var.db_username
}

# Redis Password Secret
resource "aws_secretsmanager_secret" "redis_password" {
  name                    = "${var.name_prefix}-redis-password"
  kms_key_id              = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-redis-password"
  })
}

resource "aws_secretsmanager_secret_version" "redis_password" {
  secret_id     = aws_secretsmanager_secret.redis_password.id
  secret_string = var.redis_password
}

# JWT Secret
resource "aws_secretsmanager_secret" "jwt_secret" {
  name                    = "${var.name_prefix}-jwt-secret"
  kms_key_id              = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-jwt-secret"
  })
}

resource "aws_secretsmanager_secret_version" "jwt_secret" {
  secret_id     = aws_secretsmanager_secret.jwt_secret.id
  secret_string = var.jwt_secret
}

# OAuth Client Secret (optional)
resource "aws_secretsmanager_secret" "oauth_client_secret" {
  count = var.oauth_client_secret != null ? 1 : 0

  name                    = "${var.name_prefix}-oauth-client-secret"
  kms_key_id              = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-oauth-client-secret"
  })
}

resource "aws_secretsmanager_secret_version" "oauth_client_secret" {
  count = var.oauth_client_secret != null ? 1 : 0

  secret_id     = aws_secretsmanager_secret.oauth_client_secret[0].id
  secret_string = var.oauth_client_secret
}

# API Key Secret (optional)
resource "aws_secretsmanager_secret" "api_key" {
  count = var.api_key != null ? 1 : 0

  name                    = "${var.name_prefix}-api-key"
  kms_key_id              = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-api-key"
  })
}

resource "aws_secretsmanager_secret_version" "api_key" {
  count = var.api_key != null ? 1 : 0

  secret_id     = aws_secretsmanager_secret.api_key[0].id
  secret_string = var.api_key
}

# Combined secrets JSON for single-secret mode
# This allows TMI to fetch all secrets with one API call
resource "aws_secretsmanager_secret" "all_secrets" {
  count = var.create_combined_secret ? 1 : 0

  name                    = "${var.name_prefix}-all-secrets"
  kms_key_id              = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
  recovery_window_in_days = var.recovery_window_in_days

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-all-secrets"
  })
}

resource "aws_secretsmanager_secret_version" "all_secrets" {
  count = var.create_combined_secret ? 1 : 0

  secret_id = aws_secretsmanager_secret.all_secrets[0].id
  secret_string = jsonencode({
    TMI_DB_USER         = var.db_username
    TMI_DB_PASSWORD     = var.db_password
    REDIS_PASSWORD      = var.redis_password
    TMI_JWT_SECRET      = var.jwt_secret
    OAUTH_CLIENT_SECRET = var.oauth_client_secret
  })
}

# IAM policy document for EKS pod access to secrets (IRSA pattern)
data "aws_iam_policy_document" "secrets_access" {
  # Allow reading individual secrets
  statement {
    sid    = "ReadSecrets"
    effect = "Allow"
    actions = [
      "secretsmanager:GetSecretValue",
      "secretsmanager:DescribeSecret",
    ]
    resources = concat(
      [
        aws_secretsmanager_secret.db_password.arn,
        aws_secretsmanager_secret.db_username.arn,
        aws_secretsmanager_secret.redis_password.arn,
        aws_secretsmanager_secret.jwt_secret.arn,
      ],
      var.oauth_client_secret != null ? [aws_secretsmanager_secret.oauth_client_secret[0].arn] : [],
      var.api_key != null ? [aws_secretsmanager_secret.api_key[0].arn] : [],
      var.create_combined_secret ? [aws_secretsmanager_secret.all_secrets[0].arn] : [],
    )
  }

  # Allow KMS decrypt if using customer-managed key
  dynamic "statement" {
    for_each = var.create_kms_key ? [1] : []
    content {
      sid    = "DecryptSecrets"
      effect = "Allow"
      actions = [
        "kms:Decrypt",
        "kms:DescribeKey",
      ]
      resources = [
        aws_kms_key.tmi[0].arn,
      ]
    }
  }
}

resource "aws_iam_policy" "secrets_access" {
  name        = "${var.name_prefix}-secrets-access"
  description = "Allow TMI EKS pods to read secrets from AWS Secrets Manager"
  policy      = data.aws_iam_policy_document.secrets_access.json

  tags = var.tags
}
