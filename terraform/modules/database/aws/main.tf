# AWS Database Module for TMI
# Creates RDS PostgreSQL instance with encryption, backups, and performance insights

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
  instance_class = var.instance_class

  # Storage configuration
  allocated_storage     = var.allocated_storage
  max_allocated_storage = var.max_allocated_storage
  storage_type          = "gp3"
  storage_encrypted     = true

  # Database configuration
  db_name  = var.db_name
  username = var.db_username
  password = var.db_password

  # Network configuration
  db_subnet_group_name   = var.db_subnet_group_name
  vpc_security_group_ids = var.vpc_security_group_ids

  # High availability
  multi_az = var.multi_az

  # Backup configuration
  backup_retention_period   = var.backup_retention_period
  backup_window             = var.backup_window
  maintenance_window        = var.maintenance_window
  final_snapshot_identifier = "${var.name_prefix}-postgres-final-snapshot"

  # Protection
  deletion_protection = var.deletion_protection
  skip_final_snapshot = var.skip_final_snapshot

  # Monitoring
  performance_insights_enabled = var.performance_insights_enabled

  # Apply changes
  apply_immediately = var.apply_immediately

  # Protect production database from accidental destruction
  # Note: Terraform doesn't allow variables in lifecycle blocks
  lifecycle {
    prevent_destroy = true
  }

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-postgres"
  })
}
