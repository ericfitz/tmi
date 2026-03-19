# Variables for Azure Logging Module

variable "resource_group_name" {
  description = "Name of the Azure resource group"
  type        = string
}

variable "location" {
  description = "Azure region for resources"
  type        = string
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
    condition     = var.retention_days >= 30 && var.retention_days <= 730
    error_message = "Retention days must be between 30 and 730."
  }
}

variable "enable_container_insights" {
  description = "Enable Container Insights solution"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
