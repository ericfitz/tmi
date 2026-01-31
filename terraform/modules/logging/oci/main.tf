# OCI Logging Module for TMI
# Creates Log Group, Custom Logs, and Service Connectors for log aggregation

terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
  }
}

# Log Group for TMI
resource "oci_logging_log_group" "tmi" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-logs"
  description    = "Log group for TMI application logs"

  freeform_tags = var.tags
}

# Custom Log for TMI Application
resource "oci_logging_log" "tmi_app" {
  display_name = "${var.name_prefix}-app"
  log_group_id = oci_logging_log_group.tmi.id
  log_type     = "CUSTOM"

  configuration {
    compartment_id = var.compartment_id

    source {
      category    = "custom"
      resource    = var.name_prefix
      service     = "custom"
      source_type = "OCISERVICE"
    }
  }

  is_enabled         = true
  retention_duration = var.retention_days

  freeform_tags = var.tags
}

# Custom Log for Container Instance stdout/stderr
resource "oci_logging_log" "container_logs" {
  count        = var.container_instance_id != null ? 1 : 0
  display_name = "${var.name_prefix}-container"
  log_group_id = oci_logging_log_group.tmi.id
  log_type     = "SERVICE"

  configuration {
    compartment_id = var.compartment_id

    source {
      category    = "containerinstance"
      resource    = var.container_instance_id
      service     = "containerinstance"
      source_type = "OCISERVICE"
    }
  }

  is_enabled         = true
  retention_duration = var.retention_days

  freeform_tags = var.tags
}

# Object Storage Bucket for Log Archive
resource "oci_objectstorage_bucket" "log_archive" {
  count          = var.create_archive_bucket ? 1 : 0
  compartment_id = var.compartment_id
  namespace      = var.object_storage_namespace
  name           = "${var.name_prefix}-log-archive"
  access_type    = "NoPublicAccess"
  storage_tier   = "Archive"

  # Note: versioning disabled to allow retention rules
  versioning = "Disabled"

  # Lifecycle rules for retention
  dynamic "retention_rules" {
    for_each = var.archive_retention_days > 0 ? [1] : []
    content {
      display_name = "log-retention"
      duration {
        time_amount = var.archive_retention_days
        time_unit   = "DAYS"
      }
    }
  }

  freeform_tags = var.tags
}

# Service Connector for Log Archival to Object Storage
resource "oci_sch_service_connector" "log_archive" {
  count          = var.create_archive_bucket ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-log-archive-connector"

  source {
    kind = "logging"
    log_sources {
      compartment_id = var.compartment_id
      log_group_id   = oci_logging_log_group.tmi.id
      log_id         = oci_logging_log.tmi_app.id
    }
  }

  target {
    kind                       = "objectStorage"
    bucket                     = oci_objectstorage_bucket.log_archive[0].name
    namespace                  = var.object_storage_namespace
    object_name_prefix         = "${var.name_prefix}/"
    batch_rollover_size_in_mbs = 10
    batch_rollover_time_in_ms  = 60000 # 1 minute
  }

  description = "Archive TMI logs to Object Storage"
  state       = "ACTIVE"

  freeform_tags = var.tags

  depends_on = [oci_logging_log.tmi_app]
}

# Notification Topic for Alerts (optional)
resource "oci_ons_notification_topic" "alerts" {
  count          = var.create_alert_topic ? 1 : 0
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-alerts"
  description    = "Notification topic for TMI alerts"

  freeform_tags = var.tags
}

# Email Subscription for Alerts (optional)
resource "oci_ons_subscription" "email" {
  count          = var.create_alert_topic && var.alert_email != null ? 1 : 0
  compartment_id = var.compartment_id
  topic_id       = oci_ons_notification_topic.alerts[0].id
  protocol       = "EMAIL"
  endpoint       = var.alert_email

  freeform_tags = var.tags
}

# Alarm for High Error Rate
resource "oci_monitoring_alarm" "error_rate" {
  count          = var.create_alarms ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-high-error-rate"

  is_enabled                   = true
  metric_compartment_id        = var.compartment_id
  namespace                    = "oci_logging"
  query                        = "Count[1m]{logGroup = \"${oci_logging_log_group.tmi.id}\", logContent.level = \"ERROR\"}.sum() > ${var.error_threshold}"
  severity                     = "CRITICAL"
  pending_duration             = "PT5M"
  body                         = "TMI error rate exceeded threshold. Please investigate."
  message_format               = "ONS_OPTIMIZED"
  repeat_notification_duration = "PT1H"

  destinations = var.create_alert_topic ? [oci_ons_notification_topic.alerts[0].id] : []

  freeform_tags = var.tags
}

# Alarm for Container Instance Health
resource "oci_monitoring_alarm" "container_health" {
  count          = var.create_alarms && var.container_instance_id != null ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-container-health"

  is_enabled            = true
  metric_compartment_id = var.compartment_id
  namespace             = "oci_container_instances"
  query                 = "HealthStatus[1m]{resourceId = \"${var.container_instance_id}\"}.mean() < 1"
  severity              = "CRITICAL"
  pending_duration      = "PT5M"
  body                  = "TMI container instance is unhealthy. Please investigate."
  message_format        = "ONS_OPTIMIZED"

  destinations = var.create_alert_topic ? [oci_ons_notification_topic.alerts[0].id] : []

  freeform_tags = var.tags
}

# Dynamic Group for Log Ingestion (Container Instances)
resource "oci_identity_dynamic_group" "logging" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-logging"
  description    = "Dynamic group for TMI logging"

  matching_rule = "ALL {resource.type = 'computecontainerinstance', resource.compartment.id = '${var.compartment_id}'}"

  freeform_tags = var.tags
}

# Policy to allow Container Instances to write logs
resource "oci_identity_policy" "logging" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-logging-policy"
  description    = "Allow TMI containers to write logs"

  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.logging[0].name} to use log-content in compartment id ${var.compartment_id}"
  ]

  freeform_tags = var.tags
}
