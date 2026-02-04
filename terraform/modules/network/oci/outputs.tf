# Outputs for OCI Network Module

output "vcn_id" {
  description = "OCID of the VCN"
  value       = oci_core_vcn.tmi.id
}

output "vcn_cidr" {
  description = "CIDR block of the VCN"
  value       = oci_core_vcn.tmi.cidr_blocks[0]
}

output "public_subnet_id" {
  description = "OCID of the public subnet"
  value       = oci_core_subnet.public.id
}

output "private_subnet_id" {
  description = "OCID of the private subnet"
  value       = oci_core_subnet.private.id
}

output "database_subnet_id" {
  description = "OCID of the database subnet"
  value       = oci_core_subnet.database.id
}

output "tmi_server_nsg_id" {
  description = "OCID of the TMI server network security group"
  value       = oci_core_network_security_group.tmi_server.id
}

output "redis_nsg_id" {
  description = "OCID of the Redis network security group"
  value       = oci_core_network_security_group.redis.id
}

output "lb_nsg_id" {
  description = "OCID of the load balancer network security group"
  value       = oci_core_network_security_group.lb.id
}

output "database_nsg_id" {
  description = "OCID of the database network security group"
  value       = oci_core_network_security_group.database.id
}

output "internet_gateway_id" {
  description = "OCID of the internet gateway"
  value       = oci_core_internet_gateway.tmi.id
}

output "nat_gateway_id" {
  description = "OCID of the NAT gateway"
  value       = oci_core_nat_gateway.tmi.id
}

output "service_gateway_id" {
  description = "OCID of the service gateway"
  value       = oci_core_service_gateway.tmi.id
}

output "availability_domains" {
  description = "List of availability domains"
  value       = data.oci_identity_availability_domains.ads.availability_domains[*].name
}

# Standard interface outputs for multi-cloud compatibility
output "vpc_id" {
  description = "VPC/VCN ID (standard interface)"
  value       = oci_core_vcn.tmi.id
}

output "private_subnet_ids" {
  description = "List of private subnet IDs (standard interface)"
  value       = [oci_core_subnet.private.id]
}

output "public_subnet_ids" {
  description = "List of public subnet IDs (standard interface)"
  value       = [oci_core_subnet.public.id]
}

output "database_subnet_ids" {
  description = "List of database subnet IDs (standard interface)"
  value       = [oci_core_subnet.database.id]
}

output "tmi_security_group_id" {
  description = "TMI server security group ID (standard interface)"
  value       = oci_core_network_security_group.tmi_server.id
}

output "redis_security_group_id" {
  description = "Redis security group ID (standard interface)"
  value       = oci_core_network_security_group.redis.id
}

output "lb_security_group_id" {
  description = "Load balancer security group ID (standard interface)"
  value       = oci_core_network_security_group.lb.id
}

output "tmi_ux_nsg_id" {
  description = "OCID of the TMI-UX network security group"
  value       = oci_core_network_security_group.tmi_ux.id
}

output "tmi_ux_security_group_id" {
  description = "TMI-UX security group ID (standard interface)"
  value       = oci_core_network_security_group.tmi_ux.id
}
