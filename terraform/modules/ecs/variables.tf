variable "project_name" { type = string }
variable "aws_region" { type = string }
variable "vpc_id" { type = string }
variable "subnet_ids" { type = list(string) }
variable "container_port" { type = number }
variable "task_cpu" { type = number }
variable "task_memory" { type = number }
variable "desired_count" { type = number }
variable "ecr_repo_url" { type = string }
variable "image_digest" { type = string }
variable "target_group_arn" { type = string }
variable "alb_sg_id" { type = string }
variable "backend_mode" { type = string }
variable "stock_count" { type = number }
variable "redis_addr" { type = string }
variable "log_group_name" { type = string }
variable "resilience_mode" {
  description = "Resilience mode env var"
  type        = string
  default     = "none"
}

variable "stock_mode" {
  description = "Stock mode env var"
  type        = string
  default     = "decr"
}