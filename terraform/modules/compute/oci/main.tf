# OCI Compute Module for TMI
# Creates a VM.Standard.A1.Flex (ARM Ampere) instance running TMI Server + Redis via Docker
# plus an OCI Load Balancer routing to the VM on port 8080

terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
  }
}

# Data source for availability domains
data "oci_identity_availability_domains" "ads" {
  compartment_id = var.compartment_id
}

# Find the latest Oracle Linux 9 platform image compatible with ARM64 (A1.Flex)
data "oci_core_images" "ol9_arm64" {
  compartment_id           = var.compartment_id
  operating_system         = "Oracle Linux"
  operating_system_version = "9"
  shape                    = "VM.Standard.A1.Flex"
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
  state                    = "AVAILABLE"
}

locals {
  availability_domain = var.availability_domain != null ? var.availability_domain : data.oci_identity_availability_domains.ads.availability_domains[0].name
}

# TMI Application Server VM (ARM64, runs TMI + Redis via Docker)
resource "oci_core_instance" "tmi" {
  compartment_id      = var.compartment_id
  availability_domain = local.availability_domain
  display_name        = "${var.name_prefix}-server"

  shape = "VM.Standard.A1.Flex"
  shape_config {
    ocpus         = var.vm_ocpus
    memory_in_gbs = var.vm_memory_gb
  }

  source_details {
    source_type             = "image"
    source_id               = data.oci_core_images.ol9_arm64.images[0].id
    boot_volume_size_in_gbs = var.boot_volume_size_gb
  }

  create_vnic_details {
    subnet_id        = var.private_subnet_id
    nsg_ids          = distinct(concat(var.tmi_nsg_ids, var.redis_nsg_ids))
    assign_public_ip = false
    display_name     = "${var.name_prefix}-server-vnic"
  }

  metadata = {
    user_data = base64encode(templatefile("${path.module}/templates/cloud-init.yaml.tpl", {
      tmi_image_url          = var.tmi_image_url
      redis_image_url        = var.redis_docker_image
      wallet_par_url         = var.wallet_par_url
      db_username            = var.db_username
      db_password_encoded    = urlencode(var.db_password)
      oracle_connect_string  = var.oracle_connect_string
      redis_password         = var.redis_password
      redis_password_encoded = urlencode(var.redis_password)
      jwt_secret             = var.jwt_secret
      vault_ocid             = var.vault_ocid
      log_level       = var.log_level
      oci_log_id      = var.oci_log_id != null ? var.oci_log_id : ""
      cloud_log_level = var.cloud_log_level != null ? var.cloud_log_level : var.log_level
    }))
    ssh_authorized_keys = var.ssh_authorized_keys != null ? var.ssh_authorized_keys : ""
  }

  freeform_tags = var.tags

  timeouts {
    create = "30m"
  }
}

# Load Balancer (flexible shape, 10 Mbps free tier)
resource "oci_load_balancer_load_balancer" "tmi" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-lb"
  shape          = "flexible"

  shape_details {
    minimum_bandwidth_in_mbps = var.lb_min_bandwidth_mbps
    maximum_bandwidth_in_mbps = var.lb_max_bandwidth_mbps
  }

  subnet_ids                 = var.public_subnet_ids
  network_security_group_ids = var.lb_nsg_ids
  is_private                 = false

  freeform_tags = var.tags
}

# Backend Set for TMI API
resource "oci_load_balancer_backend_set" "tmi" {
  load_balancer_id = oci_load_balancer_load_balancer.tmi.id
  name             = "${var.name_prefix}-api-backend-set"
  policy           = "ROUND_ROBIN"

  health_checker {
    protocol          = "HTTP"
    port              = 8080
    url_path          = "/"
    return_code       = 200
    interval_ms       = 10000
    timeout_in_millis = 3000
    retries           = 3
  }

  session_persistence_configuration {
    cookie_name      = "X-Oracle-BMC-LBS-Route"
    disable_fallback = false
  }
}

# Backend pointing to the ARM VM's private IP on port 8080
resource "oci_load_balancer_backend" "tmi" {
  load_balancer_id = oci_load_balancer_load_balancer.tmi.id
  backendset_name  = oci_load_balancer_backend_set.tmi.name
  ip_address       = oci_core_instance.tmi.private_ip
  port             = 8080
  weight           = 1
}

# SSL Certificate (optional)
resource "oci_load_balancer_certificate" "tmi" {
  count            = var.ssl_certificate_pem != null ? 1 : 0
  load_balancer_id = oci_load_balancer_load_balancer.tmi.id
  certificate_name = "${var.name_prefix}-cert"

  public_certificate = var.ssl_certificate_pem
  private_key        = var.ssl_private_key_pem
  ca_certificate     = var.ssl_ca_certificate_pem
}

# HTTPS Listener (only when SSL certificate is provided)
resource "oci_load_balancer_listener" "https" {
  count                    = var.ssl_certificate_pem != null ? 1 : 0
  load_balancer_id         = oci_load_balancer_load_balancer.tmi.id
  name                     = "https-listener"
  default_backend_set_name = oci_load_balancer_backend_set.tmi.name
  port                     = 443
  protocol                 = "HTTP"

  ssl_configuration {
    certificate_name        = oci_load_balancer_certificate.tmi[0].certificate_name
    verify_peer_certificate = false
    protocols               = ["TLSv1.2", "TLSv1.3"]
    cipher_suite_name       = "oci-modern-ssl-cipher-suite-v1"
  }

  connection_configuration {
    idle_timeout_in_seconds = 300
  }
}

# HTTP Listener (default — used when no SSL certificate)
resource "oci_load_balancer_listener" "http" {
  count                    = var.ssl_certificate_pem == null ? 1 : 0
  load_balancer_id         = oci_load_balancer_load_balancer.tmi.id
  name                     = "http-listener"
  default_backend_set_name = oci_load_balancer_backend_set.tmi.name
  port                     = 80
  protocol                 = "HTTP"

  connection_configuration {
    idle_timeout_in_seconds = 300
  }
}

# HTTP to HTTPS Redirect Rule Set (when SSL is configured)
resource "oci_load_balancer_rule_set" "redirect_http" {
  count            = var.ssl_certificate_pem != null && var.enable_http_redirect ? 1 : 0
  load_balancer_id = oci_load_balancer_load_balancer.tmi.id
  name             = "redirect-http-to-https"

  items {
    action = "REDIRECT"

    conditions {
      attribute_name  = "PATH"
      attribute_value = "/"
      operator        = "PREFIX_MATCH"
    }

    redirect_uri {
      protocol = "HTTPS"
      port     = 443
    }
    response_code = 301
  }
}

# HTTP Redirect Listener (when SSL is configured)
resource "oci_load_balancer_listener" "http_redirect" {
  count                    = var.ssl_certificate_pem != null && var.enable_http_redirect ? 1 : 0
  load_balancer_id         = oci_load_balancer_load_balancer.tmi.id
  name                     = "http-redirect-listener"
  default_backend_set_name = oci_load_balancer_backend_set.tmi.name
  port                     = 80
  protocol                 = "HTTP"

  rule_set_names = [oci_load_balancer_rule_set.redirect_http[0].name]
}
