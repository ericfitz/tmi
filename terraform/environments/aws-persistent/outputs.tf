# Outputs for TMI AWS Persistent Environment

output "nat_eip_allocation_id" {
  description = "Allocation ID of the permanent NAT egress EIP"
  value       = aws_eip.nat_egress.allocation_id
}

output "nat_public_ip" {
  description = "Permanent NAT egress public IP (monitored externally; never release)"
  value       = aws_eip.nat_egress.public_ip
}

output "log_bucket_name" {
  description = "S3 bucket for CloudTrail, ALB access logs, and VPC flow logs"
  value       = aws_s3_bucket.logs.id
}

output "security_alerts_topic_arn" {
  description = "SNS topic ARN for security alerts (EventBridge rules, CloudWatch alarms)"
  value       = aws_sns_topic.security_alerts.arn
}
