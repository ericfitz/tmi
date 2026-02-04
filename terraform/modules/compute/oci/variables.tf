# Variables for OCI Compute Module

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
  description = "Availability domain for container instances (defaults to first AD)"
  type        = string
  default     = null
}

# Network configuration
variable "private_subnet_id" {
  description = "OCID of the private subnet for container instances"
  type        = string
}

variable "public_subnet_ids" {
  description = "List of public subnet OCIDs for load balancer"
  type        = list(string)
}

variable "tmi_nsg_ids" {
  description = "List of NSG OCIDs for TMI server"
  type        = list(string)
  default     = []
}

variable "redis_nsg_ids" {
  description = "List of NSG OCIDs for Redis"
  type        = list(string)
  default     = []
}

variable "lb_nsg_ids" {
  description = "List of NSG OCIDs for load balancer"
  type        = list(string)
  default     = []
}

# TMI Server configuration
variable "tmi_image_url" {
  description = "Container image URL for TMI server"
  type        = string
}

variable "tmi_shape" {
  description = "Container instance shape for TMI server"
  type        = string
  default     = "CI.Standard.E4.Flex"
}

variable "tmi_ocpus" {
  description = "Number of OCPUs for TMI server"
  type        = number
  default     = 1
}

variable "tmi_memory_gb" {
  description = "Memory in GB for TMI server"
  type        = number
  default     = 4
}

# Redis configuration
variable "redis_image_url" {
  description = "Container image URL for Redis"
  type        = string
}

variable "redis_shape" {
  description = "Container instance shape for Redis"
  type        = string
  default     = "CI.Standard.E4.Flex"
}

variable "redis_ocpus" {
  description = "Number of OCPUs for Redis"
  type        = number
  default     = 1
}

variable "redis_memory_gb" {
  description = "Memory in GB for Redis"
  type        = number
  default     = 2
}

variable "redis_password" {
  description = "Redis password"
  type        = string
  sensitive   = true
}

# Database configuration
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
  description = "Oracle connect string (TNS alias)"
  type        = string
}

variable "wallet_base64" {
  description = "Base64-encoded wallet ZIP content"
  type        = string
  sensitive   = true
}

# Secrets configuration
variable "vault_ocid" {
  description = "OCID of the OCI Vault for secrets"
  type        = string
  default     = ""
}

variable "jwt_secret" {
  description = "JWT signing secret for authentication"
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

variable "extra_environment_variables" {
  description = "Additional environment variables for TMI server"
  type        = map(string)
  default     = {}
}

# Load Balancer configuration
variable "lb_min_bandwidth_mbps" {
  description = "Minimum bandwidth for load balancer in Mbps"
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
  description = "Enable HTTP to HTTPS redirect"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}

# TMI-UX configuration
variable "tmi_ux_enabled" {
  description = "Enable TMI-UX frontend container"
  type        = bool
  default     = false
}

variable "tmi_ux_image_url" {
  description = "Container image URL for TMI-UX frontend"
  type        = string
  default     = null
}

variable "tmi_ux_shape" {
  description = "Container instance shape for TMI-UX"
  type        = string
  default     = "CI.Standard.E4.Flex"
}

variable "tmi_ux_ocpus" {
  description = "Number of OCPUs for TMI-UX"
  type        = number
  default     = 1
}

variable "tmi_ux_memory_gb" {
  description = "Memory in GB for TMI-UX"
  type        = number
  default     = 2
}

variable "tmi_ux_nsg_ids" {
  description = "List of NSG OCIDs for TMI-UX"
  type        = list(string)
  default     = []
}

# Hostname routing configuration
variable "api_hostname" {
  description = "Hostname for API traffic (e.g., api.tmi.dev). Required when tmi_ux_enabled is true."
  type        = string
  default     = null
}

variable "ui_hostname" {
  description = "Hostname for UI traffic (e.g., app.tmi.dev or tmi.dev). Required when tmi_ux_enabled is true."
  type        = string
  default     = null
}
