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
    condition     = can(regex("^[a-z0-9][a-z0-9.*-]*[a-z0-9]$", var.domain_name))
    error_message = "Domain name must be a valid DNS name."
  }
}

variable "subject_alternative_names" {
  description = "Additional domain names for the certificate (SANs)"
  type        = list(string)
  default     = []
}

variable "hosted_zone_id" {
  description = "Route 53 hosted zone ID that domain_name (and its SANs) live in; used to create DNS validation records"
  type        = string
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
