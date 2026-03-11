# AWS DNS Module for TMI
# Creates Route 53 records pointing domain names to the ALB.
# Uses CNAME records to map custom domains to the ALB hostname.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

# Route 53 CNAME record for the TMI API server (tmiserver.efitz.net)
resource "aws_route53_record" "server" {
  zone_id = var.zone_id
  name    = var.server_domain
  type    = "CNAME"
  ttl     = 300
  records = [var.alb_dns_name]
}

# Route 53 CNAME record for the TMI-UX frontend (tmi.efitz.net)
resource "aws_route53_record" "ux" {
  count = var.ux_domain != null ? 1 : 0

  zone_id = var.zone_id
  name    = var.ux_domain
  type    = "CNAME"
  ttl     = 300
  records = [var.alb_dns_name]
}
