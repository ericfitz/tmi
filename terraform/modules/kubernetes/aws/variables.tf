# Variables for AWS Kubernetes (EKS) Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

# EKS Cluster configuration
variable "kubernetes_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
  default     = "1.31"
}

variable "endpoint_public_access" {
  description = "Whether the EKS API endpoint is publicly accessible"
  type        = bool
  default     = true
}

variable "public_access_cidrs" {
  description = "CIDRs allowed to access the EKS public API endpoint"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

# Node Group configuration
variable "node_count" {
  description = "Number of nodes in the managed node group"
  type        = number
  default     = 1
}

variable "node_instance_type" {
  description = "EC2 instance type for managed nodes"
  type        = string
  default     = "t3.medium"
}

# Network configuration
variable "vpc_id" {
  description = "ID of the VPC"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs for EKS cluster (must span at least 2 AZs)"
  type        = list(string)
}

variable "node_subnet_ids" {
  description = "Subnet IDs for EKS node group placement"
  type        = list(string)
}

variable "cluster_security_group_ids" {
  description = "Security group IDs for the EKS cluster"
  type        = list(string)
  default     = []
}

variable "alb_subnet_ids" {
  description = "Subnet IDs for ALB placement (public subnets for internet-facing, private for internal)"
  type        = list(string)
  default     = []
}

variable "alb_scheme" {
  description = "ALB scheme: internet-facing or internal"
  type        = string
  default     = "internet-facing"

  validation {
    condition     = contains(["internet-facing", "internal"], var.alb_scheme)
    error_message = "ALB scheme must be internet-facing or internal."
  }
}

# Secrets Manager ARNs (for IRSA policy)
variable "secret_arns" {
  description = "List of Secrets Manager ARNs the TMI pod should have access to"
  type        = list(string)
  default     = []
}

# TMI Server configuration
variable "tmi_image_url" {
  description = "Container image URL for TMI server"
  type        = string
}

variable "tmi_replicas" {
  description = "Number of TMI API pod replicas (TMI is stateful; use 1)"
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
variable "db_host" {
  description = "Database hostname"
  type        = string
}

variable "db_port" {
  description = "Database port"
  type        = number
  default     = 5432
}

variable "db_name" {
  description = "Database name"
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

# JWT configuration
variable "jwt_secret" {
  description = "JWT signing secret"
  type        = string
  sensitive   = true
}

# SSL configuration
variable "certificate_arn" {
  description = "ACM certificate ARN for ALB HTTPS listener (optional)"
  type        = string
  default     = null
}

# Load Balancer Controller
variable "lb_controller_chart_version" {
  description = "Helm chart version for AWS Load Balancer Controller"
  type        = string
  default     = "1.7.1"
}

variable "tags" {
  description = "Tags to apply to all AWS resources"
  type        = map(string)
  default     = {}
}
