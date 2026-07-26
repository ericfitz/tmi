# Variables for AWS Kubernetes (EKS) Module

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

# EKS Cluster configuration
#
# 1.36 is the current EKS default version (`aws eks describe-cluster-versions`)
# and is in STANDARD_SUPPORT until 2027-08-01. The previous pin (1.31) had
# fallen into EXTENDED_SUPPORT, which is both surcharged and a hard deadline —
# AWS force-upgrades a cluster when extended support ends.
#
# NOTE for operators: EKS upgrades ONE minor version at a time (control plane
# and node group both), so moving an existing cluster to this pin may require
# several sequential `terraform apply` runs, not one. Terraform will happily
# emit a plan that jumps multiple minors; the AWS API rejects it. Upgrade the
# node group to match the control plane at each hop. A control-plane minor
# upgrade can be rolled back only within 7 days.
#
# nullable = false is load-bearing, not decoration: since Terraform 1.1
# variables are nullable by default, so a caller passing an unset (null)
# value would propagate null in here rather than falling back to this
# default — which surfaces as "Missing required argument" from the
# aws_eks_addon_version data sources below. With nullable = false, Terraform
# substitutes this default whenever the caller passes null, which is what lets
# environments/aws-public declare `kubernetes_version = var.kubernetes_version`
# with a null default and get the pin from here.
variable "kubernetes_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
  default     = "1.36"
  nullable    = false
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

variable "everyone_is_a_reviewer" {
  description = "Grant every authenticated user security-reviewer capability (TMI_AUTH_EVERYONE_IS_A_REVIEWER). Decoupled from build mode so it can be set independently of dev/production."
  type        = bool
  default     = false
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
#
# Chart 1.17.1 == controller app v2.17.1, the newest release of the 2.x line.
# Bumped from 1.7.1 (app v2.7.1, built against Kubernetes 1.29 client libs)
# because the cluster now pins Kubernetes 1.36 — a controller that far behind
# the API server is outside any tested skew, and its admission webhooks are
# the failure mode that takes the Ingress (and therefore the ALB) down.
#
# Deliberately NOT the 3.x line (chart 3.x == app v3.x): v3 renumbered the
# chart to match the app version, requires CRDs to be applied out-of-band
# before `helm upgrade` (including Gateway API CRDs, even when Gateway API is
# unused — kubernetes-sigs/aws-load-balancer-controller#4674), and ships a
# further-changed IAM policy. None of that buys this deployment anything: it
# uses a single ALB Ingress with no Gateway API. Revisit when the 2.x line
# stops receiving updates.
variable "lb_controller_chart_version" {
  description = "Helm chart version for AWS Load Balancer Controller (chart 1.x == controller v2.x)"
  type        = string
  default     = "1.17.1"
}

variable "lb_controller_chart_local_path" {
  description = "Absolute path to a vendored aws-load-balancer-controller .tgz chart. When set, the chart is installed from this local file instead of the remote https://aws.github.io/eks-charts repo (air-gapped / flaky-registry escape hatch). Empty (default) uses the remote repo."
  type        = string
  default     = ""
}

variable "tags" {
  description = "Tags to apply to all AWS resources"
  type        = map(string)
  default     = {}
}
