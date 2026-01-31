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
  description = "OCID of the application log"
  value       = oci_logging_log.tmi_app.id
}

output "container_log_id" {
  description = "OCID of the container log (if created)"
  value       = var.container_instance_id != null ? oci_logging_log.container_logs[0].id : null
}

output "archive_bucket_name" {
  description = "Name of the log archive bucket (if created)"
  value       = var.create_archive_bucket ? oci_objectstorage_bucket.log_archive[0].name : null
}

output "service_connector_id" {
  description = "OCID of the service connector for log archival"
  value       = var.create_archive_bucket ? oci_sch_service_connector.log_archive[0].id : null
}

output "notification_topic_id" {
  description = "OCID of the notification topic (if created)"
  value       = var.create_alert_topic ? oci_ons_notification_topic.alerts[0].id : null
}

output "error_rate_alarm_id" {
  description = "OCID of the error rate alarm (if created)"
  value       = var.create_alarms ? oci_monitoring_alarm.error_rate[0].id : null
}

output "container_health_alarm_id" {
  description = "OCID of the container health alarm (if created)"
  value       = var.create_alarms && var.container_instance_id != null ? oci_monitoring_alarm.container_health[0].id : null
}

output "dynamic_group_id" {
  description = "OCID of the logging dynamic group (if created)"
  value       = var.create_dynamic_group ? oci_identity_dynamic_group.logging[0].id : null
}

output "dynamic_group_name" {
  description = "Name of the logging dynamic group (if created)"
  value       = var.create_dynamic_group ? oci_identity_dynamic_group.logging[0].name : null
}

# Standard interface outputs for multi-cloud compatibility
output "log_group" {
  description = "Log group identifier (standard interface)"
  value       = oci_logging_log_group.tmi.id
}

output "log_stream" {
  description = "Log stream identifier (standard interface)"
  value       = oci_logging_log.tmi_app.id
}

# Configuration values for TMI
output "tmi_logging_config" {
  description = "Configuration values for TMI OCI logging"
  value = {
    log_group_id   = oci_logging_log_group.tmi.id
    log_id         = oci_logging_log.tmi_app.id
    compartment_id = var.compartment_id
  }
}
