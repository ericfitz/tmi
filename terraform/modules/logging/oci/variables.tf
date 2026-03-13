# Variables for OCI Logging Module

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "tenancy_ocid" {
  description = "OCI tenancy OCID (required for container log IAM resources)"
  type        = string
  default     = ""
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "retention_days" {
  description = "Log retention duration in days"
  type        = number
  default     = 30

  validation {
    condition     = var.retention_days >= 1 && var.retention_days <= 180
    error_message = "Retention days must be between 1 and 180."
  }
}

variable "create_oke_log" {
  description = "Whether to create the OKE control plane SERVICE log"
  type        = bool
  default     = false
}

variable "oke_cluster_id" {
  description = "OKE cluster OCID for control plane log collection (required when create_oke_log is true)"
  type        = string
  default     = null
}

variable "create_container_log" {
  description = "Whether to create the container stdout/stderr log with Unified Monitoring Agent"
  type        = bool
  default     = false
}

variable "object_storage_namespace" {
  description = "Object Storage namespace for log archive bucket"
  type        = string
  default     = ""
}

variable "create_archive_bucket" {
  description = "Create Object Storage bucket for log archival"
  type        = bool
  default     = true
}

variable "archive_retention_days" {
  description = "Archive retention duration in days (0 to disable)"
  type        = number
  default     = 365
}

variable "create_alert_topic" {
  description = "Create notification topic for alerts"
  type        = bool
  default     = true
}

variable "alert_email" {
  description = "Email address for alert notifications"
  type        = string
  default     = null
}

variable "create_alarms" {
  description = "Create monitoring alarms"
  type        = bool
  default     = true
}

variable "error_threshold" {
  description = "Error count threshold for alarm"
  type        = number
  default     = 10
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
