# Variables for Azure Network Module

variable "resource_group_name" {
  description = "Name of the Azure resource group"
  type        = string
}

variable "location" {
  description = "Azure region for resources"
  type        = string
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "vnet_cidr" {
  description = "CIDR block for the Virtual Network"
  type        = string
  default     = "10.0.0.0/16"
}

variable "aks_subnet_cidr" {
  description = "CIDR block for the AKS subnet"
  type        = string
  default     = "10.0.1.0/24"
}

variable "database_subnet_cidr" {
  description = "CIDR block for the database subnet"
  type        = string
  default     = "10.0.2.0/24"
}

variable "enable_private_endpoints" {
  description = "Enable private endpoints for database and Key Vault"
  type        = bool
  default     = false
}

variable "allowed_cidr" {
  description = "CIDR block allowed for inbound traffic (used in private mode)"
  type        = string
  default     = "10.0.0.0/16"
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
