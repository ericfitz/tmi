# Variables for OCI Database Module

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "db_name" {
  description = "Database name (alphanumeric, max 14 characters)"
  type        = string
  default     = "tmidb"

  validation {
    condition     = can(regex("^[a-zA-Z][a-zA-Z0-9]{0,13}$", var.db_name))
    error_message = "Database name must start with a letter, contain only alphanumeric characters, and be 1-14 characters."
  }
}

variable "admin_password" {
  description = "Admin password for the database"
  type        = string
  sensitive   = true

  validation {
    condition     = length(var.admin_password) >= 12 && length(var.admin_password) <= 30
    error_message = "Admin password must be 12-30 characters."
  }
}

variable "wallet_password" {
  description = "Password for wallet download. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

variable "database_subnet_id" {
  description = "OCID of the database subnet for private endpoint"
  type        = string
}

variable "database_nsg_ids" {
  description = "List of NSG OCIDs to associate with the database"
  type        = list(string)
  default     = []
}

variable "cpu_core_count" {
  description = "Number of CPU cores (for Free Tier, use 1)"
  type        = number
  default     = 1
}

variable "compute_count" {
  description = "Number of ECPUs (for Free Tier, use 2)"
  type        = number
  default     = 2
}

variable "data_storage_size_in_tbs" {
  description = "Data storage size in terabytes (for Free Tier, use 1)"
  type        = number
  default     = 1
}

variable "db_version" {
  description = "Oracle Database version"
  type        = string
  default     = "23ai"
}

variable "is_free_tier" {
  description = "Whether to create as Always Free tier"
  type        = bool
  default     = true
}

variable "is_auto_scaling_enabled" {
  description = "Enable auto scaling for compute"
  type        = bool
  default     = false
}

variable "is_auto_scaling_for_storage_enabled" {
  description = "Enable auto scaling for storage"
  type        = bool
  default     = false
}

variable "prevent_destroy" {
  description = "Prevent accidental destruction of database"
  type        = bool
  default     = true
}

variable "create_wallet_bucket" {
  description = "Create Object Storage bucket for wallet"
  type        = bool
  default     = true
}

variable "object_storage_namespace" {
  description = "Object Storage namespace for wallet bucket"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
