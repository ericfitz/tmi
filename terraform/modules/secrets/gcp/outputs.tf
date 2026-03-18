# Outputs for GCP Secrets Module

output "db_password_secret_id" {
  description = "ID of the database password secret"
  value       = google_secret_manager_secret.db_password.id
}

output "redis_password_secret_id" {
  description = "ID of the Redis password secret"
  value       = google_secret_manager_secret.redis_password.id
}

output "jwt_secret_secret_id" {
  description = "ID of the JWT secret"
  value       = google_secret_manager_secret.jwt_secret.id
}

# Resolved secret values (for passing to kubernetes module)
output "db_password" {
  description = "Database password (generated or provided)"
  value       = local.db_password
  sensitive   = true
}

output "redis_password" {
  description = "Redis password (generated or provided)"
  value       = local.redis_password
  sensitive   = true
}

output "jwt_secret" {
  description = "JWT signing secret (generated or provided)"
  value       = local.jwt_secret
  sensitive   = true
}

# Standard interface outputs for multi-cloud compatibility
output "secrets_provider_id" {
  description = "Secrets provider ID (standard interface)"
  value       = var.project_id
}

output "secret_names" {
  description = "Map of secret names for TMI configuration"
  value = {
    db_password    = google_secret_manager_secret.db_password.secret_id
    redis_password = google_secret_manager_secret.redis_password.secret_id
    jwt_secret     = google_secret_manager_secret.jwt_secret.secret_id
  }
}
