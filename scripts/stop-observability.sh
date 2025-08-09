#!/bin/bash

# Stop TMI Observability Stack

set -e

echo "🛑 Stopping TMI Observability Stack..."

# Stop the observability stack
docker-compose -f docker-compose.observability.yml down

echo "✅ Observability stack stopped!"

# Option to clean up volumes (only in interactive mode)
if [ -t 0 ] && [ -t 1 ]; then
    # Interactive mode
    read -p "🗑️  Do you want to remove volumes (this will delete all observability data)? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "🧹 Removing volumes..."
        docker-compose -f docker-compose.observability.yml down -v
        echo "✅ Volumes removed!"
    fi
else
    echo "ℹ️  Running in non-interactive mode, volumes preserved."
    echo "   Use 'make delete-observability' to remove volumes."
fi