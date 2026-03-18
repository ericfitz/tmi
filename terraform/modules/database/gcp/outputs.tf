# Outputs for GCP Database Module

output "instance_name" {
  description = "Name of the Cloud SQL instance"
  value       = google_sql_database_instance.tmi.name
}

output "connection_name" {
  description = "Connection name for Cloud SQL Proxy (project:region:instance)"
  value       = google_sql_database_instance.tmi.connection_name
}

output "public_ip_address" {
  description = "Public IP address of the Cloud SQL instance"
  value       = google_sql_database_instance.tmi.public_ip_address
}

output "private_ip_address" {
  description = "Private IP address of the Cloud SQL instance"
  value       = google_sql_database_instance.tmi.private_ip_address
}

output "database_name" {
  description = "Name of the database"
  value       = google_sql_database.tmi.name
}

output "database_user" {
  description = "Database username"
  value       = google_sql_user.tmi.name
}

output "self_link" {
  description = "Self-link of the Cloud SQL instance"
  value       = google_sql_database_instance.tmi.self_link
}

# Standard interface outputs for multi-cloud compatibility
output "database_id" {
  description = "Database ID (standard interface)"
  value       = google_sql_database_instance.tmi.id
}

output "database_endpoint" {
  description = "Database endpoint/hostname (standard interface)"
  value       = var.enable_public_ip ? google_sql_database_instance.tmi.public_ip_address : google_sql_database_instance.tmi.private_ip_address
}

output "database_host" {
  description = "Database host for connection string"
  value       = var.enable_public_ip ? google_sql_database_instance.tmi.public_ip_address : google_sql_database_instance.tmi.private_ip_address
}

output "database_port" {
  description = "Database port (standard interface)"
  value       = 5432
}
