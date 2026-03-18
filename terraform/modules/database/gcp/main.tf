# GCP Database Module for TMI
# Creates Cloud SQL PostgreSQL instance with configurable networking and deletion protection

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0.0"
    }
  }
}

# Cloud SQL PostgreSQL Instance
resource "google_sql_database_instance" "tmi" {
  name                = "${var.name_prefix}-postgres"
  project             = var.project_id
  region              = var.region
  database_version    = var.database_version
  deletion_protection = var.deletion_protection

  settings {
    tier              = var.tier
    availability_type = var.availability_type
    disk_size         = var.disk_size_gb
    disk_type         = "PD_SSD"
    disk_autoresize   = var.disk_autoresize

    ip_configuration {
      ipv4_enabled    = var.enable_public_ip
      private_network = var.private_network_id

      dynamic "authorized_networks" {
        for_each = var.authorized_networks
        content {
          name  = authorized_networks.value.name
          value = authorized_networks.value.cidr
        }
      }
    }

    backup_configuration {
      enabled                        = var.enable_backups
      start_time                     = "03:00"
      point_in_time_recovery_enabled = var.enable_backups
      transaction_log_retention_days = var.enable_backups ? 7 : null

      backup_retention_settings {
        retained_backups = var.backup_retained_count
        retention_unit   = "COUNT"
      }
    }

    maintenance_window {
      day          = 7 # Sunday
      hour         = 4 # 4 AM UTC
      update_track = "stable"
    }

    database_flags {
      name  = "max_connections"
      value = "100"
    }

    user_labels = var.labels
  }
}

# TMI Database
resource "google_sql_database" "tmi" {
  name     = var.database_name
  project  = var.project_id
  instance = google_sql_database_instance.tmi.name
}

# Database User
resource "google_sql_user" "tmi" {
  name     = var.db_username
  project  = var.project_id
  instance = google_sql_database_instance.tmi.name
  password = var.db_password
}
