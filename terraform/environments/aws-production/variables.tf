# Variables for TMI AWS Production Deployment

# AWS Configuration
variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

# Naming
variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

# Network Configuration
variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "public_subnet_cidrs" {
  description = "CIDR blocks for public subnets (one per AZ)"
  type        = list(string)
  default     = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
}

variable "private_subnet_cidrs" {
  description = "CIDR blocks for private subnets (one per AZ)"
  type        = list(string)
  default     = ["10.0.4.0/24", "10.0.5.0/24", "10.0.6.0/24"]
}

variable "database_subnet_cidrs" {
  description = "CIDR blocks for database subnets (one per AZ)"
  type        = list(string)
  default     = ["10.0.7.0/24", "10.0.8.0/24", "10.0.9.0/24"]
}

variable "enable_multi_az_nat" {
  description = "Enable NAT Gateway in each AZ for high availability (increases cost)"
  type        = bool
  default     = false
}

variable "eks_api_authorized_cidrs" {
  description = "List of CIDRs authorized to access the Kubernetes API endpoint"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

# EKS Configuration
variable "kubernetes_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
  default     = "1.30"
}

variable "tmi_replicas" {
  description = "Number of TMI API pod replicas"
  type        = number
  default     = 2
}

# Database Configuration
variable "db_name" {
  description = "Database name"
  type        = string
  default     = "tmidb"
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

variable "db_engine_version" {
  description = "PostgreSQL engine version"
  type        = string
  default     = "16.6"
}

variable "db_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t4g.micro"
}

variable "db_allocated_storage" {
  description = "Initial allocated storage in GB"
  type        = number
  default     = 20
}

variable "db_max_allocated_storage" {
  description = "Maximum allocated storage in GB for autoscaling"
  type        = number
  default     = 100
}

variable "db_multi_az" {
  description = "Enable Multi-AZ deployment for RDS"
  type        = bool
  default     = false
}

variable "db_deletion_protection" {
  description = "Enable deletion protection for RDS"
  type        = bool
  default     = true
}

variable "db_skip_final_snapshot" {
  description = "Skip final snapshot when destroying the RDS instance"
  type        = bool
  default     = false
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

# Build Configuration
variable "tmi_build_mode" {
  description = "TMI build mode (dev, staging, production)"
  type        = string
  default     = "production"

  validation {
    condition     = contains(["dev", "staging", "production"], var.tmi_build_mode)
    error_message = "Build mode must be dev, staging, or production."
  }
}

# Container Images
variable "tmi_image_url" {
  description = "Container image URL for TMI server (ECR or public registry)"
  type        = string
}

variable "redis_image_url" {
  description = "Container image URL for Redis"
  type        = string
}

# TMI-UX Frontend Configuration
variable "tmi_ux_enabled" {
  description = "Enable TMI-UX frontend container deployment"
  type        = bool
  default     = false
}

variable "tmi_ux_image_url" {
  description = "Container image URL for TMI-UX frontend"
  type        = string
  default     = null
}

# Certificate Configuration (ACM)
variable "enable_certificate_automation" {
  description = "Enable ACM certificate for HTTPS"
  type        = bool
  default     = false
}

variable "domain_name" {
  description = "Domain name for the ACM certificate"
  type        = string
  default     = null
}

variable "subject_alternative_names" {
  description = "Subject alternative names for the ACM certificate"
  type        = list(string)
  default     = []
}

variable "dns_zone_id" {
  description = "Route 53 hosted zone ID for DNS validation"
  type        = string
  default     = null
}

# Alerting
variable "alert_email" {
  description = "Email address for alert notifications"
  type        = string
  default     = null
}

# Tags
variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
