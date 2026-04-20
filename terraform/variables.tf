# ---------------------------------------------------------------
# Root Variables — tweak these for each experiment run
# ---------------------------------------------------------------

variable "aws_region" {
  description = "AWS region to deploy into"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "flash-sale"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "az_count" {
  description = "Number of availability zones (ALB needs >= 2)"
  type        = number
  default     = 2
}

# ---- ECS ---------------------------------------------------------

variable "ecs_task_cpu" {
  description = "CPU units for each Fargate task (256 = 0.25 vCPU)"
  type        = number
  default     = 256
}

variable "ecs_task_memory" {
  description = "Memory (MiB) for each Fargate task"
  type        = number
  default     = 512
}

variable "ecs_desired_count" {
  description = "Number of ECS tasks to run (change per experiment: 1, 2, 4)"
  type        = number
  default     = 2
}

variable "backend_mode" {
  description = "Stock backend: 'memory' or 'redis'"
  type        = string
  default     = "redis"
}

variable "stock_count" {
  description = "Initial stock for the flash sale"
  type        = number
  default     = 100
}

variable "container_port" {
  description = "Port the Go service listens on inside the container"
  type        = number
  default     = 8080
}

# ---- ElastiCache -------------------------------------------------

variable "redis_node_type" {
  description = "ElastiCache Redis instance type"
  type        = string
  default     = "cache.t3.micro"
}

# ---- Resilience -------------------------------------------------
variable "resilience_mode" {
  description = "Resilience mode: 'none', 'failfast', 'bulkhead', 'all'"
  type        = string
  default     = "none"
}

variable "stock_mode" {
  description = "Stock deduction mode: 'decr' or 'lua'"
  type        = string
  default     = "decr"
}
