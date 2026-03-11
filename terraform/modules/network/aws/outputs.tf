# Outputs for AWS Network Module

output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.tmi.id
}

output "public_subnet_ids" {
  description = "List of public subnet IDs"
  value       = aws_subnet.public[*].id
}

output "private_subnet_ids" {
  description = "List of private subnet IDs"
  value       = aws_subnet.private[*].id
}

output "database_subnet_ids" {
  description = "List of database subnet IDs"
  value       = aws_subnet.database[*].id
}

output "alb_security_group_id" {
  description = "ID of the ALB security group"
  value       = aws_security_group.alb.id
}

output "eks_node_security_group_id" {
  description = "ID of the EKS node security group"
  value       = aws_security_group.eks_node.id
}

output "redis_security_group_id" {
  description = "ID of the Redis security group"
  value       = aws_security_group.redis.id
}

output "rds_security_group_id" {
  description = "ID of the RDS security group"
  value       = aws_security_group.rds.id
}

output "tmi_ux_security_group_id" {
  description = "ID of the TMI-UX security group"
  value       = aws_security_group.tmi_ux.id
}

output "internet_gateway_id" {
  description = "ID of the internet gateway"
  value       = aws_internet_gateway.tmi.id
}

output "nat_gateway_ids" {
  description = "List of NAT gateway IDs"
  value       = aws_nat_gateway.tmi[*].id
}

output "db_subnet_group_name" {
  description = "Name of the RDS subnet group"
  value       = aws_db_subnet_group.tmi.name
}

output "availability_zones" {
  description = "List of availability zones used"
  value       = slice(data.aws_availability_zones.available.names, 0, 3)
}

# Standard interface outputs for multi-cloud compatibility
output "tmi_security_group_id" {
  description = "TMI server security group ID (standard interface - maps to EKS node SG)"
  value       = aws_security_group.eks_node.id
}

output "lb_security_group_id" {
  description = "Load balancer security group ID (standard interface)"
  value       = aws_security_group.alb.id
}
