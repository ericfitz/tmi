# AWS Network Module for TMI
# Creates VPC with public, private, and database subnets across 3 AZs
# Includes gateways, route tables, security groups, and RDS subnet group

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

# Data source for availability zones
data "aws_availability_zones" "available" {
  state = "available"
}

# ============================================================================
# VPC
# ============================================================================

resource "aws_vpc" "tmi" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-vpc"
  })
}

# ============================================================================
# Internet Gateway
# ============================================================================

resource "aws_internet_gateway" "tmi" {
  vpc_id = aws_vpc.tmi.id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-igw"
  })
}

# ============================================================================
# NAT Gateway (single or multi-AZ)
# ============================================================================

resource "aws_eip" "nat" {
  count  = var.enable_multi_az_nat ? 3 : 1
  domain = "vpc"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-nat-eip-${count.index}"
  })

  depends_on = [aws_internet_gateway.tmi]
}

resource "aws_nat_gateway" "tmi" {
  count         = var.enable_multi_az_nat ? 3 : 1
  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-nat-${count.index}"
  })

  depends_on = [aws_internet_gateway.tmi]
}

# ============================================================================
# Subnets
# ============================================================================

# Public subnets (for ALB)
resource "aws_subnet" "public" {
  count                   = 3
  vpc_id                  = aws_vpc.tmi.id
  cidr_block              = var.public_subnet_cidrs[count.index]
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = true

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-public-${count.index}"
  })
}

# Private subnets (for EKS pods)
resource "aws_subnet" "private" {
  count             = 3
  vpc_id            = aws_vpc.tmi.id
  cidr_block        = var.private_subnet_cidrs[count.index]
  availability_zone = data.aws_availability_zones.available.names[count.index]

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-private-${count.index}"
  })
}

# Database subnets (for RDS)
resource "aws_subnet" "database" {
  count             = 3
  vpc_id            = aws_vpc.tmi.id
  cidr_block        = var.database_subnet_cidrs[count.index]
  availability_zone = data.aws_availability_zones.available.names[count.index]

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-database-${count.index}"
  })
}

# ============================================================================
# Route Tables
# ============================================================================

# Public route table (routes to IGW)
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.tmi.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.tmi.id
  }

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-public-rt"
  })
}

resource "aws_route_table_association" "public" {
  count          = 3
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# Private route tables (routes to NAT)
# When multi-AZ NAT is enabled, each AZ gets its own route table pointing to its own NAT.
# Otherwise, all private subnets share one route table pointing to the single NAT.
resource "aws_route_table" "private" {
  count  = var.enable_multi_az_nat ? 3 : 1
  vpc_id = aws_vpc.tmi.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.tmi[count.index].id
  }

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-private-rt-${count.index}"
  })
}

resource "aws_route_table_association" "private" {
  count          = 3
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[var.enable_multi_az_nat ? count.index : 0].id
}

# Database route table (no internet access)
resource "aws_route_table" "database" {
  vpc_id = aws_vpc.tmi.id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-database-rt"
  })
}

resource "aws_route_table_association" "database" {
  count          = 3
  subnet_id      = aws_subnet.database[count.index].id
  route_table_id = aws_route_table.database.id
}

# ============================================================================
# Security Groups
# ============================================================================

# ALB Security Group
resource "aws_security_group" "alb" {
  name_prefix = "${var.name_prefix}-alb-"
  description = "Security group for Application Load Balancer"
  vpc_id      = aws_vpc.tmi.id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-alb-sg"
  })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "alb_http" {
  security_group_id = aws_security_group.alb.id
  description       = "Allow HTTP from internet (for redirect)"
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-alb-http"
  })
}

resource "aws_vpc_security_group_ingress_rule" "alb_https" {
  security_group_id = aws_security_group.alb.id
  description       = "Allow HTTPS from internet"
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-alb-https"
  })
}

