# Outputs for Azure Network Module

output "vnet_id" {
  description = "ID of the Virtual Network"
  value       = azurerm_virtual_network.tmi.id
}

output "vnet_name" {
  description = "Name of the Virtual Network"
  value       = azurerm_virtual_network.tmi.name
}

output "aks_subnet_id" {
  description = "ID of the AKS subnet"
  value       = azurerm_subnet.aks.id
}

output "database_subnet_id" {
  description = "ID of the database subnet"
  value       = azurerm_subnet.database.id
}

output "aks_nsg_id" {
  description = "ID of the AKS network security group"
  value       = azurerm_network_security_group.aks.id
}

output "database_nsg_id" {
  description = "ID of the database network security group"
  value       = azurerm_network_security_group.database.id
}

output "nat_gateway_id" {
  description = "ID of the NAT gateway"
  value       = azurerm_nat_gateway.tmi.id
}

output "nat_public_ip" {
  description = "Public IP address of the NAT gateway"
  value       = azurerm_public_ip.nat.ip_address
}

output "postgresql_private_dns_zone_id" {
  description = "ID of the PostgreSQL private DNS zone"
  value       = azurerm_private_dns_zone.postgresql.id
}

# Standard interface outputs for multi-cloud compatibility
output "vpc_id" {
  description = "VPC/VNet ID (standard interface)"
  value       = azurerm_virtual_network.tmi.id
}

output "private_subnet_ids" {
  description = "List of private subnet IDs (standard interface)"
  value       = [azurerm_subnet.aks.id]
}

output "database_subnet_ids" {
  description = "List of database subnet IDs (standard interface)"
  value       = [azurerm_subnet.database.id]
}
