# Variables for AWS Kubernetes (EKS) Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "kubernetes_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
  default     = "1.30"
}

# Network configuration
variable "vpc_id" {
  description = "ID of the VPC"
  type        = string
}

variable "private_subnet_ids" {
  description = "List of private subnet IDs for EKS cluster and Fargate pods"
  type        = list(string)
}

variable "public_subnet_ids" {
  description = "List of public subnet IDs for EKS cluster public endpoint"
  type        = list(string)
}

variable "eks_security_group_ids" {
  description = "Additional security group IDs for the EKS cluster"
  type        = list(string)
  default     = []
}

variable "fargate_subnet_ids" {
  description = "List of private subnet IDs for Fargate pods"
  type        = list(string)
}

variable "authorized_cidrs" {
  description = "CIDR blocks authorized for public API server access"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

# Encryption configuration
variable "kms_key_arn" {
  description = "ARN of KMS key for EKS secrets encryption (optional)"
  type        = string
  default     = null
}

# AWS region
variable "aws_region" {
  description = "AWS region for secrets and other regional services"
  type        = string
}

# TMI Server configuration
variable "tmi_image_url" {
  description = "Container image URL for TMI server"
  type        = string
}

variable "tmi_replicas" {
  description = "Number of TMI API pod replicas (TMI is single-instance only)"
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

variable "db_endpoint" {
  description = "Database endpoint (host:port)"
  type        = string
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "tmi"
}

# Secrets configuration
variable "secrets_secret_name" {
  description = "AWS Secrets Manager combined secret name"
  type        = string
  default     = "tmi/secrets"
}

variable "jwt_secret" {
  description = "JWT signing secret for authentication"
  type        = string
  sensitive   = true
}

variable "oauth_client_secret" {
  description = "OAuth provider client secret (must differ from jwt_secret)"
  type        = string
  sensitive   = true
}

# Logging configuration
variable "log_level" {
  description = "Log level for TMI server"
  type        = string
  default     = "info"

  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.log_level)
    error_message = "Log level must be debug, info, warn, or error."
  }
}

variable "cloudwatch_log_group" {
  description = "CloudWatch log group name for cloud logging (enables CloudWatch logging and Fargate log router when set)"
  type        = string
  default     = null
}

variable "logging_policy_arn" {
  description = "ARN of IAM policy granting CloudWatch Logs write access (attached to Fargate role for log router)"
  type        = string
  default     = null
}

variable "rds_security_group_id" {
  description = "ID of the RDS security group (for adding cluster SG ingress rule)"
  type        = string
  default     = null
}

variable "redis_security_group_id" {
  description = "ID of the Redis security group (for adding cluster SG ingress rule)"
  type        = string
  default     = null
}

variable "cloud_log_level" {
  description = "Minimum log level for cloud logging (defaults to log_level if not set)"
  type        = string
  default     = null

  validation {
    condition     = var.cloud_log_level == null || contains(["debug", "info", "warn", "error"], var.cloud_log_level)
    error_message = "Cloud log level must be debug, info, warn, or error."
  }
}

variable "tmi_build_mode" {
  description = "TMI build mode (dev, staging, production)"
  type        = string
  default     = "production"

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

# SSL configuration
variable "enable_ingress" {
  description = "Enable ALB Ingress for HTTPS with host-based routing (set to true when certificate and domains are configured)"
  type        = bool
  default     = false
}

variable "ssl_certificate_arn" {
  description = "ARN of ACM certificate for HTTPS on the load balancer (optional)"
  type        = string
  default     = null
}

# Domain configuration (for ALB Ingress host-based routing)
variable "server_domain" {
  description = "Domain name for the TMI API server (e.g., tmiserver.efitz.net)"
  type        = string
  default     = null
}

variable "ux_domain" {
  description = "Domain name for the TMI-UX frontend (e.g., tmi.efitz.net)"
  type        = string
  default     = null
}

variable "ssl_certificate_pem" {
  description = "PEM-encoded SSL certificate for K8s TLS secret (optional)"
  type        = string
  default     = null
  sensitive   = true
}

variable "ssl_private_key_pem" {
  description = "PEM-encoded SSL private key for K8s TLS secret (optional)"
  type        = string
  default     = null
  sensitive   = true
}

# Tags
variable "tags" {
  description = "Tags to apply to all AWS resources"
  type        = map(string)
  default     = {}
}

# TMI-UX configuration
variable "tmi_ux_enabled" {
  description = "Enable TMI-UX frontend deployment"
  type        = bool
  default     = false
}

variable "tmi_ux_image_url" {
  description = "Container image URL for TMI-UX frontend"
  type        = string
  default     = null
}

variable "tmi_ux_replicas" {
  description = "Number of TMI-UX pod replicas"
  type        = number
  default     = 1
}
