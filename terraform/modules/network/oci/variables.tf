# Variables for OCI Network Module

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "dns_label" {
  description = "DNS label for VCN"
  type        = string
  default     = "tmi"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]{0,14}$", var.dns_label))
    error_message = "DNS label must start with a letter, contain only lowercase letters and numbers, and be 1-15 characters."
  }
}

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
  description = "List of CIDRs authorized to access the Kubernetes API endpoint (VCN-internal only)"
  type        = list(string)
  default     = ["10.0.0.0/16"]
}

variable "oke_public_endpoint" {
  description = "Whether the OKE API endpoint should be publicly accessible"
  type        = bool
  default     = false
}

variable "lb_public" {
  description = "Whether load balancers should be publicly accessible (true for public template, false for private)"
  type        = bool
  default     = false
}

variable "tags" {
  description = "Freeform tags to apply to all resources"
  type        = map(string)
  default     = {}
}
