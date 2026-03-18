# Outputs for GCP Logging Module

output "log_bucket_id" {
  description = "ID of the custom log bucket (if created)"
  value       = var.custom_retention_days != null ? google_logging_project_bucket_config.tmi[0].id : null
}

output "error_count_metric_name" {
  description = "Name of the error count log-based metric (if created)"
  value       = var.create_metrics ? google_logging_metric.error_count[0].name : null
}

output "request_count_metric_name" {
  description = "Name of the request count log-based metric (if created)"
  value       = var.create_metrics ? google_logging_metric.request_count[0].name : null
}

output "archive_bucket_name" {
  description = "Name of the log archive Cloud Storage bucket (if created)"
  value       = var.create_archive_sink ? google_storage_bucket.log_archive[0].name : null
}

output "archive_sink_name" {
  description = "Name of the log archive sink (if created)"
  value       = var.create_archive_sink ? google_logging_project_sink.archive[0].name : null
}

output "alert_policy_id" {
  description = "ID of the error rate alert policy (if created)"
  value       = var.create_alerts && var.create_metrics ? google_monitoring_alert_policy.error_rate[0].id : null
}

# Standard interface outputs for multi-cloud compatibility
output "log_group" {
  description = "Log group identifier (standard interface) - GCP uses project-level logging"
  value       = var.project_id
}

output "log_stream" {
  description = "Log stream identifier (standard interface) - GCP uses automatic container logging"
  value       = "k8s_container/tmi"
}
