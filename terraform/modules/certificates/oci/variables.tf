# Variables for OCI Certificates Module

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "tenancy_ocid" {
  description = "OCI tenancy OCID (required for dynamic group creation)"
  type        = string
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "subnet_id" {
  description = "Subnet OCID for the Function Application"
  type        = string
}

# DNS Configuration
variable "dns_zone_id" {
  description = "OCID of the OCI DNS zone for the domain"
  type        = string
}

variable "domain_name" {
  description = "Domain name for the TLS certificate (must be in the DNS zone)"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]*[a-z0-9]$", var.domain_name))
    error_message = "Domain name must be a valid DNS name."
  }
}

# ACME Configuration
variable "acme_contact_email" {
  description = "Email address for Let's Encrypt account and notifications"
  type        = string

  validation {
    condition     = can(regex("^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$", var.acme_contact_email))
    error_message = "Must be a valid email address."
  }
}

variable "acme_directory" {
  description = "ACME directory URL: staging or production"
  type        = string
  default     = "staging"

  validation {
    condition     = contains(["staging", "production"], var.acme_directory)
    error_message = "ACME directory must be 'staging' or 'production'."
  }
}

variable "certificate_renewal_days" {
  description = "Days before certificate expiry to trigger renewal"
  type        = number
  default     = 30

  validation {
    condition     = var.certificate_renewal_days >= 7 && var.certificate_renewal_days <= 60
    error_message = "Certificate renewal days must be between 7 and 60."
  }
}

# Load Balancer Configuration
variable "load_balancer_id" {
  description = "OCID of the OCI Load Balancer to update with certificates"
  type        = string
}

# Vault Configuration
variable "vault_id" {
  description = "OCID of the OCI Vault for storing certificates and ACME account key"
  type        = string
}

variable "vault_key_id" {
  description = "OCID of the encryption key in the Vault"
  type        = string
}

# Function Configuration
variable "certmgr_image_url" {
  description = "Container image URL for the certificate manager function"
  type        = string
}

variable "function_memory_mb" {
  description = "Memory allocation for the function in MB"
  type        = number
  default     = 256

  validation {
    condition     = contains([128, 256, 512, 1024], var.function_memory_mb)
    error_message = "Function memory must be 128, 256, 512, or 1024 MB."
  }
}

variable "function_timeout_seconds" {
  description = "Function timeout in seconds (max 300)"
  type        = number
  default     = 300

  validation {
    condition     = var.function_timeout_seconds >= 30 && var.function_timeout_seconds <= 300
    error_message = "Function timeout must be between 30 and 300 seconds."
  }
}

# IAM Configuration
variable "create_dynamic_group" {
  description = "Create dynamic group and policy for function access to OCI resources"
  type        = bool
  default     = true
}

variable "existing_dynamic_group_name" {
  description = "Name of existing dynamic group if create_dynamic_group is false"
  type        = string
  default     = null
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
