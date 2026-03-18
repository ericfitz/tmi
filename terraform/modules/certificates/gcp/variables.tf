# Variables for GCP Certificates Module

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "domain_names" {
  description = "List of domain names for the SSL certificate"
  type        = list(string)

  validation {
    condition     = length(var.domain_names) > 0
    error_message = "At least one domain name is required."
  }
}

variable "create_static_ip" {
  description = "Create a static external IP address for the load balancer"
  type        = bool
  default     = false
}
