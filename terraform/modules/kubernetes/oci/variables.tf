# Variables for OCI Kubernetes (OKE) Module

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
  description = "Availability domain for node pool (defaults to first AD)"
  type        = string
  default     = null
}

# OKE Cluster configuration
variable "kubernetes_version" {
  description = "Kubernetes version for the OKE cluster"
  type        = string
  default     = "v1.34.2"
}

# Managed Node Pool configuration
variable "node_count" {
  description = "Number of nodes in the managed node pool"
  type        = number
  default     = 1
}

variable "node_shape" {
  description = "Compute shape for managed nodes"
  type        = string
  default     = "VM.Standard.A1.Flex"
}

variable "node_ocpus" {
  description = "Number of OCPUs per node (for flex shapes)"
  type        = number
  default     = 1
}

variable "node_memory_gbs" {
  description = "Memory in GBs per node (for flex shapes)"
  type        = number
  default     = 6
}

variable "node_image_id" {
  description = "OCID of the OKE node image"
  type        = string
}

variable "k8s_services_cidr" {
  description = "CIDR block for Kubernetes services"
  type        = string
  default     = "10.96.0.0/16"
}

variable "k8s_pods_cidr" {
  description = "CIDR block for Kubernetes pods"
  type        = string
  default     = "10.244.0.0/16"
}

# Network configuration
variable "vcn_id" {
  description = "OCID of the VCN"
  type        = string
}

variable "oke_api_subnet_id" {
  description = "OCID of the OKE API endpoint subnet"
  type        = string
}

variable "oke_worker_subnet_id" {
  description = "OCID of the subnet for OKE worker nodes"
  type        = string
}

variable "oke_pod_subnet_id" {
  description = "OCID of the subnet for VCN-native pod IPs"
  type        = string
}

variable "public_subnet_ids" {
  description = "List of public subnet OCIDs for service load balancers"
  type        = list(string)
}

variable "oke_api_nsg_ids" {
  description = "List of NSG OCIDs for OKE API endpoint"
  type        = list(string)
  default     = []
}

variable "oke_pod_nsg_ids" {
  description = "List of NSG OCIDs for OKE worker nodes"
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

variable "tmi_replicas" {
  description = "Number of TMI API pod replicas (TMI is stateful; use 1 to avoid database corruption)"
  type        = number
  default     = 1
}

variable "tmi_cpu_request" {
  description = "CPU request for TMI API pods"
  type        = string
  default     = "500m"
}

variable "tmi_memory_request" {
  description = "Memory request for TMI API pods"
  type        = string
  default     = "1Gi"
}

variable "tmi_cpu_limit" {
  description = "CPU limit for TMI API pods"
  type        = string
  default     = "2"
}

variable "tmi_memory_limit" {
  description = "Memory limit for TMI API pods"
  type        = string
  default     = "4Gi"
}

# Redis configuration
variable "redis_image_url" {
  description = "Container image URL for Redis"
  type        = string
}

variable "redis_password" {
  description = "Redis password"
  type        = string
  sensitive   = true
}

variable "redis_cpu_request" {
  description = "CPU request for Redis pod"
  type        = string
  default     = "250m"
}

variable "redis_memory_request" {
  description = "Memory request for Redis pod"
  type        = string
  default     = "512Mi"
}

variable "redis_cpu_limit" {
  description = "CPU limit for Redis pod"
  type        = string
  default     = "1"
}

variable "redis_memory_limit" {
  description = "Memory limit for Redis pod"
  type        = string
  default     = "2Gi"
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

variable "tags" {
  description = "Freeform tags to apply to all OCI resources"
  type        = map(string)
  default     = {}
}

# TMI-UX configuration
variable "tmi_ux_enabled" {
  description = "Enable TMI-UX frontend deployment"
  type        = bool
  default     = false
}

variable "tmi_ux_image_url" {
  description = "Container image URL for TMI-UX frontend"
  type        = string
  default     = null
}

variable "tmi_ux_replicas" {
  description = "Number of TMI-UX pod replicas"
  type        = number
  default     = 1
}
