# Variables for OCI Compute Module (E5.Flex VM-based deployment)

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
variable "public_subnet_id" {
  description = "OCID of the public subnet for the VM (VM gets a direct public IP)"
  type        = string
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

# VM configuration
variable "vm_ocpus" {
  description = "Number of OCPUs for the VM"
  type        = number
  default     = 2
}

variable "vm_memory_gb" {
  description = "Memory in GB for the VM"
  type        = number
  default     = 8
}

variable "boot_volume_size_gb" {
  description = "Boot volume size in GB"
  type        = number
  default     = 50
}

variable "ssh_authorized_keys" {
  description = "SSH public key(s) for VM access"
  type        = string
  default     = null
}

# TMI image configuration
variable "tmi_image_url" {
  description = "Container image URL for TMI server (x86-64, non-Oracle build)"
  type        = string
}

# Redis Docker image
variable "redis_docker_image" {
  description = "Docker image for Redis (x86-64 compatible). Defaults to Chainguard Redis."
  type        = string
  default     = "cgr.dev/chainguard/redis:latest"
}

# TMI-UX Frontend image
variable "tmi_ux_image_url" {
  description = "Container image URL for TMI-UX frontend (x86-64)"
  type        = string
  default     = ""
}

variable "tmi_ux_api_url" {
  description = "Public URL of the TMI API for the frontend to use (e.g. http://<VM_IP>:8080)"
  type        = string
  default     = ""
}

# Kept for interface compatibility
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

# PostgreSQL configuration
variable "postgres_image_url" {
  description = "Container image URL for PostgreSQL (x86-64, from OCIR)"
  type        = string
}

variable "postgres_password" {
  description = "PostgreSQL password for the tmi database user"
  type        = string
  sensitive   = true
}

# Secrets / Auth
variable "jwt_secret" {
  description = "JWT signing secret for authentication"
  type        = string
  sensitive   = true
}

variable "oauth_client_secret" {
  description = "OAuth client secret for the TMI provider (separate from JWT secret)"
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

variable "extra_environment_variables" {
  description = "Additional environment variables for TMI server (kept for compatibility)"
  type        = map(string)
  default     = {}
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
