# Variables for GCP Database Module

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "database_version" {
  description = "PostgreSQL version for Cloud SQL"
  type        = string
  default     = "POSTGRES_16"
}

variable "tier" {
  description = "Machine type for the Cloud SQL instance"
  type        = string
  default     = "db-custom-1-3840"
}

variable "availability_type" {
  description = "Availability type: ZONAL or REGIONAL"
  type        = string
  default     = "ZONAL"

  validation {
    condition     = contains(["ZONAL", "REGIONAL"], var.availability_type)
    error_message = "Availability type must be ZONAL or REGIONAL."
  }
}

variable "disk_size_gb" {
  description = "Disk size in GB"
  type        = number
  default     = 10
}

variable "disk_autoresize" {
  description = "Enable automatic disk size increase"
  type        = bool
  default     = true
}

variable "deletion_protection" {
  description = "Enable deletion protection for the Cloud SQL instance"
  type        = bool
  default     = false
}

variable "enable_public_ip" {
  description = "Assign a public IP to the Cloud SQL instance"
  type        = bool
  default     = true
}

variable "private_network_id" {
  description = "VPC network ID for private IP connectivity (required when enable_public_ip is false)"
  type        = string
  default     = null
}

variable "authorized_networks" {
  description = "List of authorized networks for Cloud SQL access"
  type = list(object({
    name = string
    cidr = string
  }))
  default = []
}

variable "database_name" {
  description = "Name of the database to create"
  type        = string
  default     = "tmi"
}

variable "db_username" {
  description = "Database username"
  type        = string
  default     = "tmi"
}

variable "db_password" {
  description = "Database password"
  type        = string
  sensitive   = true
}

variable "enable_backups" {
  description = "Enable automated backups"
  type        = bool
  default     = true
}

variable "backup_retained_count" {
  description = "Number of backups to retain"
  type        = number
  default     = 7
}

variable "labels" {
  description = "Labels to apply to all resources"
  type        = map(string)
  default     = {}
}
