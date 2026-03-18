# Variables for Azure Certificates Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "key_vault_id" {
  description = "ID of the Key Vault for certificate storage"
  type        = string
}

variable "domain_name" {
  description = "Domain name for the TLS certificate"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]*[a-z0-9]$", var.domain_name))
    error_message = "Domain name must be a valid DNS name."
  }
}

variable "subject_alternative_names" {
  description = "Additional DNS names for the certificate"
  type        = list(string)
  default     = []
}

variable "create_self_signed_cert" {
  description = "Create a self-signed certificate in Key Vault (for development/initial setup)"
  type        = bool
  default     = false
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
