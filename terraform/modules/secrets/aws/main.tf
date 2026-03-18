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
