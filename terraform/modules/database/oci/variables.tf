# Variables for OCI Database Module

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "region" {
  description = "OCI region"
  type        = string
  default     = "us-ashburn-1"
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

  validation {
    condition     = var.wallet_password == null || (length(var.wallet_password) >= 8 && can(regex("[a-zA-Z]", var.wallet_password)) && can(regex("[0-9!@#$%&*()_+=\\[\\]{}|:,.?-]", var.wallet_password)))
    error_message = "Wallet password must be at least 8 characters and contain alphabetic characters combined with numbers or special characters."
  }
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
  description = "Oracle Database version (23ai is default; Always Free tier supports 23ai)"
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

variable "deletion_protection" {
  description = "Enable deletion protection for the database. When true, IAM policies should be created to prevent deletion. The variable is a placeholder for IAM policy integration (follow-up work)."
  type        = bool
  default     = false
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
