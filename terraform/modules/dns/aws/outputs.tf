# Outputs for AWS DNS Module

output "server_fqdn" {
  description = "Fully qualified domain name for the TMI API server"
  value       = length(aws_route53_record.server) > 0 ? aws_route53_record.server[0].fqdn : null
}

output "ux_fqdn" {
  description = "Fully qualified domain name for the TMI-UX frontend"
  value       = length(aws_route53_record.ux) > 0 ? aws_route53_record.ux[0].fqdn : null
}

output "server_url" {
  description = "HTTPS URL for the TMI API server"
  value       = length(aws_route53_record.server) > 0 ? "https://${aws_route53_record.server[0].fqdn}" : null
}

output "ux_url" {
  description = "HTTPS URL for the TMI-UX frontend"
  value       = length(aws_route53_record.ux) > 0 ? "https://${aws_route53_record.ux[0].fqdn}" : null
}
