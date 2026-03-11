# Variables for AWS DNS Module

variable "zone_id" {
  description = "Route 53 hosted zone ID"
  type        = string
}

variable "server_domain" {
  description = "Domain name for the TMI API server (e.g., tmiserver.efitz.net)"
  type        = string
}

variable "ux_domain" {
  description = "Domain name for the TMI-UX frontend (e.g., tmi.efitz.net). Set to null to skip."
  type        = string
  default     = null
}

variable "alb_dns_name" {
  description = "DNS hostname of the ALB (from Kubernetes Ingress status)"
  type        = string
  default     = null
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
