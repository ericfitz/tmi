# Outputs for AWS Certificates Module

output "certificate_arn" {
  description = "ARN of the ACM certificate"
  value       = aws_acm_certificate.tmi.arn
}

output "certificate_domain_name" {
  description = "Domain name of the certificate"
  value       = aws_acm_certificate.tmi.domain_name
}

output "certificate_status" {
  description = "Status of the certificate"
  value       = aws_acm_certificate.tmi.status
}

output "domain_validation_options" {
  description = "DNS validation records that must be created to validate the certificate"
  value       = aws_acm_certificate.tmi.domain_validation_options
}

# Standard interface output
output "certificate_id" {
  description = "Certificate identifier (standard interface)"
  value       = aws_acm_certificate.tmi.arn
}
