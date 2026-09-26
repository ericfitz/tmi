# AWS Database Module for TMI
# Creates RDS PostgreSQL instance with configurable instance class and deletion protection

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

# Connection logging, exported to CloudWatch (only when enable_log_export).
# Switching the instance to this parameter group needs one reboot to take effect.
resource "aws_db_parameter_group" "tmi" {
  count  = var.enable_log_export ? 1 : 0
  name   = "${var.name_prefix}-postgres${split(".", var.engine_version)[0]}"
  family = "postgres${split(".", var.engine_version)[0]}"

  parameter {
    name  = "log_connections"
    value = "1"
  }

  parameter {
    name  = "log_disconnections"
    value = "1"
  }

  tags = var.tags

  lifecycle {
    create_before_destroy = true
  }
}

# Pre-created so the exported log gets a retention period.
resource "aws_cloudwatch_log_group" "postgresql" {
  count             = var.enable_log_export ? 1 : 0
  name              = "/aws/rds/instance/${var.name_prefix}-postgres/postgresql"
  retention_in_days = var.log_retention_days
  tags              = var.tags
}

resource "aws_db_instance" "tmi" {
  identifier = "${var.name_prefix}-postgres"

  # Engine configuration
  engine         = "postgres"
  engine_version = var.engine_version

  # Instance configuration
  instance_class        = var.instance_class
  allocated_storage     = var.allocated_storage
  max_allocated_storage = var.max_allocated_storage
  storage_type          = "gp3"

  # Database configuration
  db_name  = var.db_name
  username = var.db_username
  password = var.db_password
  port     = 5432

  # Network configuration
  db_subnet_group_name   = var.db_subnet_group_name
  vpc_security_group_ids = var.vpc_security_group_ids

  # Availability configuration
  multi_az = false

  # Backup configuration
  backup_retention_period   = var.backup_retention_period
  skip_final_snapshot       = var.skip_final_snapshot
  final_snapshot_identifier = var.skip_final_snapshot ? null : "${var.name_prefix}-final-snapshot"

  # Protection
  deletion_protection = var.deletion_protection

  # Encryption
  storage_encrypted = true

  # Monitoring
  monitoring_interval = 0

  # Maintenance
  auto_minor_version_upgrade = true
  apply_immediately          = var.apply_immediately

  # Logging
  parameter_group_name            = one(aws_db_parameter_group.tmi[*].name)
  enabled_cloudwatch_logs_exports = var.enable_log_export ? ["postgresql"] : []

  # Public accessibility (always false - database stays in private subnets)
  publicly_accessible = false

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-postgres"
  })

  depends_on = [aws_cloudwatch_log_group.postgresql]
}
