# Outputs for TMI OCI Free Tier Deployment

# Application Access
output "application_url" {
  description = "URL to access the TMI application"
  value       = module.compute.service_endpoint
  sensitive   = true
}

output "load_balancer_ip" {
  description = "Public IP address of the load balancer"
  value       = module.compute.load_balancer_ip
}

# Network Information
output "vcn_id" {
  description = "OCID of the VCN"
  value       = module.network.vcn_id
}

output "private_subnet_id" {
  description = "OCID of the private subnet"
  value       = module.network.private_subnet_id
}

# Database Information
output "database_id" {
  description = "OCID of the Autonomous Database"
  value       = module.database.autonomous_database_id
}

output "database_connection_string" {
  description = "Database connection string"
  value       = module.database.connection_string_high
  sensitive   = true
}

output "wallet_par_url" {
  description = "Pre-authenticated request URL for wallet download"
  value       = module.database.wallet_par_url
  sensitive   = true
}

# Container Instances
output "tmi_container_instance_id" {
  description = "OCID of the TMI container instance"
  value       = module.compute.tmi_container_instance_id
}

output "redis_container_instance_id" {
  description = "OCID of the Redis container instance"
  value       = module.compute.redis_container_instance_id
}

# Secrets
output "vault_id" {
  description = "OCID of the Vault"
  value       = module.secrets.vault_id
}

output "secret_names" {
  description = "Map of secret names"
  value       = module.secrets.secret_names
  sensitive   = true
}

# Logging
output "log_group_id" {
  description = "OCID of the Log Group"
  value       = module.logging.log_group_id
}

output "archive_bucket_name" {
  description = "Name of the log archive bucket"
  value       = module.logging.archive_bucket_name
}

# Generated Passwords (for initial setup - store securely!)
output "generated_passwords" {
  description = "Generated passwords (only shown if not provided)"
  sensitive   = true
  value = {
    db_password    = var.db_password == null ? "Generated - check terraform.tfstate" : "User provided"
    redis_password = var.redis_password == null ? "Generated - check terraform.tfstate" : "User provided"
    jwt_secret     = var.jwt_secret == null ? "Generated - check terraform.tfstate" : "User provided"
  }
}

# Useful Commands
output "useful_commands" {
  description = "Useful commands for managing the deployment"
  value = {
    ssh_tunnel_redis = "oci bastion session create-port-forwarding --bastion-id <bastion-ocid> --target-resource-id ${module.compute.redis_container_instance_id} --target-port 6379"
    logs_tail        = "oci logging search --search-query 'search \"${module.logging.log_group_id}\"' --time-start $(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ) --time-end $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    container_logs   = "oci container-instances container-instance get --container-instance-id ${module.compute.tmi_container_instance_id}"
  }
}
