# Outputs for OCI Compute Module

# Combined TMI API + Redis Container Instance
output "tmi_container_instance_id" {
  description = "OCID of the TMI API + Redis container instance"
  value       = oci_container_instances_container_instance.tmi_api_redis.id
}

output "tmi_container_instance_state" {
  description = "State of the TMI API + Redis container instance"
  value       = oci_container_instances_container_instance.tmi_api_redis.state
}

output "tmi_private_ip" {
  description = "Private IP address of the TMI API + Redis container instance"
  value       = oci_container_instances_container_instance.tmi_api_redis.vnics[0].private_ip
}

# Redis is now co-located with TMI API, so these outputs reference the same instance
output "redis_container_instance_id" {
  description = "OCID of the container instance running Redis (same as TMI)"
  value       = oci_container_instances_container_instance.tmi_api_redis.id
}

output "redis_container_instance_state" {
  description = "State of the container instance running Redis (same as TMI)"
  value       = oci_container_instances_container_instance.tmi_api_redis.state
}

output "redis_private_ip" {
  description = "Private IP address of the container instance running Redis (same as TMI)"
  value       = oci_container_instances_container_instance.tmi_api_redis.vnics[0].private_ip
}

output "redis_endpoint" {
  description = "Redis connection endpoint (localhost for multi-container instance)"
  value       = "localhost:6379"
}

# TMI-UX Container Instance
output "tmi_ux_container_instance_id" {
  description = "OCID of the TMI-UX container instance"
  value       = var.tmi_ux_enabled ? oci_container_instances_container_instance.tmi_ux[0].id : null
}

output "tmi_ux_container_instance_state" {
  description = "State of the TMI-UX container instance"
  value       = var.tmi_ux_enabled ? oci_container_instances_container_instance.tmi_ux[0].state : null
}

output "tmi_ux_private_ip" {
  description = "Private IP address of the TMI-UX container instance"
  value       = var.tmi_ux_enabled ? oci_container_instances_container_instance.tmi_ux[0].vnics[0].private_ip : null
}

output "tmi_ux_backend_set_name" {
  description = "Name of the TMI-UX backend set"
  value       = var.tmi_ux_enabled ? oci_load_balancer_backend_set.tmi_ux[0].name : null
}

# Load Balancer
output "load_balancer_id" {
  description = "OCID of the load balancer"
  value       = oci_load_balancer_load_balancer.tmi.id
}

output "load_balancer_ip" {
  description = "Public IP address of the load balancer"
  # OCI provider v7.x: ip_addresses is now a list of strings
  value = oci_load_balancer_load_balancer.tmi.ip_addresses[0]
}

output "load_balancer_hostname" {
  description = "Hostname of the load balancer (if configured)"
  # Hostnames are managed separately in OCI provider v7.x
  value = null
}

output "backend_set_name" {
  description = "Name of the TMI API backend set"
  value       = oci_load_balancer_backend_set.tmi.name
}

# Application URLs
output "http_url" {
  description = "HTTP URL for the application"
  value       = "http://${oci_load_balancer_load_balancer.tmi.ip_addresses[0]}"
}

output "https_url" {
  description = "HTTPS URL for the application (if SSL configured)"
  value       = var.ssl_certificate_pem != null ? "https://${oci_load_balancer_load_balancer.tmi.ip_addresses[0]}" : null
}

# Standard interface outputs for multi-cloud compatibility
output "service_endpoint" {
  description = "Service endpoint URL (standard interface)"
  value       = var.ssl_certificate_pem != null ? "https://${oci_load_balancer_load_balancer.tmi.ip_addresses[0]}" : "http://${oci_load_balancer_load_balancer.tmi.ip_addresses[0]}"
}

output "container_instance_ids" {
  description = "List of container instance IDs (standard interface)"
  value = compact([
    oci_container_instances_container_instance.tmi_api_redis.id,
    var.tmi_ux_enabled ? oci_container_instances_container_instance.tmi_ux[0].id : null
  ])
}

output "load_balancer_dns" {
  description = "Load balancer DNS name (standard interface)"
  value       = oci_load_balancer_load_balancer.tmi.ip_addresses[0]
}

# Hostname routing outputs
output "routing_policy_name" {
  description = "Name of the hostname routing policy (if enabled)"
  value       = var.tmi_ux_enabled && var.api_hostname != null && var.ui_hostname != null ? oci_load_balancer_load_balancer_routing_policy.hostname_routing[0].name : null
}
