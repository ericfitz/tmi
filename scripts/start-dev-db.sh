#!/bin/bash

# Script to ensure the PostgreSQL Docker container is running before starting the TMI service
# This script is for development use only

CONTAINER_NAME="tmi-postgresql"

# Check if the container exists
if ! docker ps -a --format '{{.Names}}' | grep -q $CONTAINER_NAME; then
    echo "Error: Docker container with name $CONTAINER_NAME not found."
    echo "Creating PostgreSQL container..."
    
    # Create PostgreSQL container if it doesn't exist
    docker run --name $CONTAINER_NAME \
        -e POSTGRES_USER=tmi_dev \
        -e POSTGRES_PASSWORD=dev123 \
        -e POSTGRES_DB=tmi_dev \
        -d -p 5432:5432 tmi-postgres
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to create PostgreSQL container."
        exit 1
    fi
    
    # Wait for PostgreSQL to initialize (new containers need more time)
    echo "Waiting for PostgreSQL to initialize (new container)..."
    sleep 10
else
    # Check if the container is running
    if ! docker ps --format '{{.Names}}' | grep -q $CONTAINER_NAME; then
        echo "Starting PostgreSQL container $CONTAINER_NAME..."
        docker start $CONTAINER_NAME
    
        # Wait for PostgreSQL to be ready
        echo "Waiting for PostgreSQL to be ready..."
        sleep 5
    
        # Check if container started successfully
        if ! docker ps --format '{{.Names}}' | grep -q $CONTAINER_NAME; then
            echo "Error: Failed to start PostgreSQL container."
            exit 1
        fi
    
        echo "PostgreSQL container started successfully."
    else
        echo "PostgreSQL container is already running."
    fi
fi

# Verify PostgreSQL is accessible
echo "Verifying PostgreSQL connection..."

# Wait for PostgreSQL to be ready with retries
MAX_RETRIES=15
RETRY_COUNT=0
POSTGRES_READY=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker exec $CONTAINER_NAME pg_isready -U tmi_dev -d tmi_dev -h localhost -p 5432 > /dev/null 2>&1; then
        POSTGRES_READY=true
        break
    fi
    
    RETRY_COUNT=$((RETRY_COUNT + 1))
    echo "PostgreSQL not ready yet (attempt $RETRY_COUNT/$MAX_RETRIES), waiting 2 seconds..."
    sleep 2
done

if [ "$POSTGRES_READY" = true ]; then
    echo "PostgreSQL is ready."
else
    echo "Warning: PostgreSQL is not responding after $MAX_RETRIES attempts. The container may need more time to initialize."
    exit 1
fi

echo "Database container is ready for TMI service."
