# Outputs for AWS Logging Module

output "log_group_name" {
  description = "Name of the CloudWatch Log Group"
  value       = aws_cloudwatch_log_group.tmi.name
}

output "log_group_arn" {
  description = "ARN of the CloudWatch Log Group"
  value       = aws_cloudwatch_log_group.tmi.arn
}

output "fluent_bit_role_arn" {
  description = "ARN of the Fluent Bit IAM role"
  value       = aws_iam_role.fluent_bit.arn
}

# Standard interface outputs for multi-cloud compatibility
output "log_group" {
  description = "Log group identifier (standard interface)"
  value       = aws_cloudwatch_log_group.tmi.name
}

output "log_stream" {
  description = "Log stream prefix (standard interface)"
  value       = "pod/"
}
