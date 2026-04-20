# ---------------------------------------------------------------
# Logging module — CloudWatch log group for ECS containers
# ---------------------------------------------------------------



resource "aws_cloudwatch_log_group" "ecs" {
  name              = "/ecs/${var.project_name}"
  retention_in_days = 7 # Keep logs for 1 week (enough for experiments)

  tags = { Name = "${var.project_name}-logs" }
}

