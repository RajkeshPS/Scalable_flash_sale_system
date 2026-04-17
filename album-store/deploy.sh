#!/bin/bash
set -e

# ─── Config ──────────────────────────────────────────────────────────────────
REGION="us-west-2"
ACCOUNT_ID="608749156451"
APP_NAME="album-store"
ECR_URL="${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com/${APP_NAME}"

echo "=== Album Store Deploy ==="

# ─── Step 1: ECR Login ──────────────────────────────────────────────────────
echo "[1/4] Logging into ECR..."
aws ecr get-login-password --region $REGION | docker login --username AWS --password-stdin ${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com

# ─── Step 2: Build ───────────────────────────────────────────────────────────
echo "[2/4] Building Docker image..."
docker build -t ${APP_NAME}:latest .

# ─── Step 3: Tag & Push ─────────────────────────────────────────────────────
echo "[3/4] Pushing to ECR..."
docker tag ${APP_NAME}:latest ${ECR_URL}:latest
docker push ${ECR_URL}:latest

# ─── Step 4: Force new deployment ────────────────────────────────────────────
echo "[4/4] Updating ECS service..."
aws ecs update-service \
  --cluster ${APP_NAME}-cluster \
  --service ${APP_NAME}-svc \
  --force-new-deployment \
  --region $REGION

echo ""
echo "=== Deploy complete! ==="
echo "ECS will roll out new tasks. Check status with:"
echo "  aws ecs describe-services --cluster ${APP_NAME}-cluster --services ${APP_NAME}-svc --region $REGION --query 'services[0].deployments'"