# ---------------------------------------------------------------
# Root Outputs — what you need after `terraform apply`
# ---------------------------------------------------------------

output "alb_dns_name" {
  description = "ALB public URL — point Locust here"
  value       = module.alb.alb_dns_name
}

output "ecr_repository_url" {
  description = "ECR repo — push your Docker image here"
  value       = module.ecr.repository_url
}

output "redis_endpoint" {
  description = "ElastiCache Redis endpoint"
  value       = module.network.redis_endpoint
}

output "vpc_id" {
  description = "VPC ID"
  value       = module.network.vpc_id
}