# ---------------------------------------------------------------
# Root main.tf — wires all modules together
# ---------------------------------------------------------------

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    docker = {
      source  = "kreuzwerker/docker"
      version = "~> 3.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ---- ECR Auth Token (for Docker provider to push images) ---------

data "aws_ecr_authorization_token" "token" {
  depends_on = [module.ecr]
}

provider "docker" {
  registry_auth {
    address  = split("/", module.ecr.repository_url)[0]
    username = data.aws_ecr_authorization_token.token.user_name
    password = data.aws_ecr_authorization_token.token.password
  }
}

# ---- Networking (VPC, subnets, IGW, route tables) ----------------

module "network" {
  source       = "./modules/network"
  project_name = var.project_name
  vpc_cidr     = var.vpc_cidr
  az_count     = var.az_count
  aws_region   = var.aws_region
}

# ---- ECR (container registry) ------------------------------------

module "ecr" {
  source       = "./modules/ecr"
  project_name = var.project_name
}

#---- Docker Build & Push -----------------------------------------

resource "docker_image" "app" {
  name = "${module.ecr.repository_url}:latest"

  build {
    context    = "${path.module}/../src"
    dockerfile = "Dockerfile"
  }

  triggers = {
    # Rebuild when any source file changes
    dir_sha = sha1(join("", [
      filesha1("${path.module}/../src/Dockerfile"),
      filesha1("${path.module}/../src/go.mod"),
      filesha1("${path.module}/../src/go.sum"),
      filesha1("${path.module}/../src/cmd/server/main.go"),
      filesha1("${path.module}/../src/config/config.go"),
      filesha1("${path.module}/../src/internal/stock/backend.go"),
      filesha1("${path.module}/../src/internal/handlers/handlers.go"),
    ]))
  }
}

resource "docker_registry_image" "app" {
  name = docker_image.app.name

  # Force push when the local image changes
  keep_remotely = false
}

# ---- ALB (application load balancer) -----------------------------

module "alb" {
  source            = "./modules/alb"
  project_name      = var.project_name
  vpc_id            = module.network.vpc_id
  public_subnet_ids = module.network.public_subnet_ids
  container_port    = var.container_port
}

# ---- CloudWatch Logging ------------------------------------------

module "logging" {
  source       = "./modules/logging"
  project_name = var.project_name
  aws_region   = var.aws_region
}

# ---- ECS Fargate (service + task definition) ---------------------

module "ecs" {
  source = "./modules/ecs"

  project_name   = var.project_name
  aws_region     = var.aws_region
  vpc_id         = module.network.vpc_id
  subnet_ids     = module.network.public_subnet_ids
  container_port = var.container_port

  # Task sizing
  task_cpu    = var.ecs_task_cpu
  task_memory = var.ecs_task_memory

  # Scaling
  desired_count = var.ecs_desired_count

  # Container image — reference the pushed digest so ECS picks up changes
  ecr_repo_url  = module.ecr.repository_url
  image_digest  = docker_registry_image.app.sha256_digest

  # ALB integration
  target_group_arn = module.alb.target_group_arn
  alb_sg_id        = module.alb.alb_security_group_id

  # App config (passed as env vars to the container)
  backend_mode = var.backend_mode
  stock_count  = var.stock_count
  redis_addr   = "${module.network.redis_endpoint}:6379"
  resilience_mode = var.resilience_mode
  stock_mode      = var.stock_mode

  # Logging
  log_group_name = module.logging.log_group_name

  depends_on = [module.alb, docker_registry_image.app]
}