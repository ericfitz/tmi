# Variables for GCP Kubernetes (GKE Autopilot) Module

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

# GKE Cluster configuration
variable "network_id" {
  description = "VPC network ID"
  type        = string
}

variable "subnetwork_id" {
  description = "Subnetwork ID"
  type        = string
}

variable "pods_range_name" {
  description = "Name of the secondary IP range for pods"
  type        = string
}

variable "services_range_name" {
  description = "Name of the secondary IP range for services"
  type        = string
}

variable "enable_private_cluster" {
  description = "Enable private GKE cluster (nodes have no public IPs)"
  type        = bool
  default     = false
}

variable "enable_private_endpoint" {
  description = "Enable private endpoint (K8s API only accessible from VPC)"
  type        = bool
  default     = false
}

variable "master_ipv4_cidr_block" {
  description = "CIDR block for the GKE master (required for private clusters)"
  type        = string
  default     = "172.16.0.0/28"
}

variable "master_authorized_cidrs" {
  description = "List of CIDRs authorized to access the Kubernetes API"
  type = list(object({
    name = string
    cidr = string
  }))
  default = []
}

variable "deletion_protection" {
  description = "Enable deletion protection for the GKE cluster"
  type        = bool
  default     = false
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
  default     = "500m"
}

variable "tmi_memory_request" {
  description = "Memory request for TMI API pods"
  type        = string
  default     = "1Gi"
}

variable "tmi_cpu_limit" {
  description = "CPU limit for TMI API pods"
  type        = string
  default     = "2"
}

variable "tmi_memory_limit" {
  description = "Memory limit for TMI API pods"
  type        = string
  default     = "4Gi"
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
  default     = "250m"
}

variable "redis_memory_request" {
  description = "Memory request for Redis pod"
  type        = string
  default     = "512Mi"
}

variable "redis_cpu_limit" {
  description = "CPU limit for Redis pod"
  type        = string
  default     = "1"
}

variable "redis_memory_limit" {
  description = "Memory limit for Redis pod"
  type        = string
  default     = "2Gi"
}

# Database configuration
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

variable "db_host" {
  description = "Database host (Cloud SQL IP address or connection name)"
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

# Build mode
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

# Load Balancer configuration
variable "enable_internal_lb" {
  description = "Use an internal load balancer (private templates)"
  type        = bool
  default     = false
}

variable "labels" {
  description = "Labels to apply to GCP resources"
  type        = map(string)
  default     = {}
}
