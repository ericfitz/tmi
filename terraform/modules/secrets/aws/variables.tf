# Variables for AWS Secrets Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

# Required secrets
variable "db_username" {
  description = "Database username"
  type        = string
  default     = "admin"
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

variable "create_kms_key" {
  description = "Create a customer-managed KMS key for encrypting secrets (uses aws/secretsmanager default key if false)"
  type        = bool
  default     = true
}

variable "recovery_window_in_days" {
  description = "Number of days AWS Secrets Manager waits before permanently deleting a secret (0 for immediate deletion)"
  type        = number
  default     = 7

  validation {
    condition     = var.recovery_window_in_days == 0 || (var.recovery_window_in_days >= 7 && var.recovery_window_in_days <= 30)
    error_message = "Recovery window must be 0 (immediate) or between 7 and 30 days."
  }
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
