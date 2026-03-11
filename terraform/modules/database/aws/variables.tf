# Variables for AWS Database Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "db_name" {
  description = "Name of the database to create"
  type        = string
  default     = "tmidb"

  validation {
    condition     = can(regex("^[a-zA-Z][a-zA-Z0-9_]{0,62}$", var.db_name))
    error_message = "Database name must start with a letter, contain only alphanumeric characters and underscores, and be 1-63 characters."
  }
}

variable "db_username" {
  description = "Master username for the database"
  type        = string
  default     = "tmi"

  validation {
    condition     = can(regex("^[a-zA-Z][a-zA-Z0-9_]{0,62}$", var.db_username))
    error_message = "Username must start with a letter, contain only alphanumeric characters and underscores, and be 1-63 characters."
  }
}

variable "db_password" {
  description = "Master password for the database"
  type        = string
  sensitive   = true

  validation {
    condition     = length(var.db_password) >= 8 && length(var.db_password) <= 128
    error_message = "Password must be 8-128 characters."
  }
}

variable "engine_version" {
  description = "PostgreSQL engine version"
  type        = string
  default     = "16.4"
}

variable "instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t4g.micro"
}

variable "allocated_storage" {
  description = "Initial allocated storage in GB"
  type        = number
  default     = 20
}

variable "max_allocated_storage" {
  description = "Maximum allocated storage in GB for autoscaling (set to 0 to disable autoscaling)"
  type        = number
  default     = 100
}

variable "multi_az" {
  description = "Enable Multi-AZ deployment for high availability"
  type        = bool
  default     = false
}

variable "db_subnet_group_name" {
  description = "Name of the DB subnet group (created in network module)"
  type        = string
}

variable "vpc_security_group_ids" {
  description = "List of VPC security group IDs to associate with the instance"
  type        = list(string)
}

variable "deletion_protection" {
  description = "Enable deletion protection"
  type        = bool
  default     = true
}

variable "skip_final_snapshot" {
  description = "Skip final snapshot when destroying the instance"
  type        = bool
  default     = false
}

variable "backup_retention_period" {
  description = "Number of days to retain automated backups"
  type        = number
  default     = 7
}

variable "backup_window" {
  description = "Preferred backup window (UTC, format: hh24:mi-hh24:mi)"
  type        = string
  default     = "03:00-04:00"
}

variable "maintenance_window" {
  description = "Preferred maintenance window (UTC, format: ddd:hh24:mi-ddd:hh24:mi)"
  type        = string
  default     = "sun:04:30-sun:05:30"
}

variable "performance_insights_enabled" {
  description = "Enable Performance Insights (free for db.t4g.micro)"
  type        = bool
  default     = true
}

variable "apply_immediately" {
  description = "Apply changes immediately instead of during the next maintenance window"
  type        = bool
  default     = false
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
