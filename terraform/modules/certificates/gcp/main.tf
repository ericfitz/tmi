# GCP Certificates Module for TMI
# Creates Google-managed SSL certificates for use with GKE Ingress/Load Balancer

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0.0"
    }
  }
}

# Google-managed SSL Certificate
# Requires the domain to resolve to the load balancer IP for validation
resource "google_compute_managed_ssl_certificate" "tmi" {
  project = var.project_id
  name    = "${var.name_prefix}-ssl-cert"

  managed {
    domains = var.domain_names
  }
}

# Static external IP address for the load balancer (optional)
# Useful for setting up DNS records before certificate provisioning
resource "google_compute_global_address" "tmi" {
  count   = var.create_static_ip ? 1 : 0
  project = var.project_id
  name    = "${var.name_prefix}-ip"
}
