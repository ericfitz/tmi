# Outputs for AWS Logging Module

output "app_log_group_name" {
  description = "Name of the application CloudWatch Log Group"
  value       = aws_cloudwatch_log_group.app.name
}

output "app_log_group_arn" {
  description = "ARN of the application CloudWatch Log Group"
  value       = aws_cloudwatch_log_group.app.arn
}

output "container_log_group_name" {
  description = "Name of the container CloudWatch Log Group"
  value       = aws_cloudwatch_log_group.containers.name
}

output "container_log_group_arn" {
  description = "ARN of the container CloudWatch Log Group"
  value       = aws_cloudwatch_log_group.containers.arn
}

output "archive_bucket_name" {
  description = "Name of the log archive S3 bucket (if created)"
  value       = var.create_archive_bucket ? aws_s3_bucket.log_archive[0].id : null
}

output "archive_bucket_arn" {
  description = "ARN of the log archive S3 bucket (if created)"
  value       = var.create_archive_bucket ? aws_s3_bucket.log_archive[0].arn : null
}

output "sns_topic_arn" {
  description = "ARN of the SNS alert topic (if created)"
  value       = var.create_alert_topic ? aws_sns_topic.alerts[0].arn : null
}

output "error_rate_alarm_arn" {
  description = "ARN of the high error rate CloudWatch alarm (if created)"
  value       = var.create_alarms ? aws_cloudwatch_metric_alarm.high_error_rate[0].arn : null
}

output "logging_policy_arn" {
  description = "ARN of the IAM policy for writing logs"
  value       = aws_iam_policy.logging.arn
}

# Standard interface outputs for multi-cloud compatibility
output "log_group" {
  description = "Log group identifier (standard interface)"
  value       = aws_cloudwatch_log_group.app.name
}

output "log_stream" {
  description = "Log stream identifier (standard interface)"
  value       = aws_cloudwatch_log_group.containers.name
}

# Configuration values for TMI
output "tmi_logging_config" {
  description = "Configuration values for TMI AWS logging"
  value = {
    app_log_group       = aws_cloudwatch_log_group.app.name
    container_log_group = aws_cloudwatch_log_group.containers.name
    region              = data.aws_region.current.id
    logging_policy_arn  = aws_iam_policy.logging.arn
  }
}
