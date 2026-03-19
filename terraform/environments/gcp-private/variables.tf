# Variables for TMI GCP Private Deployment

# GCP Configuration
variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

# Naming
variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

# Network Configuration
variable "primary_subnet_cidr" {
  description = "CIDR block for the primary subnet"
  type        = string
  default     = "10.0.0.0/24"
}

variable "pods_cidr" {
  description = "CIDR block for GKE pod IPs"
  type        = string
  default     = "10.1.0.0/16"
}

variable "services_cidr" {
  description = "CIDR block for GKE service IPs"
  type        = string
  default     = "10.2.0.0/20"
}

variable "master_ipv4_cidr_block" {
  description = "CIDR block for the GKE master (private cluster)"
  type        = string
  default     = "172.16.0.0/28"
}

variable "private_ingress_cidrs" {
  description = "CIDRs allowed to access the internal load balancer"
  type        = list(string)
  default     = []
}

# Database Configuration
variable "database_name" {
  description = "Name of the PostgreSQL database"
  type        = string
  default     = "tmi"
}

variable "db_username" {
  description = "Database username"
  type        = string
  default     = "tmi"
}

variable "db_password" {
  description = "Database password. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

# Secrets Configuration
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

# Container Images
variable "tmi_image_url" {
  description = "Container image URL for TMI server"
  type        = string
}

variable "redis_image_url" {
  description = "Container image URL for Redis"
  type        = string
}

# Extra Environment Variables
variable "extra_env_vars" {
  description = "Additional environment variables merged into the TMI ConfigMap"
  type        = map(string)
  default     = {}
}

# Labels
variable "labels" {
  description = "Additional labels to apply to all resources"
  type        = map(string)
  default     = {}
}
