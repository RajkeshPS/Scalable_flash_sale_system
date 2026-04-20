# ---------------------------------------------------------------
# ECS module — Fargate cluster, task definition, service
# ---------------------------------------------------------------

# ---- ECS Cluster -------------------------------------------------

resource "aws_ecs_cluster" "main" {
  name = "${var.project_name}-cluster"
  tags = { Name = "${var.project_name}-cluster" }
}

# ---- IAM: Use pre-existing LabRole (AWS Academy environment)

data "aws_iam_role" "lab_role" {
  name = "LabRole"
}

# ---- Security Group for ECS tasks --------------------------------

resource "aws_security_group" "ecs_tasks" {
  name   = "${var.project_name}-ecs-tasks-sg"
  vpc_id = var.vpc_id

  # Allow traffic from ALB only
  ingress {
    description     = "HTTP from ALB"
    from_port       = var.container_port
    to_port         = var.container_port
    protocol        = "tcp"
    security_groups = [var.alb_sg_id]
  }

  # Allow outbound to anywhere (Redis, ECR, CloudWatch)
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-ecs-tasks-sg" }
}

# ---- Task Definition ---------------------------------------------

resource "aws_ecs_task_definition" "app" {
  family                   = "${var.project_name}-task"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = data.aws_iam_role.lab_role.arn
  task_role_arn            = data.aws_iam_role.lab_role.arn

  container_definitions = jsonencode([
    {
      name      = var.project_name
      image     = "${var.ecr_repo_url}@${var.image_digest}"
      essential = true

      portMappings = [
        {
          containerPort = var.container_port
          protocol      = "tcp"
        }
      ]

      environment = [
        { name = "PORT",         value = tostring(var.container_port) },
        { name = "BACKEND_MODE", value = var.backend_mode },
        { name = "STOCK_COUNT",  value = tostring(var.stock_count) },
        { name = "REDIS_ADDR",   value = var.redis_addr },
        { name = "RESILIENCE_MODE", value = var.resilience_mode },
        { name = "STOCK_MODE",      value = var.stock_mode }
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = var.log_group_name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "ecs"
        }
      }
    }
  ])

  tags = { Name = "${var.project_name}-task" }
}

# ---- ECS Service -------------------------------------------------

resource "aws_ecs_service" "app" {
  name            = "${var.project_name}-service"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = var.desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.subnet_ids
    security_groups  = [aws_security_group.ecs_tasks.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = var.target_group_arn
    container_name   = var.project_name
    container_port   = var.container_port
  }

  # Allow service to stabilize during deploys
  health_check_grace_period_seconds = 30

  tags = { Name = "${var.project_name}-service" }
}
