# Variables for TMI OCI Logging Test

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

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi-logtest"
}

variable "dns_label" {
  description = "DNS label for VCN"
  type        = string
  default     = "tmilog"
}

# Network CIDRs - use different ranges from production to avoid conflicts
variable "vcn_cidr" {
  type    = string
  default = "10.1.0.0/16"
}

variable "public_subnet_cidr" {
  type    = string
  default = "10.1.1.0/24"
}

variable "private_subnet_cidr" {
  type    = string
  default = "10.1.2.0/24"
}

variable "database_subnet_cidr" {
  type    = string
  default = "10.1.3.0/24"
}

variable "oke_api_subnet_cidr" {
  type    = string
  default = "10.1.4.0/28"
}

variable "oke_pod_subnet_cidr" {
  type    = string
  default = "10.1.5.0/24"
}

# OKE Configuration
variable "kubernetes_version" {
  type    = string
  default = "v1.34.2"
}

# Managed Node Pool Configuration
variable "node_shape" {
  description = "Compute shape for OKE managed nodes"
  type        = string
  default     = "VM.Standard.A1.Flex"
}

variable "node_ocpus" {
  description = "Number of OCPUs per node"
  type        = number
  default     = 1
}

variable "node_memory_gbs" {
  description = "Memory in GBs per node"
  type        = number
  default     = 6
}

variable "node_image_id" {
  description = "OCID of the OKE node image"
  type        = string
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
