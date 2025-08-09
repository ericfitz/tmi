#!/bin/bash

# Start TMI Observability Stack
# This script starts the local observability infrastructure for development

set -e

echo "🔍 Starting TMI Observability Stack..."

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

# Create config directory if it doesn't exist
mkdir -p config

# Start the observability stack
echo "🚀 Starting services..."
docker-compose -f docker-compose.observability.yml up -d

# Wait for services to be ready
echo "⏳ Waiting for services to start..."
sleep 10

# Check service health
echo "🏥 Checking service health..."

# Check Jaeger
if curl -f http://localhost:16686/api/services > /dev/null 2>&1; then
    echo "✅ Jaeger UI available at http://localhost:16686"
else
    echo "⚠️  Jaeger not ready yet, may need more time"
fi

# Check Prometheus
if curl -f http://localhost:9090/-/ready > /dev/null 2>&1; then
    echo "✅ Prometheus available at http://localhost:9090"
else
    echo "⚠️  Prometheus not ready yet, may need more time"
fi

# Check Grafana
if curl -f http://localhost:3000/api/health > /dev/null 2>&1; then
    echo "✅ Grafana available at http://localhost:3000 (admin/admin)"
else
    echo "⚠️  Grafana not ready yet, may need more time"
fi

# Check OpenTelemetry Collector
if curl -f http://localhost:4318/v1/traces > /dev/null 2>&1; then
    echo "✅ OpenTelemetry Collector ready at http://localhost:4318"
else
    echo "⚠️  OpenTelemetry Collector not ready yet, may need more time"
fi

echo ""
echo "🎉 Observability stack started!"
echo ""
echo "Services:"
echo "  📊 Grafana:     http://localhost:3000 (admin/admin)"
echo "  🔍 Jaeger:      http://localhost:16686"
echo "  📈 Prometheus:  http://localhost:9090"
echo "  📋 Loki:       http://localhost:3100"
echo "  📡 OTel:       http://localhost:4318 (HTTP) / :4317 (gRPC)"
echo ""
echo "To stop: docker-compose -f docker-compose.observability.yml down"
echo "To view logs: docker-compose -f docker-compose.observability.yml logs -f [service]"