# GCP Logging Module for TMI
# Cloud Logging is built-in for GKE Autopilot with minimal configuration needed
# This module configures log retention, log-based metrics, and optional log sinks

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0.0"
    }
  }
}

# Log bucket for TMI-specific log retention (overrides project default)
resource "google_logging_project_bucket_config" "tmi" {
  count          = var.custom_retention_days != null ? 1 : 0
  project        = var.project_id
  location       = var.region
  bucket_id      = "${var.name_prefix}-logs"
  description    = "TMI application log bucket with custom retention"
  retention_days = var.custom_retention_days
}

# Log-based metric: TMI error count
resource "google_logging_metric" "error_count" {
  count   = var.create_metrics ? 1 : 0
  project = var.project_id
  name    = "${var.name_prefix}-error-count"
  filter  = "resource.type=\"k8s_container\" resource.labels.namespace_name=\"tmi\" severity>=ERROR"

  metric_descriptor {
    metric_kind  = "DELTA"
    value_type   = "INT64"
    unit         = "1"
    display_name = "TMI Error Count"
  }
}

# Log-based metric: TMI request count
resource "google_logging_metric" "request_count" {
  count   = var.create_metrics ? 1 : 0
  project = var.project_id
  name    = "${var.name_prefix}-request-count"
  filter  = "resource.type=\"k8s_container\" resource.labels.namespace_name=\"tmi\" resource.labels.container_name=\"tmi-api\""

  metric_descriptor {
    metric_kind  = "DELTA"
    value_type   = "INT64"
    unit         = "1"
    display_name = "TMI Request Count"
  }
}

# Log sink to Cloud Storage for long-term archival (optional)
resource "google_logging_project_sink" "archive" {
  count                  = var.create_archive_sink ? 1 : 0
  project                = var.project_id
  name                   = "${var.name_prefix}-log-archive"
  destination            = "storage.googleapis.com/${google_storage_bucket.log_archive[0].name}"
  filter                 = "resource.type=\"k8s_container\" resource.labels.namespace_name=\"tmi\""
  unique_writer_identity = true
}

# Cloud Storage bucket for log archival
resource "google_storage_bucket" "log_archive" {
  count         = var.create_archive_sink ? 1 : 0
  project       = var.project_id
  name          = "${var.project_id}-${var.name_prefix}-log-archive"
  location      = var.region
  storage_class = "NEARLINE"
  force_destroy = true

  lifecycle_rule {
    action {
      type = "Delete"
    }
    condition {
      age = var.archive_retention_days
    }
  }

  labels = var.labels
}

# Grant the log sink writer permission to write to the bucket
resource "google_storage_bucket_iam_member" "log_sink_writer" {
  count  = var.create_archive_sink ? 1 : 0
  bucket = google_storage_bucket.log_archive[0].name
  role   = "roles/storage.objectCreator"
  member = google_logging_project_sink.archive[0].writer_identity
}

# Alert policy for high error rate (optional)
resource "google_monitoring_alert_policy" "error_rate" {
  count        = var.create_alerts && var.create_metrics ? 1 : 0
  project      = var.project_id
  display_name = "${var.name_prefix} High Error Rate"
  combiner     = "OR"

  conditions {
    display_name = "TMI error rate above threshold"
    condition_threshold {
      filter          = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.error_count[0].name}\" resource.type=\"k8s_container\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = var.error_threshold

      aggregations {
        alignment_period   = "60s"
        per_series_aligner = "ALIGN_RATE"
      }
    }
  }

  notification_channels = var.notification_channel_ids
}
