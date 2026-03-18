# Outputs for AWS Secrets Module

output "db_credentials_secret_arn" {
  description = "ARN of the database credentials secret"
  value       = aws_secretsmanager_secret.db_credentials.arn
}

output "db_credentials_secret_name" {
  description = "Name of the database credentials secret"
  value       = aws_secretsmanager_secret.db_credentials.name
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

# Generated password values (for passing to other modules)
output "db_password" {
  description = "Generated database password"
  value       = random_password.db_password.result
  sensitive   = true
}

output "redis_password" {
  description = "Generated Redis password"
  value       = random_password.redis_password.result
  sensitive   = true
}

output "jwt_secret" {
  description = "Generated JWT secret"
  value       = random_password.jwt_secret.result
  sensitive   = true
}

# Standard interface outputs for multi-cloud compatibility
output "secrets_provider_id" {
  description = "Secrets provider ID (standard interface)"
  value       = aws_secretsmanager_secret.db_credentials.arn
}

output "secret_arns" {
  description = "Map of all secret ARNs for IAM policy creation"
  value = {
    db_credentials = aws_secretsmanager_secret.db_credentials.arn
    redis_password = aws_secretsmanager_secret.redis_password.arn
    jwt_secret     = aws_secretsmanager_secret.jwt_secret.arn
  }
}
