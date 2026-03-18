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
