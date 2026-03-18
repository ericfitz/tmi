# Variables for GCP Network Module

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "tmi"
}

variable "primary_subnet_cidr" {
  description = "CIDR block for the primary subnet (GKE nodes)"
  type        = string
  default     = "10.0.0.0/24"
}

variable "pods_cidr" {
  description = "CIDR block for GKE pod IPs (secondary range)"
  type        = string
  default     = "10.1.0.0/16"
}

variable "services_cidr" {
  description = "CIDR block for GKE service IPs (secondary range)"
  type        = string
  default     = "10.2.0.0/20"
}

variable "enable_public_ingress" {
  description = "Allow HTTP/HTTPS ingress from the internet (public template)"
  type        = bool
  default     = true
}

variable "private_ingress_cidrs" {
  description = "List of CIDRs allowed for ingress (private template, when enable_public_ingress is false)"
  type        = list(string)
  default     = []
}

variable "enable_private_services_access" {
  description = "Enable Private Service Access for Cloud SQL private IP connectivity"
  type        = bool
  default     = false
}

variable "labels" {
  description = "Labels to apply to all resources"
  type        = map(string)
  default     = {}
}
