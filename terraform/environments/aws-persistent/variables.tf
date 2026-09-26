# Variables for TMI AWS Persistent Environment

variable "admin_users" {
  description = "IAM users that get the explicit Deny on releasing the NAT egress EIP (every admin principal in the account)"
  type        = list(string)
  default     = ["llm-platform-dev"]
}

variable "security_alert_email" {
  description = "Email address subscribed to the tmi-security-alerts SNS topic"
  type        = string
  default     = "security@tmi.dev"
}
