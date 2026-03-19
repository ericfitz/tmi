# GCP Kubernetes (GKE Autopilot) Module for TMI
# Creates a GKE Autopilot cluster with Workload Identity

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
  }
}

# GCP Service Account for Workload Identity
resource "google_service_account" "tmi_workload" {
  project      = var.project_id
  account_id   = "${var.name_prefix}-workload"
  display_name = "TMI Workload Identity SA"
  description  = "Service account for TMI GKE workloads via Workload Identity"
}

# IAM binding: Allow K8s SA to impersonate the GCP SA
resource "google_service_account_iam_member" "workload_identity_binding" {
  service_account_id = google_service_account.tmi_workload.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[tmi/tmi-api]"

  depends_on = [google_container_cluster.tmi]
}

# GKE Autopilot Cluster
resource "google_container_cluster" "tmi" {
  name     = "${var.name_prefix}-gke"
  project  = var.project_id
  location = var.region

  # Enable Autopilot mode
  enable_autopilot = true

  network    = var.network_id
  subnetwork = var.subnetwork_id

  ip_allocation_policy {
    cluster_secondary_range_name  = var.pods_range_name
    services_secondary_range_name = var.services_range_name
  }

  # Private cluster configuration
  dynamic "private_cluster_config" {
    for_each = var.enable_private_cluster ? [1] : []
    content {
      enable_private_nodes    = true
      enable_private_endpoint = var.enable_private_endpoint
      master_ipv4_cidr_block  = var.master_ipv4_cidr_block
    }
  }

  # Master authorized networks (for private clusters)
  dynamic "master_authorized_networks_config" {
    for_each = length(var.master_authorized_cidrs) > 0 ? [1] : []
    content {
      dynamic "cidr_blocks" {
        for_each = var.master_authorized_cidrs
        content {
          cidr_block   = cidr_blocks.value.cidr
          display_name = cidr_blocks.value.name
        }
      }
    }
  }

  # Release channel
  release_channel {
    channel = "REGULAR"
  }

  deletion_protection = var.deletion_protection
}

# Get cluster credentials for kubernetes provider
data "google_client_config" "current" {}
