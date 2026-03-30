# Variables for TMI OCI Public Deployment

# ---------------------------------------------------------------------------
# OCI Configuration
# ---------------------------------------------------------------------------
variable "region" {
  description = "OCI region"
  type        = string
  default     = "us-ashburn-1"
}

variable "tenancy_ocid" {
  description = "OCI tenancy OCID"
  type        = string
}

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "oci_config_profile" {
  description = "OCI CLI config profile name"
  type        = string
  default     = "DEFAULT"
}

variable "kubeconfig_path" {
  description = "Path to kubeconfig file for the kubernetes provider"
  type        = string
  default     = "~/.kube/config"
}

variable "kubeconfig_context" {
  description = "Kubeconfig context name for the OKE cluster. Set to null to use the current context."
  type        = string
  default     = null
}

# ---------------------------------------------------------------------------
# Naming
# ---------------------------------------------------------------------------
variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "dns_label" {
  description = "DNS label for VCN"
  type        = string
  default     = "tmi"
}

# ---------------------------------------------------------------------------
# Network Configuration
# ---------------------------------------------------------------------------
variable "vcn_cidr" {
  description = "CIDR block for the VCN"
  type        = string
  default     = "10.0.0.0/16"
}

variable "public_subnet_cidr" {
  description = "CIDR block for the public subnet"
  type        = string
  default     = "10.0.1.0/24"
}

variable "private_subnet_cidr" {
  description = "CIDR block for the private subnet"
  type        = string
  default     = "10.0.2.0/24"
}

variable "database_subnet_cidr" {
  description = "CIDR block for the database subnet"
  type        = string
  default     = "10.0.3.0/24"
}

variable "oke_api_subnet_cidr" {
  description = "CIDR block for the OKE API endpoint subnet"
  type        = string
  default     = "10.0.4.0/28"
}

variable "oke_pod_subnet_cidr" {
  description = "CIDR block for the OKE pod subnet"
  type        = string
  default     = "10.0.5.0/24"
}

variable "oke_api_authorized_cidrs" {
  description = "List of CIDRs authorized to access the Kubernetes API endpoint"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

# ---------------------------------------------------------------------------
# OKE Configuration
# ---------------------------------------------------------------------------
variable "kubernetes_version" {
  description = "Kubernetes version for the OKE cluster"
  type        = string
  default     = "v1.34.2"
}

variable "node_ocpus" {
  description = "Number of OCPUs per node (Always Free allows up to 4 A1 OCPUs total)"
  type        = number
  default     = 2
}

variable "node_memory_gbs" {
  description = "Memory in GBs per node (Always Free allows up to 24 GB total)"
  type        = number
  default     = 12
}

variable "node_image_id" {
  description = "OCID of the OKE node image (arm64 for Always Free A1 shape)"
  type        = string
}

# ---------------------------------------------------------------------------
# Database Configuration
# ---------------------------------------------------------------------------
variable "db_name" {
  description = "Database name (alphanumeric, max 14 characters)"
  type        = string
  default     = "tmidb"
}

variable "is_free_tier" {
  description = "Deploy Autonomous Database on OCI Always Free tier"
  type        = bool
  default     = true
}

variable "db_username" {
  description = "Database username"
  type        = string
  default     = "ADMIN"
}

variable "db_password" {
  description = "Database password. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

# ---------------------------------------------------------------------------
# Secrets
# ---------------------------------------------------------------------------
variable "redis_password" {
  description = "Redis password. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

variable "jwt_secret" {
  description = "JWT signing secret. If not provided, a 64-character random secret is generated."
  type        = string
  sensitive   = true
  default     = null
}

# ---------------------------------------------------------------------------
# Container Images
# ---------------------------------------------------------------------------
variable "tmi_image_url" {
  description = "Container image URL for TMI server (arm64 for Always Free)"
  type        = string
}

variable "redis_image_url" {
  description = "Container image URL for Redis (arm64 for Always Free)"
  type        = string
}

# ---------------------------------------------------------------------------
# TMI-UX Frontend Configuration (optional)
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# Deployer Customization
# ---------------------------------------------------------------------------
variable "extra_env_vars" {
  description = "Additional environment variables merged into the TMI ConfigMap. Use this for OAuth provider config, feature flags, etc."
  type        = map(string)
  default     = {}
}

# ---------------------------------------------------------------------------
# Alerting
# ---------------------------------------------------------------------------
variable "alert_email" {
  description = "Email address for alert notifications (optional)"
  type        = string
  default     = null
}

# ---------------------------------------------------------------------------
# Certificate Automation (Let's Encrypt)
# ---------------------------------------------------------------------------
variable "enable_certificate_automation" {
  description = "Enable automatic Let's Encrypt certificate management"
  type        = bool
  default     = false
}

variable "domain_name" {
  description = "Domain name for TLS certificate (must be in the DNS zone)"
  type        = string
  default     = null
}

variable "api_hostname" {
  description = "Hostname for API ingress (e.g., api.oci.tmi.dev). Enables single-LB ingress routing."
  type        = string
  default     = null
}

variable "ux_hostname" {
  description = "Hostname for UX frontend ingress (e.g., app.oci.tmi.dev)"
  type        = string
  default     = null
}

variable "dns_zone_id" {
  description = "OCID of the OCI DNS zone for the domain"
  type        = string
  default     = null
}

variable "acme_contact_email" {
  description = "Email address for Let's Encrypt account and notifications"
  type        = string
  default     = null
}

variable "acme_directory" {
  description = "ACME directory URL: staging or production"
  type        = string
  default     = "staging"

  validation {
    condition     = contains(["staging", "production"], var.acme_directory)
    error_message = "ACME directory must be 'staging' or 'production'."
  }
}

variable "certificate_renewal_days" {
  description = "Days before certificate expiry to trigger renewal"
  type        = number
  default     = 30

  validation {
    condition     = var.certificate_renewal_days >= 7 && var.certificate_renewal_days <= 60
    error_message = "Certificate renewal days must be between 7 and 60."
  }
}

variable "certmgr_image_url" {
  description = "Container image URL for the certificate manager function"
  type        = string
  default     = null
}

# ---------------------------------------------------------------------------
# Object Storage Bucket Names (optional overrides)
# ---------------------------------------------------------------------------
variable "log_archive_bucket_name" {
  description = "Override name for the log archive bucket. Default: {name_prefix}-{compartment_name}-log-archive"
  type        = string
  default     = null
}

variable "wallet_bucket_name" {
  description = "Override name for the database wallet bucket. Default: {name_prefix}-{compartment_name}-wallet"
  type        = string
  default     = null
}

# ---------------------------------------------------------------------------
# Tags
# ---------------------------------------------------------------------------
variable "tags" {
  description = "Additional freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
