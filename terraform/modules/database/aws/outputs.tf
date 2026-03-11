# Outputs for AWS Database Module

output "db_instance_id" {
  description = "Identifier of the RDS instance"
  value       = aws_db_instance.tmi.id
}

output "db_instance_endpoint" {
  description = "Connection endpoint (hostname:port)"
  value       = aws_db_instance.tmi.endpoint
}

output "db_instance_address" {
  description = "Hostname of the RDS instance"
  value       = aws_db_instance.tmi.address
}

output "db_instance_port" {
  description = "Port of the RDS instance"
  value       = aws_db_instance.tmi.port
}

output "db_name" {
  description = "Name of the database"
  value       = aws_db_instance.tmi.db_name
}

output "database_url" {
  description = "PostgreSQL connection URL"
  value       = "postgresql://${aws_db_instance.tmi.username}:${var.db_password}@${aws_db_instance.tmi.endpoint}/${aws_db_instance.tmi.db_name}"
  sensitive   = true
}

# Standard interface outputs for multi-cloud compatibility
output "database_id" {
  description = "Database ID (standard interface)"
  value       = aws_db_instance.tmi.id
}

output "database_endpoint" {
  description = "Database endpoint/hostname (standard interface)"
  value       = aws_db_instance.tmi.address
}

output "database_port" {
  description = "Database port (standard interface)"
  value       = aws_db_instance.tmi.port
}

output "database_name" {
  description = "Database name (standard interface)"
  value       = aws_db_instance.tmi.db_name
}
