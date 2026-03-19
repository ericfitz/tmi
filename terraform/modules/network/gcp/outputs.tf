# Outputs for GCP Network Module

output "network_id" {
  description = "ID of the VPC network"
  value       = google_compute_network.tmi.id
}

output "network_name" {
  description = "Name of the VPC network"
  value       = google_compute_network.tmi.name
}

output "network_self_link" {
  description = "Self-link of the VPC network"
  value       = google_compute_network.tmi.self_link
}

output "primary_subnet_id" {
  description = "ID of the primary subnet"
  value       = google_compute_subnetwork.primary.id
}

output "primary_subnet_name" {
  description = "Name of the primary subnet"
  value       = google_compute_subnetwork.primary.name
}

output "primary_subnet_self_link" {
  description = "Self-link of the primary subnet"
  value       = google_compute_subnetwork.primary.self_link
}

output "pods_range_name" {
  description = "Name of the secondary IP range for pods"
  value       = "${var.name_prefix}-pods"
}

output "services_range_name" {
  description = "Name of the secondary IP range for services"
  value       = "${var.name_prefix}-services"
}

output "router_name" {
  description = "Name of the Cloud Router"
  value       = google_compute_router.tmi.name
}

output "nat_name" {
  description = "Name of the Cloud NAT"
  value       = google_compute_router_nat.tmi.name
}

# Standard interface outputs for multi-cloud compatibility
output "vpc_id" {
  description = "VPC/Network ID (standard interface)"
  value       = google_compute_network.tmi.id
}

output "private_subnet_ids" {
  description = "List of private subnet IDs (standard interface)"
  value       = [google_compute_subnetwork.primary.id]
}
