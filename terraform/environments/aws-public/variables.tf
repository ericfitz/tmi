# Variables for TMI AWS Public Environment

variable "aws_region" {
  description = "AWS region for deployment"
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix for all resource names"
  type        = string
  default     = "tmi"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

# EKS only accepts ONE minor version bump per update, for the control plane and
# for the node group alike. Upgrading an existing cluster across several minors
# is therefore a sequence of applies, not one: set this to each intermediate
# version in turn (1.33, then 1.34, ...) and apply each time. Terraform already
# orders the work correctly within a hop — control plane, then node group, then
# the core addons — because of the dependencies between those resources.
# Leaving this unset takes the module default, which is the version this
# deployment is expected to converge on.
variable "kubernetes_version" {
  description = "Kubernetes version for the EKS cluster and node group. Bump one minor at a time when upgrading an existing cluster."
  type        = string
  default     = null
}

variable "db_name" {
  description = "Name of the PostgreSQL database"
  type        = string
  default     = "tmi"
}

variable "db_username" {
  description = "Master username for the database"
  type        = string
  default     = "tmi"
}

variable "tmi_image_tag" {
  description = "Docker image tag for the TMI server"
  type        = string
  default     = "latest"
}

variable "redis_image_tag" {
  description = "Docker image tag for Redis"
  type        = string
  default     = "latest"
}

variable "extra_env_vars" {
  description = "Additional environment variables for TMI server (merged into ConfigMap)"
  type        = map(string)
  default     = {}
}

# The TMI deployment uses api.tmi.dev, served out of the tmi.dev hosted zone
# (Z017072317XPHCAN503JC). It previously used server.aws.tmi.dev out of a
# delegated aws.tmi.dev zone; that delegation was retired once tmi.dev itself
# moved into this account. The browser client is a separate host
# (www.tmi.dev) — see TMI_CORS_ALLOWED_ORIGINS in
# terraform/modules/kubernetes/aws/k8s_resources.tf.
variable "domain_name" {
  description = "FQDN for the TMI server (e.g. api.example.com)"
  type        = string
}

variable "hosted_zone_id" {
  description = "Route 53 hosted zone ID that domain_name lives in (zone must be in this account)"
  type        = string
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}

variable "lb_controller_chart_local_path" {
  description = "Absolute path to a vendored aws-load-balancer-controller .tgz. When set, installs the ALB controller from this local file instead of the remote helm repo. Empty (default) uses the remote repo."
  type        = string
  default     = ""
}
