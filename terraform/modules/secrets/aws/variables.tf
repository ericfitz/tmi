# Variables for AWS Secrets Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "db_username" {
  description = "Database username to store in secrets"
  type        = string
  default     = "tmi"
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
