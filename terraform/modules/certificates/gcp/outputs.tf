# Outputs for GCP Certificates Module

output "certificate_id" {
  description = "ID of the Google-managed SSL certificate"
  value       = google_compute_managed_ssl_certificate.tmi.id
}

output "certificate_name" {
  description = "Name of the Google-managed SSL certificate"
  value       = google_compute_managed_ssl_certificate.tmi.name
}

output "certificate_self_link" {
  description = "Self-link of the Google-managed SSL certificate"
  value       = google_compute_managed_ssl_certificate.tmi.self_link
}

output "static_ip_address" {
  description = "Static external IP address (if created)"
  value       = var.create_static_ip ? google_compute_global_address.tmi[0].address : null
}

output "static_ip_name" {
  description = "Name of the static IP address resource (if created)"
  value       = var.create_static_ip ? google_compute_global_address.tmi[0].name : null
}

# Configuration summary
output "certificate_config" {
  description = "Summary of certificate configuration"
  value = {
    domain_names = var.domain_names
    certificate  = google_compute_managed_ssl_certificate.tmi.name
    static_ip    = var.create_static_ip ? google_compute_global_address.tmi[0].address : "Not created"
  }
}
