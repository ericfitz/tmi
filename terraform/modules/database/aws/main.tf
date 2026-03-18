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

  # Public accessibility (always false - database stays in private subnets)
  publicly_accessible = false

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-postgres"
  })
}
