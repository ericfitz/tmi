# OCI Logging Module for TMI
# Creates Log Group, OKE control plane log, container log collection via Unified Monitoring Agent,
# and Service Connectors for log aggregation

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

# OKE Control Plane Log - collects API server, controller manager, scheduler logs
# Note: This only captures control plane logs (kube-apiserver, cloud-controller-manager,
# kube-scheduler, kube-controller-manager). Pod stdout/stderr logs require the
# Unified Monitoring Agent configuration below.
resource "oci_logging_log" "oke_control_plane" {
  count        = var.create_oke_log ? 1 : 0
  display_name = "${var.name_prefix}-oke-control-plane"
  log_group_id = oci_logging_log_group.tmi.id
  log_type     = "SERVICE"

  configuration {
    compartment_id = var.compartment_id

    source {
      category    = "all-service-logs"
      resource    = var.oke_cluster_id
      service     = "oke-k8s-cp-prod"
      source_type = "OCISERVICE"
    }
  }

  is_enabled         = true
  retention_duration = var.retention_days

  freeform_tags = var.tags
}

# Custom Log for container stdout/stderr - receives logs from the Unified Monitoring Agent
resource "oci_logging_log" "container_logs" {
  count        = var.create_container_log ? 1 : 0
  display_name = "${var.name_prefix}-container-logs"
  log_group_id = oci_logging_log_group.tmi.id
  log_type     = "CUSTOM"

  is_enabled         = true
  retention_duration = var.retention_days

  freeform_tags = var.tags
}

# Dynamic Group matching OKE worker node instances for log shipping authorization
resource "oci_identity_dynamic_group" "oke_workers" {
  count          = var.create_container_log ? 1 : 0
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-oke-workers"
  description    = "OKE worker node instances for ${var.name_prefix} container log shipping"
  matching_rule  = "ANY {instance.compartment.id = '${var.compartment_id}'}"

  freeform_tags = var.tags
}

# IAM Policy granting worker nodes permission to ship logs
# Note: Policies must be created at the tenancy level
resource "oci_identity_policy" "logging_policy" {
  count          = var.create_container_log ? 1 : 0
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-logging-policy"
  description    = "Allow OKE worker nodes to ship container logs to OCI Logging"
  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.oke_workers[0].name} to use log-content in compartment id ${var.compartment_id}",
    "Allow dynamic-group ${oci_identity_dynamic_group.oke_workers[0].name} to manage log-groups in compartment id ${var.compartment_id}",
  ]

  freeform_tags = var.tags

  depends_on = [oci_identity_dynamic_group.oke_workers]
}

# Unified Monitoring Agent Configuration for container log collection
# Deploys a Fluentd-based agent on OKE worker nodes that tails /var/log/containers/*.log
# and ships to the custom log above.
# Requirements:
# - Application logs MUST be JSON format (use slog.NewJSONHandler, not TextHandler)
# - CRI-O container runtime writes logs in CRI format: <timestamp> <stream> <logtag> <message>
# - The REGEXP parser extracts the CRI fields; the inner message (JSON) is parsed automatically
resource "oci_logging_unified_agent_configuration" "container_logs" {
  count          = var.create_container_log ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-container-agent"
  is_enabled     = true
  description    = "Collects container stdout/stderr logs from OKE worker nodes"

  service_configuration {
    configuration_type = "LOGGING"

    destination {
      log_object_id = oci_logging_log.container_logs[0].id
    }

    sources {
      name        = "container-logs"
      source_type = "LOG_TAIL"
      paths       = ["/var/log/containers/*.log"]

      parser {
        parser_type    = "REGEXP"
        expression     = "^(?<time>[^ ]+) (?<stream>stdout|stderr) (?<logtag>[^ ]*) (?<message>.*)$"
        time_format    = "%Y-%m-%dT%H:%M:%S.%N%:z"
        field_time_key = "time"
      }
    }
  }

  group_association {
    group_list = [oci_identity_dynamic_group.oke_workers[0].id]
  }

  freeform_tags = var.tags

  depends_on = [
    oci_logging_log.container_logs,
    oci_identity_dynamic_group.oke_workers,
    oci_identity_policy.logging_policy,
  ]
}

# Object Storage Bucket for Log Archive
resource "oci_objectstorage_bucket" "log_archive" {
  count          = var.create_archive_bucket ? 1 : 0
  compartment_id = var.compartment_id
  namespace      = var.object_storage_namespace
  name           = "${var.name_prefix}-log-archive"
  access_type    = "NoPublicAccess"
  storage_tier   = "Archive"

  # Versioning enabled for log integrity
  versioning = "Enabled"

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
  count          = var.create_archive_bucket && var.create_oke_log ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-log-archive-connector"

  source {
    kind = "logging"
    log_sources {
      compartment_id = var.compartment_id
      log_group_id   = oci_logging_log_group.tmi.id
      log_id         = oci_logging_log.oke_control_plane[0].id
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

  depends_on = [oci_logging_log.oke_control_plane]
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
  count          = var.create_alarms && var.create_oke_log ? 1 : 0
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
