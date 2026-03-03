# Outputs for OCI Compute Module (E5.Flex VM-based deployment)

# VM Instance
output "tmi_instance_id" {
  description = "OCID of the TMI VM instance"
  value       = oci_core_instance.tmi.id
}

output "tmi_instance_state" {
  description = "State of the TMI VM instance"
  value       = oci_core_instance.tmi.state
}

output "tmi_private_ip" {
  description = "Private IP address of the TMI VM"
  value       = oci_core_instance.tmi.private_ip
}

output "vm_public_ip" {
  description = "Public IP address of the TMI VM (direct access, no Load Balancer)"
  value       = oci_core_instance.tmi.public_ip
}

output "application_url" {
  description = "URL for the TMI-UX frontend"
  value       = "http://${oci_core_instance.tmi.public_ip}:4200"
}

output "api_url" {
  description = "URL for the TMI API"
  value       = "http://${oci_core_instance.tmi.public_ip}:8080"
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

output "container_instance_ids" {
  description = "VM instance ID (standard interface, named for compatibility)"
  value       = [oci_core_instance.tmi.id]
}

output "routing_policy_name" {
  description = "Routing policy name (not used in VM mode)"
  value       = null
}
