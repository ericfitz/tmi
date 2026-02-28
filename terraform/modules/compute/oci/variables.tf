# Variables for OCI Compute Module (ARM VM-based deployment)

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "availability_domain" {
  description = "Availability domain for the VM (defaults to first AD)"
  type        = string
  default     = null
}

# Network configuration
variable "private_subnet_id" {
  description = "OCID of the private subnet for the VM"
  type        = string
}

variable "public_subnet_ids" {
  description = "List of public subnet OCIDs for load balancer"
  type        = list(string)
}

variable "tmi_nsg_ids" {
  description = "List of NSG OCIDs for TMI server VM"
  type        = list(string)
  default     = []
}

variable "redis_nsg_ids" {
  description = "List of NSG OCIDs (also applied to VM since Redis runs locally)"
  type        = list(string)
  default     = []
}

variable "lb_nsg_ids" {
  description = "List of NSG OCIDs for load balancer"
  type        = list(string)
  default     = []
}

# VM configuration
variable "vm_ocpus" {
  description = "Number of OCPUs for the ARM VM (free tier: up to 4 across all A1.Flex)"
  type        = number
  default     = 4
}

variable "vm_memory_gb" {
  description = "Memory in GB for the ARM VM (free tier: up to 24 GB across all A1.Flex)"
  type        = number
  default     = 24
}

variable "boot_volume_size_gb" {
  description = "Boot volume size in GB"
  type        = number
  default     = 50
}

variable "ssh_authorized_keys" {
  description = "SSH public key(s) for VM access (optional, for debugging)"
  type        = string
  default     = null
}

# TMI image configuration
variable "tmi_image_url" {
  description = "Container image URL for TMI server (must be linux/arm64)"
  type        = string
}

# Redis Docker image (used directly by Docker on the VM — must be arm64-compatible)
variable "redis_docker_image" {
  description = "Docker image for Redis (must be multi-arch or arm64). Defaults to Chainguard Redis."
  type        = string
  default     = "cgr.dev/chainguard/redis:latest"
}

# Kept for interface compatibility but not used (Redis runs as Docker on the VM)
variable "redis_image_url" {
  description = "Unused in VM mode (Redis runs as Docker container via redis_docker_image)"
  type        = string
  default     = ""
}

variable "redis_password" {
  description = "Redis password"
  type        = string
  sensitive   = true
}

# Database configuration (Oracle ADB)
variable "db_username" {
  description = "Database username"
  type        = string
  default     = "ADMIN"
}

variable "db_password" {
  description = "Database password"
  type        = string
  sensitive   = true
}

variable "oracle_connect_string" {
  description = "Oracle TNS alias from the wallet (e.g. tmidb_high)"
  type        = string
}

variable "wallet_par_url" {
  description = "Pre-authenticated request URL to download the Oracle ADB wallet ZIP"
  type        = string
  sensitive   = true
}

# Secrets / Auth
variable "vault_ocid" {
  description = "OCID of the OCI Vault (for TMI runtime secrets access)"
  type        = string
  default     = ""
}

variable "jwt_secret" {
  description = "JWT signing secret for authentication"
  type        = string
  sensitive   = true
}

# Logging
variable "log_level" {
  description = "Log level for TMI server"
  type        = string
  default     = "info"

  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.log_level)
    error_message = "Log level must be debug, info, warn, or error."
  }
}

variable "oci_log_id" {
  description = "OCID of the OCI Log for cloud logging (enables cloud logging when set)"
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
  description = "Additional environment variables for TMI server (unused in VM mode, kept for compatibility)"
  type        = map(string)
  default     = {}
}

# Load Balancer configuration
variable "lb_min_bandwidth_mbps" {
  description = "Minimum bandwidth for load balancer in Mbps (10 = free tier)"
  type        = number
  default     = 10
}

variable "lb_max_bandwidth_mbps" {
  description = "Maximum bandwidth for load balancer in Mbps"
  type        = number
  default     = 10
}

# SSL configuration
variable "ssl_certificate_pem" {
  description = "PEM-encoded SSL certificate (optional)"
  type        = string
  default     = null
  sensitive   = true
}

variable "ssl_private_key_pem" {
  description = "PEM-encoded SSL private key (optional)"
  type        = string
  default     = null
  sensitive   = true
}

variable "ssl_ca_certificate_pem" {
  description = "PEM-encoded SSL CA certificate (optional)"
  type        = string
  default     = null
  sensitive   = true
}

variable "enable_http_redirect" {
  description = "Enable HTTP to HTTPS redirect (only relevant when SSL is configured)"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
