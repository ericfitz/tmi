# Outputs for OCI Compute Module

# TMI Server Container Instance
output "tmi_container_instance_id" {
  description = "OCID of the TMI container instance"
  value       = oci_container_instances_container_instance.tmi.id
}

output "tmi_container_instance_state" {
  description = "State of the TMI container instance"
  value       = oci_container_instances_container_instance.tmi.state
}

output "tmi_private_ip" {
  description = "Private IP address of the TMI container instance"
  value       = oci_container_instances_container_instance.tmi.vnics[0].private_ip
}

# Redis Container Instance
output "redis_container_instance_id" {
  description = "OCID of the Redis container instance"
  value       = oci_container_instances_container_instance.redis.id
}

output "redis_container_instance_state" {
  description = "State of the Redis container instance"
  value       = oci_container_instances_container_instance.redis.state
}

output "redis_private_ip" {
  description = "Private IP address of the Redis container instance"
  value       = oci_container_instances_container_instance.redis.vnics[0].private_ip
}

output "redis_endpoint" {
  description = "Redis connection endpoint"
  value       = "${oci_container_instances_container_instance.redis.vnics[0].private_ip}:6379"
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
  description = "Name of the backend set"
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
  value = [
    oci_container_instances_container_instance.tmi.id,
    oci_container_instances_container_instance.redis.id
  ]
}

output "load_balancer_dns" {
  description = "Load balancer DNS name (standard interface)"
  value       = oci_load_balancer_load_balancer.tmi.ip_addresses[0]
}
