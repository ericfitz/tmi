#!/bin/bash

# --- Configuration ---
PG_CONTAINER_NAME="tmi-postgresql"
REDIS_CONTAINER_NAME="tmi-redis"
PG_IMAGE="bitnami/postgresql:latest"
REDIS_IMAGE="bitnami/redis:latest"
PG_HOST_PORT="5432"
PG_CONTAINER_PORT="5432"
REDIS_HOST_PORT="6379"
REDIS_CONTAINER_PORT="6379"
PG_USER="postgres"
PG_PASSWORD="postgres"

# ECR Configuration
ECR_REGISTRY="706702818127.dkr.ecr.us-east-1.amazonaws.com"
ECR_REGION="us-east-1"
ECR_PG_REPOSITORY="efitz/tmi-postgresql-dev"
ECR_REDIS_REPOSITORY="efitz/tmi-redis-dev"

# --- Script Setup ---
set -e           # Exit immediately if a command exits with a non-zero status.
set -o pipefail  # Exit if any command in a pipeline fails.

echo "--- TMI Docker Container Setup and ECR Push Script ---"

# --- Pre-checks ---
if ! command -v docker &> /dev/null
then
    echo "Error: Docker is not installed or not in your PATH."
    echo "Please install Docker Desktop for Mac (https://docs.docker.com/desktop/install/mac-install/)"
    exit 1
fi

if ! command -v aws &> /dev/null
then
    echo "Error: AWS CLI is not installed or not in your PATH."
    echo "Please install AWS CLI (https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)"
    exit 1
fi

echo "Docker and AWS CLI found. Proceeding with setup."

# --- Check AWS credentials ---
echo "Checking AWS credentials..."
if ! aws sts get-caller-identity &> /dev/null; then
    echo "Error: AWS credentials not configured or invalid."
    echo "Please configure your AWS credentials using 'aws configure' or environment variables."
    exit 1
fi

AWS_ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
echo "Using AWS Account: ${AWS_ACCOUNT}"

# --- Authenticate Docker to ECR ---
echo ""
echo "--- Authenticating Docker to ECR ---"
echo "Region: ${ECR_REGION}"
echo "Registry: ${ECR_REGISTRY}"

aws ecr get-login-password --region "${ECR_REGION}" | docker login --username AWS --password-stdin "${ECR_REGISTRY}"

if [ $? -ne 0 ]; then
    echo "Error: Failed to authenticate Docker with ECR."
    exit 1
fi
echo "Successfully authenticated with ECR."

# --- Stop and Remove Existing Containers (for a clean start) ---
echo ""
echo "Checking for and stopping/removing existing containers..."

if docker ps -a --format "{{.Names}}" | grep -Eq "^${PG_CONTAINER_NAME}$"; then
    echo "Stopping existing ${PG_CONTAINER_NAME}..."
    docker stop "${PG_CONTAINER_NAME}" > /dev/null
    echo "Removing existing ${PG_CONTAINER_NAME}..."
    docker rm "${PG_CONTAINER_NAME}" > /dev/null
else
    echo "${PG_CONTAINER_NAME} not found or not running."
fi

if docker ps -a --format "{{.Names}}" | grep -Eq "^${REDIS_CONTAINER_NAME}$"; then
    echo "Stopping existing ${REDIS_CONTAINER_NAME}..."
    docker stop "${REDIS_CONTAINER_NAME}" > /dev/null
    echo "Removing existing ${REDIS_CONTAINER_NAME}..."
    docker rm "${REDIS_CONTAINER_NAME}" > /dev/null
else
    echo "${REDIS_CONTAINER_NAME} not found or not running."
fi

echo "Previous containers cleaned up (if they existed)."

# --- Pull and Push PostgreSQL Image to ECR ---
echo ""
echo "--- Processing PostgreSQL Image ---"
echo "Pulling image: ${PG_IMAGE}"
docker pull "${PG_IMAGE}"

if [ $? -ne 0 ]; then
    echo "Error: Failed to pull ${PG_IMAGE}."
    exit 1
fi

# Tag for ECR
ECR_PG_URI="${ECR_REGISTRY}/${ECR_PG_REPOSITORY}:latest"
echo "Tagging for ECR: ${ECR_PG_URI}"
docker tag "${PG_IMAGE}" "${ECR_PG_URI}"

# Push to ECR
echo "Pushing PostgreSQL image to ECR..."
docker push "${ECR_PG_URI}"

if [ $? -ne 0 ]; then
    echo "Error: Failed to push PostgreSQL image to ECR."
    exit 1
fi
echo "Successfully pushed PostgreSQL image to ECR: ${ECR_PG_URI}"

# --- Pull and Push Redis Image to ECR ---
echo ""
echo "--- Processing Redis Image ---"
echo "Pulling image: ${REDIS_IMAGE}"
docker pull "${REDIS_IMAGE}"

