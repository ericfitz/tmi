# GCP Secrets Module for TMI
# Creates Secret Manager secrets for DB credentials, Redis password, and JWT secret
# Includes random password generation and IAM bindings for GKE Workload Identity

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0.0"
    }
  }
}

# Random password generation
resource "random_password" "db_password" {
  count            = var.db_password == null ? 1 : 0
  length           = 24
  special          = true
  override_special = "!#$%&*()-_=+[]{}|:,.?"
}

resource "random_password" "redis_password" {
  count   = var.redis_password == null ? 1 : 0
  length  = 24
  special = false
}

resource "random_password" "jwt_secret" {
  count   = var.jwt_secret == null ? 1 : 0
  length  = 64
  special = false
}

locals {
  db_password    = var.db_password != null ? var.db_password : random_password.db_password[0].result
  redis_password = var.redis_password != null ? var.redis_password : random_password.redis_password[0].result
  jwt_secret     = var.jwt_secret != null ? var.jwt_secret : random_password.jwt_secret[0].result
}

# Database Password Secret
resource "google_secret_manager_secret" "db_password" {
  project   = var.project_id
  secret_id = "${var.name_prefix}-db-password"

  replication {
    auto {}
  }

  labels = var.labels
}

resource "google_secret_manager_secret_version" "db_password" {
  secret      = google_secret_manager_secret.db_password.id
  secret_data = local.db_password
}

# Redis Password Secret
resource "google_secret_manager_secret" "redis_password" {
  project   = var.project_id
  secret_id = "${var.name_prefix}-redis-password"

  replication {
    auto {}
  }

  labels = var.labels
}

resource "google_secret_manager_secret_version" "redis_password" {
  secret      = google_secret_manager_secret.redis_password.id
  secret_data = local.redis_password
}

# JWT Secret
resource "google_secret_manager_secret" "jwt_secret" {
  project   = var.project_id
  secret_id = "${var.name_prefix}-jwt-secret"

  replication {
    auto {}
  }

  labels = var.labels
}

resource "google_secret_manager_secret_version" "jwt_secret" {
  secret      = google_secret_manager_secret.jwt_secret.id
  secret_data = local.jwt_secret
}

# IAM binding: Allow GKE Workload Identity service account to access secrets
resource "google_secret_manager_secret_iam_member" "db_password_accessor" {
  count     = var.workload_identity_sa_email != null ? 1 : 0
  project   = var.project_id
  secret_id = google_secret_manager_secret.db_password.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.workload_identity_sa_email}"
}

resource "google_secret_manager_secret_iam_member" "redis_password_accessor" {
  count     = var.workload_identity_sa_email != null ? 1 : 0
  project   = var.project_id
  secret_id = google_secret_manager_secret.redis_password.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.workload_identity_sa_email}"
}

resource "google_secret_manager_secret_iam_member" "jwt_secret_accessor" {
  count     = var.workload_identity_sa_email != null ? 1 : 0
  project   = var.project_id
  secret_id = google_secret_manager_secret.jwt_secret.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.workload_identity_sa_email}"
}
