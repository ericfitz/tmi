# AWS Secrets Module for TMI
# Creates Secrets Manager secrets for DB credentials, Redis password, and JWT secret
# Generates random passwords for all secrets

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0.0"
    }
  }
}

# Random password generation
resource "random_password" "db_password" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}|:,.?"
}

resource "random_password" "redis_password" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}|:,.?"
}

resource "random_password" "jwt_secret" {
  length  = 64
  special = false
}

# Settings-encryption key for secret-classified system settings (#547).
#
# This is NOT a password, and random_password is the wrong generator for it:
# internal/crypto/settings_encryptor.go's decodeHexKey() hex-decodes the value
# and rejects anything that is not exactly 32 bytes (64 hex characters), so the
# key must be hex text, not an arbitrary character soup. random_id with
# byte_length = 32 gives exactly that via its .hex attribute — 32 random bytes
# rendered as 64 lowercase hex chars, which is also the AES-256-GCM key size
# the encryptor expects.
resource "random_id" "settings_encryption_key" {
  byte_length = 32
}

# Database credentials secret
resource "aws_secretsmanager_secret" "db_credentials" {
  name        = "${var.name_prefix}-db-credentials"
  description = "TMI database credentials"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "db_credentials" {
  secret_id = aws_secretsmanager_secret.db_credentials.id
  secret_string = jsonencode({
    username = var.db_username
    password = random_password.db_password.result
  })
}

# Redis password secret
resource "aws_secretsmanager_secret" "redis_password" {
  name        = "${var.name_prefix}-redis-password"
  description = "TMI Redis password"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "redis_password" {
  secret_id     = aws_secretsmanager_secret.redis_password.id
  secret_string = random_password.redis_password.result
}

# JWT secret
resource "aws_secretsmanager_secret" "jwt_secret" {
  name        = "${var.name_prefix}-jwt-secret"
  description = "TMI JWT signing secret"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "jwt_secret" {
  secret_id     = aws_secretsmanager_secret.jwt_secret.id
  secret_string = random_password.jwt_secret.result
}

# Settings encryption key
resource "aws_secretsmanager_secret" "settings_encryption_key" {
  name        = "${var.name_prefix}-settings-encryption-key"
  description = "TMI settings-at-rest encryption key (AES-256-GCM, hex-encoded)"

  tags = var.tags
}

# Stored as a JSON object, not a bare string, and the property name is not
# arbitrary. internal/secrets/aws_provider.go treats the secret as a JSON map
# from secret KEY to value, and internal/crypto/settings_encryptor.go asks that
# provider for the key named "settings_encryption_key" (see SecretKeys in
# internal/secrets/provider.go). Storing the raw hex here instead would make
# the AWS provider return ErrSecretNotFound and silently disable encryption.
#
# This shape is what lets `dbtool --import-config` fetch the key itself, using
# the deployer's own AWS identity, rather than having scripts/deploy-aws.sh
# read the value and hand it over — the value never reaches a shell variable,
# argv, or the environment.
resource "aws_secretsmanager_secret_version" "settings_encryption_key" {
  secret_id = aws_secretsmanager_secret.settings_encryption_key.id
  secret_string = jsonencode({
    settings_encryption_key = random_id.settings_encryption_key.hex
  })
}
