# Variables for Azure Secrets Module

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

variable "soft_delete_retention_days" {
  description = "Number of days to retain soft-deleted secrets"
  type        = number
  default     = 7

  validation {
    condition     = var.soft_delete_retention_days >= 7 && var.soft_delete_retention_days <= 90
    error_message = "Soft delete retention must be between 7 and 90 days."
  }
}

variable "purge_protection_enabled" {
  description = "Enable purge protection for the Key Vault"
  type        = bool
  default     = false
}

variable "aks_identity_object_id" {
  description = "Object ID of the AKS managed identity for Key Vault access"
  type        = string
  default     = null
}

# Optional pre-set secrets (if null, random values are generated)
variable "db_password" {
  description = "Database password. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

variable "redis_password" {
  description = "Redis password. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

variable "jwt_secret" {
  description = "JWT signing secret. If not provided, a random secret is generated."
  type        = string
  sensitive   = true
  default     = null
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
