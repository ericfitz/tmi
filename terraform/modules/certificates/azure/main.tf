# Azure Certificates Module for TMI
# Creates Key Vault certificate or configures Let's Encrypt integration
# TLS termination handled at the NGINX Ingress Controller level

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 3.80.0"
    }
  }
}

# Self-signed certificate in Key Vault (for initial setup / development)
# For production, deployers should replace this with a proper certificate
# or configure cert-manager with Let's Encrypt in the cluster
resource "azurerm_key_vault_certificate" "tmi" {
  count        = var.create_self_signed_cert ? 1 : 0
  name         = "${var.name_prefix}-tls"
  key_vault_id = var.key_vault_id

  certificate_policy {
    issuer_parameters {
      name = "Self"
    }

    key_properties {
      exportable = true
      key_size   = 2048
      key_type   = "RSA"
      reuse_key  = true
    }

    lifetime_action {
      action {
        action_type = "AutoRenew"
      }

      trigger {
        days_before_expiry = 30
      }
    }

    secret_properties {
      content_type = "application/x-pkcs12"
    }

    x509_certificate_properties {
      extended_key_usage = ["1.3.6.1.5.5.7.3.1"] # Server Authentication
      key_usage = [
        "cRLSign",
        "dataEncipherment",
        "digitalSignature",
        "keyAgreement",
        "keyCertSign",
        "keyEncipherment",
      ]

      subject            = "CN=${var.domain_name}"
      validity_in_months = 12

      subject_alternative_names {
        dns_names = var.subject_alternative_names
      }
    }
  }

  tags = var.tags
}
