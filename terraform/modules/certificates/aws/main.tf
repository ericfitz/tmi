# AWS Certificates Module for TMI
# Creates ACM certificates with optional Route 53 DNS validation
#
# Unlike OCI which requires a custom Lambda function for Let's Encrypt automation,
# AWS ACM provides free, auto-renewing certificates natively. This makes the module
# much simpler: just request a certificate, validate it, and ACM handles renewal.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

# ACM Certificate
resource "aws_acm_certificate" "tmi" {
  domain_name               = var.domain_name
  subject_alternative_names = var.subject_alternative_names
  validation_method         = "DNS"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-certificate"
  })

  lifecycle {
    create_before_destroy = true
  }
}

# Route 53 DNS validation records (only when zone_id is provided)
resource "aws_route53_record" "validation" {
  for_each = var.zone_id != "" ? {
    for dvo in aws_acm_certificate.tmi.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  } : {}

  allow_overwrite = true
  name            = each.value.name
  records         = [each.value.record]
  ttl             = 60
  type            = each.value.type
  zone_id         = var.zone_id
}

# ACM certificate validation (waits for validation to complete)
resource "aws_acm_certificate_validation" "tmi" {
  count = var.zone_id != "" && var.wait_for_validation ? 1 : 0

  certificate_arn         = aws_acm_certificate.tmi.arn
  validation_record_fqdns = [for record in aws_route53_record.validation : record.fqdn]
}
