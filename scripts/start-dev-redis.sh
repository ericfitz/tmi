#!/bin/bash

# Script to ensure the Redis Docker container is running before starting the TMI service
# This script is for development use only

CONTAINER_NAME="tmi-redis"

# Check if the container exists
if ! docker ps -a --format '{{.Names}}' | grep -q $CONTAINER_NAME; then
    echo "Error: Docker container with name $CONTAINER_NAME not found."
    echo "Creating Redis container..."
    
    # Create Redis container if it doesn't exist
    docker run --name $CONTAINER_NAME -e ALLOW_EMPTY_PASSWORD=yes -d -p 6379:6379 tmi-redis
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to create Redis container."
        exit 1
    fi
else
    # Check if the container is running
    if ! docker ps --format '{{.Names}}' | grep -q $CONTAINER_NAME; then
        echo "Starting Redis container $CONTAINER_NAME..."
        docker start $CONTAINER_NAME
    
        # Wait for Redis to be ready
        echo "Waiting for Redis to be ready..."
        sleep 3
    
        # Check if container started successfully
        if ! docker ps --format '{{.Names}}' | grep -q $CONTAINER_NAME; then
            echo "Error: Failed to start Redis container."
            exit 1
        fi
    
        echo "Redis container started successfully."
    else
        echo "Redis container is already running."
    fi
fi

# Verify Redis is accessible
echo "Verifying Redis connection..."
if ! docker exec $CONTAINER_NAME redis-cli ping > /dev/null 2>&1; then
    echo "Warning: Redis is not responding. The container may need more time to initialize."
else
    echo "Redis is ready."
fi

echo "Redis container is ready for TMI service."