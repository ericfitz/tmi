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
  description = "(Unused by this module; kept for aws-private compatibility — see the note above tmi_image_url) Subnet IDs for ALB placement"
  type        = list(string)
  default     = []
}

variable "alb_scheme" {
  description = "(Unused by this module; kept for aws-private compatibility — see the note above tmi_image_url) ALB scheme: internet-facing or internal"
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
#
# tmi_image_url / tmi_replicas / alb_scheme / alb_subnet_ids below are no
# longer consumed inside this module (the TMI API Deployment/Service/Ingress
# moved to the deployments/k8s/dev/aws kustomize overlay — see the note atop
# k8s_resources.tf). They are KEPT here (with defaults, so aws-public can
# simply stop passing them) solely because terraform/environments/aws-private
# still passes them explicitly to this module; removing the declarations
# would break `terraform validate` for aws-private, which this refactor must
# not touch. Follow-up: once aws-private is refactored the same way (out of
# scope here), these can be deleted for real.
variable "tmi_image_url" {
  description = "(Unused by this module; kept for aws-private compatibility) Container image URL for TMI server"
  type        = string
  default     = null
}

variable "tmi_replicas" {
  description = "(Unused by this module; kept for aws-private compatibility) Number of TMI API pod replicas"
  type        = number
  default     = 1
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
  description = "(Unused by this module; kept for aws-private compatibility — see the note above tmi_image_url) Container image URL for Redis"
  type        = string
  default     = null
}

variable "redis_password" {
  description = "Redis password (feeds the tmi-secrets Secret's TMI_REDIS_PASSWORD key)"
  type        = string
  sensitive   = true
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

# NOTE: certificate_arn was removed — TLS termination is now configured on
# the Ingress annotations owned by the deployments/k8s/dev/aws overlay (Task
# 5/6), not by this module. terraform/modules/certificates/aws still creates
# and DNS-validates the ACM certificate; its ARN flows to the overlay via the
# deploy script, not through this module. Neither aws-public nor aws-private
# passed this variable explicitly, so removing it does not break either
# environment.

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
