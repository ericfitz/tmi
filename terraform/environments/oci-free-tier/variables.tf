# Variables for TMI OCI Free Tier Deployment

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
  description = "CIDR block for the database subnet"
  type        = string
  default     = "10.0.3.0/24"
}

# Database Configuration
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

variable "prevent_database_destroy" {
  description = "Prevent accidental destruction of database"
  type        = bool
  default     = true
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

# Container Images
variable "tmi_image_url" {
  description = "Container image URL for TMI server"
  type        = string
}

variable "redis_image_url" {
  description = "Container image URL for Redis"
  type        = string
}

# SSL Configuration
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

# Alerting
variable "alert_email" {
  description = "Email address for alert notifications"
  type        = string
  default     = null
}

# Certificate Automation (Let's Encrypt)
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

# Tags
variable "tags" {
  description = "Additional freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
