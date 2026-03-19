# Variables for TMI OCI Private Deployment

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
  description = "CIDR block for the public subnet (used for NAT/Service gateways)"
  type        = string
  default     = "10.0.1.0/24"
}

variable "private_subnet_cidr" {
  description = "CIDR block for the private subnet (nodes, internal LB)"
  type        = string
  default     = "10.0.2.0/24"
}

variable "database_subnet_cidr" {
  description = "CIDR block for the database subnet (private endpoint)"
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

variable "deployer_ip" {
  description = "Deployer's public IP for temporary K8s API access during provisioning. Auto-detected if null."
  type        = string
  default     = null
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
  description = "Number of OCPUs per node (for flex shapes)"
  type        = number
  default     = 2
}

variable "node_memory_gbs" {
  description = "Memory in GBs per node (for flex shapes)"
  type        = number
  default     = 16
}

variable "node_image_id" {
  description = "OCID of the OKE node image (arm64 for A1 shape)"
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

variable "db_compute_count" {
  description = "Number of ECPUs for ADB (non-free tier)"
  type        = number
  default     = 2
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
  description = "Container image URL for TMI server"
  type        = string
}

variable "redis_image_url" {
  description = "Container image URL for Redis"
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
  description = "Additional environment variables merged into the TMI ConfigMap. Use for OAuth config, WebSocket origins, feature flags, etc."
  type        = map(string)
  default     = {}
}

# ---------------------------------------------------------------------------
# Load Balancer Configuration
# ---------------------------------------------------------------------------
variable "lb_min_bandwidth_mbps" {
  description = "Minimum bandwidth for internal load balancer in Mbps"
  type        = number
  default     = 10
}

variable "lb_max_bandwidth_mbps" {
  description = "Maximum bandwidth for internal load balancer in Mbps"
  type        = number
  default     = 100
}

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
variable "log_retention_days" {
  description = "Log retention duration in days"
  type        = number
  default     = 90
}

variable "log_archive_retention_days" {
  description = "Archive log retention duration in days"
  type        = number
  default     = 365
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
  default     = "production"

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
# tmi-tf-wh Webhook Analyzer (optional)
# ---------------------------------------------------------------------------
variable "tmi_tf_wh_enabled" {
  description = "Enable tmi-tf-wh webhook analyzer deployment"
  type        = bool
  default     = false
}

variable "tmi_tf_wh_image_url" {
  description = "Container image URL for tmi-tf-wh"
  type        = string
  default     = null
}

variable "tmi_tf_wh_extra_env_vars" {
  description = "Additional environment variables for tmi-tf-wh"
  type        = map(string)
  default     = {}
}

# ---------------------------------------------------------------------------
# Tags
# ---------------------------------------------------------------------------
variable "tags" {
  description = "Additional freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
