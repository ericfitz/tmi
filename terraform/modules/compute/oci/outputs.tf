# Outputs for OCI Compute Module (ARM VM-based deployment)

# VM Instance
output "tmi_instance_id" {
  description = "OCID of the TMI ARM VM instance"
  value       = oci_core_instance.tmi.id
}

output "tmi_instance_state" {
  description = "State of the TMI ARM VM instance"
  value       = oci_core_instance.tmi.state
}

output "tmi_private_ip" {
  description = "Private IP address of the TMI ARM VM"
  value       = oci_core_instance.tmi.private_ip
}

# Compatibility aliases (previously container instance outputs)
output "tmi_container_instance_id" {
  description = "Alias for tmi_instance_id (compatibility)"
  value       = oci_core_instance.tmi.id
}

output "tmi_container_instance_state" {
  description = "Alias for tmi_instance_state (compatibility)"
  value       = oci_core_instance.tmi.state
}

output "redis_container_instance_id" {
  description = "Redis runs on the same VM as TMI (alias for tmi_instance_id)"
  value       = oci_core_instance.tmi.id
}

output "redis_container_instance_state" {
  description = "Redis runs on the same VM as TMI (alias for tmi_instance_state)"
  value       = oci_core_instance.tmi.state
}

output "redis_private_ip" {
  description = "Redis private IP (same VM as TMI, Redis listens on 127.0.0.1)"
  value       = oci_core_instance.tmi.private_ip
}

output "redis_endpoint" {
  description = "Redis connection endpoint (localhost on the VM)"
  value       = "127.0.0.1:6379"
}

# Load Balancer
output "load_balancer_id" {
  description = "OCID of the load balancer"
  value       = oci_load_balancer_load_balancer.tmi.id
}

output "load_balancer_ip" {
  description = "Public IP address of the load balancer"
  value       = oci_load_balancer_load_balancer.tmi.ip_addresses[0]
}

output "load_balancer_hostname" {
  description = "Hostname of the load balancer"
  value       = null
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
  description = "VM instance ID (standard interface, named for compatibility)"
  value       = [oci_core_instance.tmi.id]
}

output "load_balancer_dns" {
  description = "Load balancer DNS name (standard interface)"
  value       = oci_load_balancer_load_balancer.tmi.ip_addresses[0]
}

output "routing_policy_name" {
  description = "Routing policy name (not used in VM mode)"
  value       = null
}
