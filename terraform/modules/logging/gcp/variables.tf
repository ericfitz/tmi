# Variables for GCP Logging Module

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for log bucket location"
  type        = string
  default     = "us-central1"
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "custom_retention_days" {
  description = "Custom log retention in days (null uses project default of 30 days)"
  type        = number
  default     = null

  validation {
    condition     = var.custom_retention_days == null || (var.custom_retention_days >= 1 && var.custom_retention_days <= 3650)
    error_message = "Retention days must be between 1 and 3650, or null for project default."
  }
}

variable "create_metrics" {
  description = "Create log-based metrics for monitoring"
  type        = bool
  default     = true
}

variable "create_archive_sink" {
  description = "Create a log sink to Cloud Storage for long-term archival"
  type        = bool
  default     = false
}

variable "archive_retention_days" {
  description = "Days to retain archived logs in Cloud Storage"
  type        = number
  default     = 365
}

variable "create_alerts" {
  description = "Create monitoring alert policies"
  type        = bool
  default     = false
}

variable "error_threshold" {
  description = "Error rate threshold for alert (errors per second)"
  type        = number
  default     = 0.1
}

variable "notification_channel_ids" {
  description = "List of notification channel IDs for alerts"
  type        = list(string)
  default     = []
}

variable "labels" {
  description = "Labels to apply to all resources"
  type        = map(string)
  default     = {}
}
