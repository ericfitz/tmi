# Outputs for OCI Database Module

output "autonomous_database_id" {
  description = "OCID of the Autonomous Database"
  value       = oci_database_autonomous_database.tmi.id
}

output "connection_strings" {
  description = "Connection strings for the database"
  value       = oci_database_autonomous_database.tmi.connection_strings
}

output "connection_string_high" {
  description = "High connection string (for OLTP workloads)"
  value       = try(oci_database_autonomous_database.tmi.connection_strings[0].profiles[index(oci_database_autonomous_database.tmi.connection_strings[0].profiles[*].display_name, "tmidb_high")].value, "")
}

output "connection_string_medium" {
  description = "Medium connection string"
  value       = try(oci_database_autonomous_database.tmi.connection_strings[0].profiles[index(oci_database_autonomous_database.tmi.connection_strings[0].profiles[*].display_name, "tmidb_medium")].value, "")
}

output "connection_string_low" {
  description = "Low connection string"
  value       = try(oci_database_autonomous_database.tmi.connection_strings[0].profiles[index(oci_database_autonomous_database.tmi.connection_strings[0].profiles[*].display_name, "tmidb_low")].value, "")
}

output "private_endpoint_ip" {
  description = "Private endpoint IP address"
  value       = oci_database_autonomous_database.tmi.private_endpoint_ip
}

output "db_name" {
  description = "Database name"
  value       = oci_database_autonomous_database.tmi.db_name
}

output "db_version" {
  description = "Database version"
  value       = oci_database_autonomous_database.tmi.db_version
}

output "wallet_content_base64" {
  description = "Base64 encoded wallet content"
  value       = oci_database_autonomous_database_wallet.tmi.content
  sensitive   = true
}

output "wallet_bucket_name" {
  description = "Name of the wallet bucket"
  value       = var.create_wallet_bucket ? oci_objectstorage_bucket.wallet[0].name : null
}

output "wallet_object_name" {
  description = "Name of the wallet object"
  value       = var.create_wallet_bucket ? oci_objectstorage_object.wallet[0].object : null
}

output "wallet_par_url" {
  description = "Pre-authenticated request URL for wallet download"
  value       = var.create_wallet_bucket ? "https://objectstorage.${var.region}.oraclecloud.com${oci_objectstorage_preauthrequest.wallet[0].access_uri}" : null
  sensitive   = true
}

# Standard interface outputs for multi-cloud compatibility
output "database_id" {
  description = "Database ID (standard interface)"
  value       = oci_database_autonomous_database.tmi.id
}

output "database_endpoint" {
  description = "Database endpoint/hostname (standard interface)"
  value       = oci_database_autonomous_database.tmi.private_endpoint_ip
}

output "database_port" {
  description = "Database port (standard interface)"
  value       = 1522
}

output "database_name" {
  description = "Database name (standard interface)"
  value       = oci_database_autonomous_database.tmi.db_name
}
