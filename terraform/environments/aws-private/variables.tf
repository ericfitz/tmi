# Variables for TMI AWS Private Environment

variable "aws_region" {
  description = "AWS region for deployment"
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix for all resource names"
  type        = string
  default     = "tmi"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "db_name" {
  description = "Name of the PostgreSQL database"
  type        = string
  default     = "tmi"
}

variable "db_username" {
  description = "Master username for the database"
  type        = string
  default     = "tmi"
}

variable "tmi_image_tag" {
  description = "Docker image tag for the TMI server"
  type        = string
  default     = "latest"
}

variable "redis_image_tag" {
  description = "Docker image tag for Redis"
  type        = string
  default     = "latest"
}

variable "extra_env_vars" {
  description = "Additional environment variables for TMI server (merged into ConfigMap)"
  type        = map(string)
  default     = {}
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
