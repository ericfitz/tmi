# AWS Certificates Module for TMI
# Creates ACM certificate with DNS validation

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
  domain_name       = var.domain_name
  validation_method = "DNS"

  subject_alternative_names = var.subject_alternative_names

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-certificate"
  })

  lifecycle {
    create_before_destroy = true
  }
}

# ============================================================================
# DNS validation
# ============================================================================

resource "aws_route53_record" "validation" {
  for_each = {
    for dvo in aws_acm_certificate.tmi.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  }

  zone_id         = var.hosted_zone_id
  name            = each.value.name
  type            = each.value.type
  ttl             = 60
  records         = [each.value.record]
  allow_overwrite = true
}

resource "aws_acm_certificate_validation" "tmi" {
  certificate_arn         = aws_acm_certificate.tmi.arn
  validation_record_fqdns = [for r in aws_route53_record.validation : r.fqdn]
}
