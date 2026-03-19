# Outputs for AWS Database Module

output "db_instance_id" {
  description = "ID of the RDS instance"
  value       = aws_db_instance.tmi.id
}

output "db_instance_arn" {
  description = "ARN of the RDS instance"
  value       = aws_db_instance.tmi.arn
}

output "host" {
  description = "Hostname of the RDS instance"
  value       = aws_db_instance.tmi.address
}

output "port" {
  description = "Port of the RDS instance"
  value       = aws_db_instance.tmi.port
}

output "database_name" {
  description = "Name of the database"
  value       = aws_db_instance.tmi.db_name
}

output "username" {
  description = "Master username"
  value       = aws_db_instance.tmi.username
}

output "connection_string" {
  description = "PostgreSQL connection string (without password)"
  value       = "postgresql://${aws_db_instance.tmi.username}@${aws_db_instance.tmi.address}:${aws_db_instance.tmi.port}/${aws_db_instance.tmi.db_name}"
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
