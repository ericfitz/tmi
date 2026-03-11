# Outputs for AWS Certificates Module

output "certificate_arn" {
  description = "ARN of the ACM certificate"
  value       = aws_acm_certificate.tmi.arn
}

output "certificate_domain_name" {
  description = "Primary domain name of the certificate"
  value       = aws_acm_certificate.tmi.domain_name
}

output "certificate_status" {
  description = "Status of the certificate (PENDING_VALIDATION, ISSUED, etc.)"
  value       = aws_acm_certificate.tmi.status
}

output "dns_validation_records" {
  description = "DNS validation records for manual validation (when zone_id is not provided)"
  value = [
    for dvo in aws_acm_certificate.tmi.domain_validation_options : {
      domain_name = dvo.domain_name
      name        = dvo.resource_record_name
      type        = dvo.resource_record_type
      value       = dvo.resource_record_value
    }
  ]
}

# Configuration summary (matching OCI module pattern)
output "certificate_config" {
  description = "Summary of certificate configuration"
  value = {
    domain_name  = aws_acm_certificate.tmi.domain_name
    status       = aws_acm_certificate.tmi.status
    auto_renewal = true
  }
}
