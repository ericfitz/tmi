# Variables for TMI Azure Private Deployment

# Azure Configuration
variable "subscription_id" {
  description = "Azure subscription ID"
  type        = string
}

variable "location" {
  description = "Azure region for resources"
  type        = string
  default     = "eastus"
}

# Naming
variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

# Network Configuration
variable "vnet_cidr" {
  description = "CIDR block for the Virtual Network"
  type        = string
  default     = "10.0.0.0/16"
}

variable "aks_subnet_cidr" {
  description = "CIDR block for the AKS subnet"
  type        = string
  default     = "10.0.1.0/24"
}

variable "database_subnet_cidr" {
  description = "CIDR block for the database subnet"
  type        = string
  default     = "10.0.2.0/24"
}

variable "allowed_cidr" {
  description = "CIDR block allowed for inbound traffic to the private cluster"
  type        = string
  default     = "10.0.0.0/16"
}

# AKS Configuration
variable "kubernetes_version" {
  description = "Kubernetes version for the AKS cluster"
  type        = string
  default     = "1.29"
}

# Database Configuration
variable "db_name" {
  description = "Name of the TMI database"
  type        = string
  default     = "tmi"
}

variable "db_username" {
  description = "Database administrator username"
  type        = string
  default     = "tmiadmin"

  validation {
    condition     = !contains(["admin", "administrator", "root", "sa", "postgres"], lower(var.db_username))
    error_message = "Admin username cannot be a reserved name (admin, administrator, root, sa, postgres)."
  }
}

variable "db_password" {
  description = "Database password. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

# Secrets
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
  description = "Additional environment variables for TMI server (merged into ConfigMap)"
  type        = map(string)
  default     = {}
}

# Tags
variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
