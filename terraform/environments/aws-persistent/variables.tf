# Variables for TMI AWS Persistent Environment

variable "admin_users" {
  description = "IAM users that get the explicit Denies on releasing the NAT egress EIP and on deleting or overwriting the settings-key escrow secret (every admin principal in the account)"
  type        = list(string)
  default     = ["llm-platform-dev"]
}

variable "security_alert_email" {
  description = "Email address subscribed to the tmi-security-alerts SNS topic"
  type        = string
  default     = "security@tmi.dev"
}

variable "rds_instance_identifier" {
  description = "DBInstanceIdentifier of the aws-public RDS instance to alarm on (T362). A literal default, not a cross-stack read: aws-persistent must not depend on aws-public state (dependency direction is aws-public -> aws-persistent only)."
  type        = string
  default     = "tmi-postgres"
}
