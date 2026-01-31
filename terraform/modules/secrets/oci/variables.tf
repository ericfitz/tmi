# Variables for OCI Secrets Module

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "tenancy_ocid" {
  description = "OCI tenancy OCID (required for dynamic group creation)"
  type        = string
  default     = ""
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "vault_type" {
  description = "Vault type: DEFAULT or VIRTUAL_PRIVATE"
  type        = string
  default     = "DEFAULT"

  validation {
    condition     = contains(["DEFAULT", "VIRTUAL_PRIVATE"], var.vault_type)
    error_message = "Vault type must be DEFAULT or VIRTUAL_PRIVATE."
  }
}

variable "key_protection_mode" {
  description = "Key protection mode: SOFTWARE or HSM"
  type        = string
  default     = "SOFTWARE"

  validation {
    condition     = contains(["SOFTWARE", "HSM"], var.key_protection_mode)
    error_message = "Key protection mode must be SOFTWARE or HSM."
  }
}

# Required secrets
variable "db_username" {
  description = "Database username"
  type        = string
  default     = "ADMIN"
}

variable "db_password" {
  description = "Database password"
  type        = string
  sensitive   = true
}

variable "redis_password" {
  description = "Redis password"
  type        = string
  sensitive   = true
}

variable "jwt_secret" {
  description = "JWT signing secret"
  type        = string
  sensitive   = true
}

# Optional secrets
variable "oauth_client_secret" {
  description = "OAuth client secret (optional)"
  type        = string
  sensitive   = true
  default     = null
}

variable "api_key" {
  description = "API key (optional)"
  type        = string
  sensitive   = true
  default     = null
}

variable "create_combined_secret" {
  description = "Create a combined JSON secret for single-secret mode"
  type        = bool
  default     = true
}

variable "create_dynamic_group" {
  description = "Create dynamic group and policy for container instances"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
