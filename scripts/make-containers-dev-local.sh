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
PG_DATA_HOST_DIR="/Users/efitz/Projects/tmi/pgsql-data-vol"
PG_CONTAINER_DATA_DIR="/bitnami/postgresql/data" # Bitnami PostgreSQL's data directory
PG_USER="postgres"
PG_PASSWORD="postgres"

# --- Script Setup ---
set -e           # Exit immediately if a command exits with a non-zero status.
set -o pipefail  # Exit if any command in a pipeline fails.

echo "--- TMI Docker Container Setup Script ---"

# --- Pre-checks ---
if ! command -v docker &> /dev/null
then
    echo "Error: Docker is not installed or not in your PATH."
    echo "Please install Docker Desktop for Mac (https://docs.docker.com/desktop/install/mac-install/)"
    exit 1
fi

echo "Docker found. Proceeding with setup."

# --- Create Host Volume Directory (if it doesn't exist) ---
echo "Ensuring host directory for PostgreSQL data exists: ${PG_DATA_HOST_DIR}"
mkdir -p "${PG_DATA_HOST_DIR}"
if [ $? -ne 0 ]; then
    echo "Error: Could not create host directory ${PG_DATA_HOST_DIR}. Check permissions."
    exit 1
fi
echo "Host directory ready."

# --- Stop and Remove Existing Containers (for a clean start) ---
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

# --- Create tmi-postgresql Container ---
echo ""
echo "--- Creating tmi-postgresql container ---"
echo "Image: ${PG_IMAGE}"
echo "Port: 127.0.0.1:${PG_HOST_PORT}:${PG_CONTAINER_PORT}"
echo "Volume: ${PG_DATA_HOST_DIR} -> ${PG_CONTAINER_DATA_DIR}"
echo "User: ${PG_USER}, Password: ${PG_PASSWORD}"

docker run -d \
  --name "${PG_CONTAINER_NAME}" \
  -p "127.0.0.1:${PG_HOST_PORT}:${PG_CONTAINER_PORT}" \
  -v "${PG_DATA_HOST_DIR}:${PG_CONTAINER_DATA_DIR}" \
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
echo "Port: 127.0.0.1:${REDIS_HOST_PORT}:${REDIS_CONTAINER_PORT}"
echo "No password required (ALLOW_EMPTY_PASSWORD=yes)."

docker run -d \
  --name "${REDIS_CONTAINER_NAME}" \
  -p "127.0.0.1:${REDIS_HOST_PORT}:${REDIS_CONTAINER_PORT}" \
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
echo "--- Setup Complete! ---"
echo "You can now connect to PostgreSQL at localhost:${PG_HOST_PORT}"
echo "  User: ${PG_USER}"
echo "  Password: ${PG_PASSWORD}"
echo "You can now connect to Redis at localhost:${REDIS_HOST_PORT}"
echo "  No password required."
echo ""
echo "To stop these containers:"
echo "  docker stop ${PG_CONTAINER_NAME} ${REDIS_CONTAINER_NAME}"
echo "To remove these containers (and preserve PostgreSQL data volume):"
echo "  docker rm ${PG_CONTAINER_NAME} ${REDIS_CONTAINER_NAME}"
echo "To view logs for PostgreSQL:"
echo "  docker logs ${PG_CONTAINER_NAME}"
echo "To view logs for Redis:"
echo "  docker logs ${REDIS_CONTAINER_NAME}"