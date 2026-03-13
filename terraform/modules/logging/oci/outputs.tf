# Outputs for OCI Logging Module

output "log_group_id" {
  description = "OCID of the Log Group"
  value       = oci_logging_log_group.tmi.id
}

output "log_group_name" {
  description = "Name of the Log Group"
  value       = oci_logging_log_group.tmi.display_name
}

output "app_log_id" {
  description = "OCID of the OKE control plane log (if created)"
  value       = var.create_oke_log ? oci_logging_log.oke_control_plane[0].id : null
}

output "container_log_id" {
  description = "OCID of the container stdout/stderr custom log (if created)"
  value       = var.create_container_log ? oci_logging_log.container_logs[0].id : null
}

output "unified_agent_config_id" {
  description = "OCID of the Unified Monitoring Agent configuration (if created)"
  value       = var.create_container_log ? oci_logging_unified_agent_configuration.container_logs[0].id : null
}

output "archive_bucket_name" {
  description = "Name of the log archive bucket (if created)"
  value       = var.create_archive_bucket ? oci_objectstorage_bucket.log_archive[0].name : null
}

output "control_plane_log_id" {
  description = "OCID of the OKE control plane log (if created)"
  value       = var.create_oke_log ? oci_logging_log.oke_control_plane[0].id : null
}

output "service_connector_id" {
  description = "OCID of the service connector for log archival"
  value       = var.create_archive_bucket && var.create_oke_log ? oci_sch_service_connector.log_archive[0].id : null
}

output "notification_topic_id" {
  description = "OCID of the notification topic (if created)"
  value       = var.create_alert_topic ? oci_ons_notification_topic.alerts[0].id : null
}

output "error_rate_alarm_id" {
  description = "OCID of the error rate alarm (if created)"
  value       = var.create_alarms && var.create_oke_log ? oci_monitoring_alarm.error_rate[0].id : null
}

# Standard interface outputs for multi-cloud compatibility
output "log_group" {
  description = "Log group identifier (standard interface)"
  value       = oci_logging_log_group.tmi.id
}

output "log_stream" {
  description = "Log stream identifier (standard interface)"
  value       = var.create_container_log ? oci_logging_log.container_logs[0].id : var.create_oke_log ? oci_logging_log.oke_control_plane[0].id : null
}
