# OCI Compute Module for TMI
# Creates Container Instances for TMI Server and Redis, plus Load Balancer

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

locals {
  availability_domain = var.availability_domain != null ? var.availability_domain : data.oci_identity_availability_domains.ads.availability_domains[0].name
}

# Redis Container Instance
resource "oci_container_instances_container_instance" "redis" {
  compartment_id      = var.compartment_id
  availability_domain = local.availability_domain
  display_name        = "${var.name_prefix}-redis"

  shape = var.redis_shape
  shape_config {
    ocpus         = var.redis_ocpus
    memory_in_gbs = var.redis_memory_gb
  }

  vnics {
    subnet_id             = var.private_subnet_id
    nsg_ids               = var.redis_nsg_ids
    is_public_ip_assigned = false
    display_name          = "${var.name_prefix}-redis-vnic"
  }

  containers {
    display_name = "redis"
    image_url    = var.redis_image_url

    environment_variables = {
      REDIS_PASSWORD = var.redis_password
      REDIS_PORT     = "6379"
    }

    resource_config {
      vcpus_limit         = var.redis_ocpus
      memory_limit_in_gbs = var.redis_memory_gb
    }

    health_checks {
      health_check_type   = "TCP"
      port                = 6379
      interval_in_seconds = 30
      timeout_in_seconds  = 10
      failure_threshold   = 3
    }
  }

  container_restart_policy = "ALWAYS"

  graceful_shutdown_timeout_in_seconds = 30

  freeform_tags = var.tags
}

# TMI Server Container Instance
resource "oci_container_instances_container_instance" "tmi" {
  compartment_id      = var.compartment_id
  availability_domain = local.availability_domain
  display_name        = "${var.name_prefix}-tmi"

  shape = var.tmi_shape
  shape_config {
    ocpus         = var.tmi_ocpus
    memory_in_gbs = var.tmi_memory_gb
  }

  vnics {
    subnet_id             = var.private_subnet_id
    nsg_ids               = var.tmi_nsg_ids
    is_public_ip_assigned = false
    display_name          = "${var.name_prefix}-tmi-vnic"
  }

  containers {
    display_name = "tmi-server"
    image_url    = var.tmi_image_url

    environment_variables = merge(
      {
        # Database configuration - TMI_DATABASE_URL is required
        # Format for Oracle ADB with wallet: oracle://user:password@tns_alias
        # Password is URL-encoded to handle special characters
        TMI_DATABASE_URL           = "oracle://${var.db_username}:${urlencode(var.db_password)}@${var.oracle_connect_string}"
        TMI_ORACLE_WALLET_LOCATION = "/wallet"

        # Redis configuration
        REDIS_URL = "redis://:${var.redis_password}@${oci_container_instances_container_instance.redis.vnics[0].private_ip}:6379"

        # Authentication configuration
        TMI_JWT_SECRET = var.jwt_secret
        TMI_BUILD_MODE = "dev"

        # OAuth provider configuration - TMI internal provider for dev/test
        OAUTH_PROVIDERS_TMI_ENABLED       = "true"
        OAUTH_PROVIDERS_TMI_CLIENT_ID     = "tmi-oci-deployment"
        OAUTH_PROVIDERS_TMI_CLIENT_SECRET = var.jwt_secret

        # Secrets provider configuration
        TMI_SECRETS_PROVIDER       = "oci"
        TMI_SECRETS_OCI_VAULT_OCID = var.vault_ocid

        # Logging configuration
        TMI_LOG_LEVEL = var.log_level
        TMI_LOG_DIR   = "/tmp"

        # Server configuration
        TMI_SERVER_ADDRESS = "0.0.0.0:8080"
      },
      var.extra_environment_variables
    )

    # Mount wallet volume
    volume_mounts {
      mount_path   = "/wallet"
      volume_name  = "wallet-volume"
      is_read_only = true
    }

    resource_config {
      vcpus_limit         = var.tmi_ocpus
      memory_limit_in_gbs = var.tmi_memory_gb
    }

    health_checks {
      health_check_type        = "HTTP"
      port                     = 8080
      path                     = "/"
      interval_in_seconds      = 30
      timeout_in_seconds       = 10
      failure_threshold        = 3
      initial_delay_in_seconds = 60
    }
  }

  # Wallet volume from base64 content
  volumes {
    name        = "wallet-volume"
    volume_type = "CONFIGFILE"

    configs {
      file_name = "wallet.zip"
      data      = var.wallet_base64
    }
  }

  container_restart_policy = "ALWAYS"

  graceful_shutdown_timeout_in_seconds = 60

  depends_on = [oci_container_instances_container_instance.redis]

  freeform_tags = var.tags
}

# Load Balancer
resource "oci_load_balancer_load_balancer" "tmi" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-lb"
  shape          = "flexible"

  shape_details {
    minimum_bandwidth_in_mbps = var.lb_min_bandwidth_mbps
    maximum_bandwidth_in_mbps = var.lb_max_bandwidth_mbps
  }

  subnet_ids = var.public_subnet_ids

  network_security_group_ids = var.lb_nsg_ids

  is_private = false

  freeform_tags = var.tags
}

# Backend Set
resource "oci_load_balancer_backend_set" "tmi" {
  load_balancer_id = oci_load_balancer_load_balancer.tmi.id
  name             = "${var.name_prefix}-backend-set"
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

# Backend
resource "oci_load_balancer_backend" "tmi" {
  load_balancer_id = oci_load_balancer_load_balancer.tmi.id
  backendset_name  = oci_load_balancer_backend_set.tmi.name
  ip_address       = oci_container_instances_container_instance.tmi.vnics[0].private_ip
  port             = 8080
  weight           = 1
}

# SSL Certificate (self-signed for testing, or use Let's Encrypt)
resource "oci_load_balancer_certificate" "tmi" {
  count            = var.ssl_certificate_pem != null ? 1 : 0
  load_balancer_id = oci_load_balancer_load_balancer.tmi.id
  certificate_name = "${var.name_prefix}-cert"

  public_certificate = var.ssl_certificate_pem
  private_key        = var.ssl_private_key_pem
  ca_certificate     = var.ssl_ca_certificate_pem
}

# HTTPS Listener (with SSL)
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

# HTTP Listener (without SSL - for testing or HTTP redirect)
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