if [ $? -ne 0 ]; then
    echo "Error: Failed to pull ${REDIS_IMAGE}."
    exit 1
fi

# Tag for ECR
ECR_REDIS_URI="${ECR_REGISTRY}/${ECR_REDIS_REPOSITORY}:latest"
echo "Tagging for ECR: ${ECR_REDIS_URI}"
docker tag "${REDIS_IMAGE}" "${ECR_REDIS_URI}"

# Push to ECR
echo "Pushing Redis image to ECR..."
docker push "${ECR_REDIS_URI}"

if [ $? -ne 0 ]; then
    echo "Error: Failed to push Redis image to ECR."
    exit 1
fi
echo "Successfully pushed Redis image to ECR: ${ECR_REDIS_URI}"

# --- Create tmi-postgresql Container ---
echo ""
echo "--- Creating tmi-postgresql container ---"
echo "Image: ${PG_IMAGE}"
echo "Port: 0.0.0.0:${PG_HOST_PORT}:${PG_CONTAINER_PORT}"
echo "User: ${PG_USER}, Password: ${PG_PASSWORD}"

docker run -d \
  --name "${PG_CONTAINER_NAME}" \
  -p "0.0.0.0:${PG_HOST_PORT}:${PG_CONTAINER_PORT}" \
  -e "POSTGRESQL_USERNAME=${PG_USER}" \
  -e "POSTGRESQL_PASSWORD=${PG_PASSWORD}" \
  --restart=unless-stopped \
  "${PG_IMAGE}"

if [ $? -ne 0 ]; then
    echo "Error: Failed to create ${PG_CONTAINER_NAME} container."
    exit 1
fi
echo "${PG_CONTAINER_NAME} container created successfully."

# --- Create tmi-redis Container ---
echo ""
echo "--- Creating tmi-redis container ---"
echo "Image: ${REDIS_IMAGE}"
echo "Port: 0.0.0.0:${REDIS_HOST_PORT}:${REDIS_CONTAINER_PORT}"
echo "No password required (ALLOW_EMPTY_PASSWORD=yes)."

docker run -d \
  --name "${REDIS_CONTAINER_NAME}" \
  -p "0.0.0.0:${REDIS_HOST_PORT}:${REDIS_CONTAINER_PORT}" \
  -e "ALLOW_EMPTY_PASSWORD=yes" \
  --restart=unless-stopped \
  "${REDIS_IMAGE}"

if [ $? -ne 0 ]; then
    echo "Error: Failed to create ${REDIS_CONTAINER_NAME} container."
    exit 1
fi
echo "${REDIS_CONTAINER_NAME} container created successfully."

# --- Verification ---
echo ""
echo "--- Verification ---"
echo "Waiting a few seconds for containers to initialize..."
sleep 5 # Give containers a moment to start up

echo "Checking running Docker containers:"
docker ps -f name="${PG_CONTAINER_NAME}" -f name="${REDIS_CONTAINER_NAME}"

echo ""
echo "Checking ECR repositories for pushed images..."
echo "PostgreSQL images:"
aws ecr describe-images \
    --repository-name "${ECR_PG_REPOSITORY}" \
    --region "${ECR_REGION}" \
    --query 'imageDetails[*].[imageTags[0],imagePushedAt,imageSizeInBytes]' \
    --output table 2>/dev/null || echo "No images found in PostgreSQL repository"

echo ""
echo "Redis images:"
aws ecr describe-images \
    --repository-name "${ECR_REDIS_REPOSITORY}" \
    --region "${ECR_REGION}" \
    --query 'imageDetails[*].[imageTags[0],imagePushedAt,imageSizeInBytes]' \
    --output table 2>/dev/null || echo "No images found in Redis repository"

echo ""
echo "--- Setup Complete! ---"
echo "Local containers are running:"
echo "  PostgreSQL: localhost:${PG_HOST_PORT} (User: ${PG_USER}, Password: ${PG_PASSWORD})"
echo "  Redis: localhost:${REDIS_HOST_PORT} (No password required)"
echo ""
echo "Images pushed to ECR:"
echo "  PostgreSQL: ${ECR_PG_URI}"
echo "  Redis: ${ECR_REDIS_URI}"
echo ""
echo "To stop these containers:"
echo "  docker stop ${PG_CONTAINER_NAME} ${REDIS_CONTAINER_NAME}"
echo "To remove these containers:"
echo "  docker rm ${PG_CONTAINER_NAME} ${REDIS_CONTAINER_NAME}"
echo "To view logs:"
echo "  docker logs ${PG_CONTAINER_NAME}"
echo "  docker logs ${REDIS_CONTAINER_NAME}"
echo ""
echo "For ECS deployment, use the ECR URIs:"
echo "  PostgreSQL: ${ECR_PG_URI}"
echo "  Redis: ${ECR_REDIS_URI}"