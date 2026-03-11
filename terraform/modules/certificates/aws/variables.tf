# Variables for AWS Certificates Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "domain_name" {
  description = "Primary domain name for the TLS certificate"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]*[a-z0-9]$", var.domain_name))
    error_message = "Domain name must be a valid DNS name."
  }
}

variable "subject_alternative_names" {
  description = "Additional domain names (SANs) to include in the certificate"
  type        = list(string)
  default     = []
}

variable "zone_id" {
  description = "Route 53 hosted zone ID for automated DNS validation (leave empty for manual validation)"
  type        = string
  default     = ""
}

variable "wait_for_validation" {
  description = "Whether to wait for certificate validation to complete (only applies when zone_id is provided)"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