resource "aws_vpc_security_group_egress_rule" "alb_to_eks" {
  security_group_id            = aws_security_group.alb.id
  description                  = "Allow traffic to EKS nodes on port 8080"
  referenced_security_group_id = aws_security_group.eks_node.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-alb-to-eks"
  })
}

resource "aws_vpc_security_group_egress_rule" "alb_to_tmi_ux" {
  security_group_id            = aws_security_group.alb.id
  description                  = "Allow traffic to TMI-UX on port 8080"
  referenced_security_group_id = aws_security_group.tmi_ux.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-alb-to-tmi-ux"
  })
}

# EKS Node Security Group
resource "aws_security_group" "eks_node" {
  name_prefix = "${var.name_prefix}-eks-node-"
  description = "Security group for EKS worker nodes"
  vpc_id      = aws_vpc.tmi.id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-eks-node-sg"
  })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "eks_from_alb" {
  security_group_id            = aws_security_group.eks_node.id
  description                  = "Allow traffic from ALB on port 8080"
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-eks-from-alb"
  })
}

resource "aws_vpc_security_group_egress_rule" "eks_to_rds" {
  security_group_id            = aws_security_group.eks_node.id
  description                  = "Allow traffic to RDS on port 5432"
  referenced_security_group_id = aws_security_group.rds.id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-eks-to-rds"
  })
}

resource "aws_vpc_security_group_egress_rule" "eks_to_redis" {
  security_group_id            = aws_security_group.eks_node.id
  description                  = "Allow traffic to Redis on port 6379"
  referenced_security_group_id = aws_security_group.redis.id
  from_port                    = 6379
  to_port                      = 6379
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-eks-to-redis"
  })
}

resource "aws_vpc_security_group_egress_rule" "eks_to_internet_https" {
  security_group_id = aws_security_group.eks_node.id
  description       = "Allow HTTPS to internet (OAuth, external APIs)"
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-eks-to-internet-https"
  })
}

# Redis Security Group
resource "aws_security_group" "redis" {
  name_prefix = "${var.name_prefix}-redis-"
  description = "Security group for Redis (ElastiCache)"
  vpc_id      = aws_vpc.tmi.id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-redis-sg"
  })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "redis_from_eks" {
  security_group_id            = aws_security_group.redis.id
  description                  = "Allow traffic from EKS nodes on port 6379"
  referenced_security_group_id = aws_security_group.eks_node.id
  from_port                    = 6379
  to_port                      = 6379
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-redis-from-eks"
  })
}

# RDS Security Group
resource "aws_security_group" "rds" {
  name_prefix = "${var.name_prefix}-rds-"
  description = "Security group for RDS (PostgreSQL)"
  vpc_id      = aws_vpc.tmi.id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-rds-sg"
  })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "rds_from_eks" {
  security_group_id            = aws_security_group.rds.id
  description                  = "Allow traffic from EKS nodes on port 5432"
  referenced_security_group_id = aws_security_group.eks_node.id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-rds-from-eks"
  })
}

# TMI-UX Security Group
resource "aws_security_group" "tmi_ux" {
  name_prefix = "${var.name_prefix}-tmi-ux-"
  description = "Security group for TMI-UX (frontend)"
  vpc_id      = aws_vpc.tmi.id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-tmi-ux-sg"
  })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "tmi_ux_from_alb" {
  security_group_id            = aws_security_group.tmi_ux.id
  description                  = "Allow traffic from ALB on port 8080"
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-tmi-ux-from-alb"
  })
}

resource "aws_vpc_security_group_egress_rule" "tmi_ux_to_internet_https" {
  security_group_id = aws_security_group.tmi_ux.id
  description       = "Allow HTTPS to internet"
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-tmi-ux-to-internet-https"
  })
}

# ============================================================================
# RDS Subnet Group
# ============================================================================

resource "aws_db_subnet_group" "tmi" {
  name       = "${var.name_prefix}-db-subnet-group"
  subnet_ids = aws_subnet.database[*].id

  tags = merge(var.tags, {
    Name = "${var.name_prefix}-db-subnet-group"
  })
}
