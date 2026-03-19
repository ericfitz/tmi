# Variables for GCP Secrets Module

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

# Secrets (if not provided, random values are generated)
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

# Workload Identity
variable "workload_identity_sa_email" {
  description = "GCP service account email for GKE Workload Identity (grants Secret Manager access)"
  type        = string
  default     = null
}

variable "labels" {
  description = "Labels to apply to all resources"
  type        = map(string)
  default     = {}
}
