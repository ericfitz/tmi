# GCP Network Module for TMI
# Creates VPC with subnets, Cloud NAT for outbound, and firewall rules
# Supports both public and private cluster networking

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0.0"
    }
  }
}

# VPC Network
resource "google_compute_network" "tmi" {
  name                    = "${var.name_prefix}-vpc"
  project                 = var.project_id
  auto_create_subnetworks = false
  routing_mode            = "REGIONAL"
}

# Primary Subnet (GKE nodes, pods, services)
resource "google_compute_subnetwork" "primary" {
  name                     = "${var.name_prefix}-primary"
  project                  = var.project_id
  region                   = var.region
  network                  = google_compute_network.tmi.id
  ip_cidr_range            = var.primary_subnet_cidr
  private_ip_google_access = true

  secondary_ip_range {
    range_name    = "${var.name_prefix}-pods"
    ip_cidr_range = var.pods_cidr
  }

  secondary_ip_range {
    range_name    = "${var.name_prefix}-services"
    ip_cidr_range = var.services_cidr
  }
}

# Cloud Router (required for Cloud NAT)
resource "google_compute_router" "tmi" {
  name    = "${var.name_prefix}-router"
  project = var.project_id
  region  = var.region
  network = google_compute_network.tmi.id
}

# Cloud NAT for outbound internet access
resource "google_compute_router_nat" "tmi" {
  name                               = "${var.name_prefix}-nat"
  project                            = var.project_id
  region                             = var.region
  router                             = google_compute_router.tmi.name
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}

# Firewall: Allow internal traffic within VPC
resource "google_compute_firewall" "allow_internal" {
  name    = "${var.name_prefix}-allow-internal"
  project = var.project_id
  network = google_compute_network.tmi.id

  allow {
    protocol = "tcp"
  }

  allow {
    protocol = "udp"
  }

  allow {
    protocol = "icmp"
  }

  source_ranges = [
    var.primary_subnet_cidr,
    var.pods_cidr,
    var.services_cidr,
  ]

  description = "Allow internal traffic within TMI VPC"
}

# Firewall: Allow health checks from Google Cloud health check ranges
resource "google_compute_firewall" "allow_health_checks" {
  name    = "${var.name_prefix}-allow-health-checks"
  project = var.project_id
  network = google_compute_network.tmi.id

  allow {
    protocol = "tcp"
    ports    = ["8080", "3000"]
  }

  # Google Cloud health check IP ranges
  source_ranges = [
    "35.191.0.0/16",
    "130.211.0.0/22",
  ]

  target_tags = ["${var.name_prefix}-gke-node"]

  description = "Allow Google Cloud health checks to TMI and TMI-UX pods"
}

# Firewall: Allow HTTP(S) ingress to load balancer (public templates)
resource "google_compute_firewall" "allow_http_https" {
  count   = var.enable_public_ingress ? 1 : 0
  name    = "${var.name_prefix}-allow-http-https"
  project = var.project_id
  network = google_compute_network.tmi.id

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }

  source_ranges = ["0.0.0.0/0"]

  description = "Allow HTTP/HTTPS ingress from internet (public template)"
}

# Firewall: Allow HTTP(S) ingress from specific CIDRs (private templates)
resource "google_compute_firewall" "allow_private_ingress" {
  count   = var.enable_public_ingress ? 0 : (length(var.private_ingress_cidrs) > 0 ? 1 : 0)
  name    = "${var.name_prefix}-allow-private-ingress"
  project = var.project_id
  network = google_compute_network.tmi.id

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }

  source_ranges = var.private_ingress_cidrs

  description = "Allow HTTP/HTTPS ingress from authorized CIDRs (private template)"
}

# Private Service Access for Cloud SQL private IP
resource "google_compute_global_address" "private_services" {
  count         = var.enable_private_services_access ? 1 : 0
  name          = "${var.name_prefix}-private-services"
  project       = var.project_id
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.tmi.id
}

resource "google_service_networking_connection" "private_services" {
  count                   = var.enable_private_services_access ? 1 : 0
  network                 = google_compute_network.tmi.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_services[0].name]
}
