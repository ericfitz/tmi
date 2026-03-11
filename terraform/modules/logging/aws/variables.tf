# Variables for AWS Logging Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "retention_days" {
  description = "CloudWatch log retention duration in days"
  type        = number
  default     = 30

  validation {
    condition     = var.retention_days >= 1 && var.retention_days <= 3653
    error_message = "Retention days must be between 1 and 3653."
  }
}

variable "create_archive_bucket" {
  description = "Create S3 bucket for log archival"
  type        = bool
  default     = true
}

variable "archive_transition_days" {
  description = "Days before transitioning archived logs to Glacier"
  type        = number
  default     = 90
}

variable "archive_retention_days" {
  description = "Days before expiring archived logs"
  type        = number
  default     = 365
}

variable "create_alert_topic" {
  description = "Create SNS topic for alerts"
  type        = bool
  default     = true
}

variable "alert_email" {
  description = "Email address for alert notifications"
  type        = string
  default     = null
}

variable "create_alarms" {
  description = "Create CloudWatch metric alarms"
  type        = bool
  default     = true
}

variable "error_threshold" {
  description = "Error count threshold for alarm (per 5-minute period)"
  type        = number
  default     = 10
}

variable "kms_key_arn" {
  description = "KMS key ARN for CloudWatch log group encryption (optional)"
  type        = string
  default     = null
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
