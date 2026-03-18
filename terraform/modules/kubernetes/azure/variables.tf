# Variables for Azure Kubernetes (AKS) Module

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

# AKS Cluster configuration
variable "kubernetes_version" {
  description = "Kubernetes version for the AKS cluster"
  type        = string
  default     = "1.29"
}

variable "aks_sku_tier" {
  description = "AKS SKU tier: Free or Standard"
  type        = string
  default     = "Free"

  validation {
    condition     = contains(["Free", "Standard"], var.aks_sku_tier)
    error_message = "AKS SKU tier must be Free or Standard."
  }
}

variable "node_count" {
  description = "Number of nodes in the default node pool"
  type        = number
  default     = 1
}

variable "node_vm_size" {
  description = "VM size for AKS nodes"
  type        = string
  default     = "Standard_B2s"
}

variable "os_disk_size_gb" {
  description = "OS disk size in GB for AKS nodes"
  type        = number
  default     = 30
}

variable "k8s_service_cidr" {
  description = "CIDR block for Kubernetes services"
  type        = string
  default     = "10.96.0.0/16"
}

variable "k8s_dns_service_ip" {
  description = "IP address for the Kubernetes DNS service (must be within service CIDR)"
  type        = string
  default     = "10.96.0.10"
}

# Network configuration
variable "aks_subnet_id" {
  description = "ID of the AKS subnet"
  type        = string
}

# Private cluster configuration
variable "private_cluster_enabled" {
  description = "Enable private cluster (API server accessible only from VNet)"
  type        = bool
  default     = false
}

variable "api_server_authorized_ip_ranges" {
  description = "List of authorized IP ranges for API server access (used during provisioning)"
  type        = list(string)
  default     = null
}

# ACR integration
variable "acr_id" {
  description = "ID of the Azure Container Registry for AKS-ACR integration"
  type        = string
  default     = null
}

# NGINX Ingress Controller
variable "nginx_ingress_chart_version" {
  description = "Helm chart version for NGINX Ingress Controller"
  type        = string
  default     = "4.9.0"
}

# TMI Server configuration
variable "tmi_image_url" {
  description = "Container image URL for TMI server"
  type        = string
}

variable "tmi_replicas" {
  description = "Number of TMI API pod replicas (TMI is stateful; use 1 to avoid database corruption)"
  type        = number
  default     = 1
}

variable "tmi_cpu_request" {
  description = "CPU request for TMI API pods"
  type        = string
  default     = "250m"
}

variable "tmi_memory_request" {
  description = "Memory request for TMI API pods"
  type        = string
  default     = "512Mi"
}

variable "tmi_cpu_limit" {
  description = "CPU limit for TMI API pods"
  type        = string
  default     = "1"
}

variable "tmi_memory_limit" {
  description = "Memory limit for TMI API pods"
  type        = string
  default     = "2Gi"
}

# Redis configuration
variable "redis_image_url" {
  description = "Container image URL for Redis"
  type        = string
}

variable "redis_password" {
  description = "Redis password"
  type        = string
  sensitive   = true
}

variable "redis_cpu_request" {
  description = "CPU request for Redis pod"
  type        = string
  default     = "100m"
}

variable "redis_memory_request" {
  description = "Memory request for Redis pod"
  type        = string
  default     = "256Mi"
}

variable "redis_cpu_limit" {
  description = "CPU limit for Redis pod"
  type        = string
  default     = "500m"
}

variable "redis_memory_limit" {
  description = "Memory limit for Redis pod"
  type        = string
  default     = "1Gi"
}

# Database configuration
variable "db_username" {
  description = "Database username"
  type        = string
  default     = "tmiadmin"
}

variable "db_password" {
  description = "Database password"
  type        = string
  sensitive   = true
}

variable "db_host" {
  description = "Database hostname (FQDN)"
  type        = string
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "tmi"
}

# Secrets configuration
variable "jwt_secret" {
  description = "JWT signing secret for authentication"
  type        = string
  sensitive   = true
}

# Build mode configuration
variable "tmi_build_mode" {
  description = "TMI build mode (dev, staging, production)"
  type        = string
  default     = "dev"

  validation {
    condition     = contains(["dev", "staging", "production"], var.tmi_build_mode)
    error_message = "Build mode must be dev, staging, or production."
  }
}

variable "extra_environment_variables" {
  description = "Additional environment variables for TMI server"
  type        = map(string)
  default     = {}
}

variable "tags" {
  description = "Tags to apply to all Azure resources"
  type        = map(string)
  default     = {}
}
