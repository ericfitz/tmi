# Variables for Azure Database Module

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

variable "postgresql_version" {
  description = "PostgreSQL version"
  type        = string
  default     = "16"

  validation {
    condition     = contains(["13", "14", "15", "16", "17"], var.postgresql_version)
    error_message = "PostgreSQL version must be 13, 14, 15, 16, or 17."
  }
}

variable "admin_username" {
  description = "Administrator username for PostgreSQL"
  type        = string
  default     = "tmiadmin"

  validation {
    condition     = !contains(["admin", "administrator", "root", "sa", "postgres"], lower(var.admin_username))
    error_message = "Admin username cannot be a reserved name (admin, administrator, root, sa, postgres)."
  }
}

variable "admin_password" {
  description = "Administrator password for PostgreSQL"
  type        = string
  sensitive   = true

  validation {
    condition     = length(var.admin_password) >= 8 && length(var.admin_password) <= 128
    error_message = "Admin password must be 8-128 characters."
  }
}

variable "database_name" {
  description = "Name of the TMI database"
  type        = string
  default     = "tmi"
}

variable "sku_name" {
  description = "SKU name for the PostgreSQL Flexible Server (e.g., B_Standard_B1ms, B_Standard_B2s)"
  type        = string
  default     = "B_Standard_B1ms"
}

variable "storage_mb" {
  description = "Storage size in MB"
  type        = number
  default     = 32768

  validation {
    condition     = var.storage_mb >= 32768
    error_message = "Storage must be at least 32768 MB (32 GB)."
  }
}

variable "backup_retention_days" {
  description = "Backup retention period in days"
  type        = number
  default     = 7

  validation {
    condition     = var.backup_retention_days >= 7 && var.backup_retention_days <= 35
    error_message = "Backup retention must be between 7 and 35 days."
  }
}

variable "availability_zone" {
  description = "Availability zone for the server (1, 2, or 3)"
  type        = string
  default     = "1"
}

variable "enable_private_access" {
  description = "Enable private access via VNet integration"
  type        = bool
  default     = false
}

variable "database_subnet_id" {
  description = "ID of the database subnet for VNet integration"
  type        = string
  default     = null
}

variable "private_dns_zone_id" {
  description = "ID of the private DNS zone for PostgreSQL"
  type        = string
  default     = null
}

variable "deletion_protection" {
  description = "Enable deletion protection via Azure management lock"
  type        = bool
  default     = false
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
