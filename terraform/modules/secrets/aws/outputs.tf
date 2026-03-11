# Outputs for AWS Secrets Module

# KMS key outputs
output "kms_key_id" {
  description = "ID of the KMS key used for secrets encryption"
  value       = var.create_kms_key ? aws_kms_key.tmi[0].key_id : null
}

output "kms_key_arn" {
  description = "ARN of the KMS key used for secrets encryption"
  value       = var.create_kms_key ? aws_kms_key.tmi[0].arn : null
}

# Individual secret ARNs
output "db_password_secret_arn" {
  description = "ARN of the database password secret"
  value       = aws_secretsmanager_secret.db_password.arn
}

output "db_password_secret_name" {
  description = "Name of the database password secret"
  value       = aws_secretsmanager_secret.db_password.name
}

output "db_username_secret_arn" {
  description = "ARN of the database username secret"
  value       = aws_secretsmanager_secret.db_username.arn
}

output "db_username_secret_name" {
  description = "Name of the database username secret"
  value       = aws_secretsmanager_secret.db_username.name
}

output "redis_password_secret_arn" {
  description = "ARN of the Redis password secret"
  value       = aws_secretsmanager_secret.redis_password.arn
}

output "redis_password_secret_name" {
  description = "Name of the Redis password secret"
  value       = aws_secretsmanager_secret.redis_password.name
}

output "jwt_secret_arn" {
  description = "ARN of the JWT secret"
  value       = aws_secretsmanager_secret.jwt_secret.arn
}

output "jwt_secret_name" {
  description = "Name of the JWT secret"
  value       = aws_secretsmanager_secret.jwt_secret.name
}

output "oauth_client_secret_arn" {
  description = "ARN of the OAuth client secret (if created)"
  value       = var.oauth_client_secret != null ? aws_secretsmanager_secret.oauth_client_secret[0].arn : null
  sensitive   = true
}

output "oauth_client_secret_name" {
  description = "Name of the OAuth client secret (if created)"
  value       = var.oauth_client_secret != null ? aws_secretsmanager_secret.oauth_client_secret[0].name : null
  sensitive   = true
}

output "api_key_secret_arn" {
  description = "ARN of the API key secret (if created)"
  value       = var.api_key != null ? aws_secretsmanager_secret.api_key[0].arn : null
  sensitive   = true
}

output "api_key_secret_name" {
  description = "Name of the API key secret (if created)"
  value       = var.api_key != null ? aws_secretsmanager_secret.api_key[0].name : null
  sensitive   = true
}

# Combined secret outputs
output "combined_secret_arn" {
  description = "ARN of the combined secrets (single-secret mode)"
  value       = var.create_combined_secret ? aws_secretsmanager_secret.all_secrets[0].arn : null
}

output "combined_secret_name" {
  description = "Name of the combined secrets (single-secret mode)"
  value       = var.create_combined_secret ? aws_secretsmanager_secret.all_secrets[0].name : null
}

# IAM policy output
output "secrets_policy_arn" {
  description = "ARN of the IAM policy for reading secrets (attach to EKS IRSA role)"
  value       = aws_iam_policy.secrets_access.arn
}

# Standard interface outputs for multi-cloud compatibility
output "secrets_provider_id" {
  description = "Secrets provider ID (standard interface) - AWS account ID"
  value       = data.aws_caller_identity.current.account_id
}

output "secrets_endpoint" {
  description = "Secrets endpoint (standard interface) - AWS Secrets Manager regional endpoint"
  value       = "https://secretsmanager.${local.region_name}.amazonaws.com"
}

# Secret names for environment variable configuration
output "secret_names" {
  description = "Map of secret names for TMI configuration"
  sensitive   = true
  value = {
    db_username         = aws_secretsmanager_secret.db_username.name
    db_password         = aws_secretsmanager_secret.db_password.name
    redis_password      = aws_secretsmanager_secret.redis_password.name
    jwt_secret          = aws_secretsmanager_secret.jwt_secret.name
    oauth_client_secret = var.oauth_client_secret != null ? aws_secretsmanager_secret.oauth_client_secret[0].name : null
    api_key             = var.api_key != null ? aws_secretsmanager_secret.api_key[0].name : null
    all_secrets         = var.create_combined_secret ? aws_secretsmanager_secret.all_secrets[0].name : null
  }
}
