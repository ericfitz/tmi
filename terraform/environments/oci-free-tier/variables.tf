# Variables for TMI OCI Free Development Deployment

# OCI Configuration
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

# Naming
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

# Network Configuration
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
  description = "CIDR block for the database subnet (kept for VCN completeness)"
  type        = string
  default     = "10.0.3.0/24"
}

# Secrets Configuration
variable "postgres_password" {
  description = "PostgreSQL password for the tmi user. If not provided, a random password is generated."
  type        = string
  sensitive   = true
  default     = null
}

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

variable "oauth_client_secret" {
  description = "OAuth client secret for the TMI provider. If not provided, a random secret is generated."
  type        = string
  sensitive   = true
  default     = null
}

# VM SSH Access
variable "ssh_authorized_keys" {
  description = "SSH public key(s) for direct VM access (replaces OCI Bastion)"
  type        = string
  default     = null
}

# Container Images
variable "tmi_image_url" {
  description = "Container image URL for TMI server (x86-64, non-Oracle build from OCIR)"
  type        = string
}

variable "redis_image_url" {
  description = "Container image URL for Redis (x86-64, from OCIR)"
  type        = string
  default     = ""
}

# OCIR Authentication (for cloud-init to pull private images)
variable "ocir_username" {
  description = "OCIR username for pulling private images (format: <namespace>/<email>)"
  type        = string
  default     = ""
}

variable "ocir_auth_token" {
  description = "OCI Auth Token for OCIR docker login (create at OCI Console > Profile > Auth Tokens)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "postgres_image_url" {
  description = "Container image URL for PostgreSQL (x86-64, from OCIR)"
  type        = string
}

# TMI-UX Frontend Configuration
variable "tmi_ux_image_url" {
  description = "Container image URL for TMI-UX frontend"
  type        = string
  default     = ""
}

variable "tmi_ux_api_url" {
  description = "Public URL of the TMI API for the frontend (e.g. http://<VM_IP>:8080). Update after first apply."
  type        = string
  default     = ""
}

# Tags
variable "tags" {
  description = "Additional freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
