#!/bin/sh
# Promtail entrypoint script
# Generates config from environment variables (POSIX-compliant, no bash/sed needed)

set -e

CONFIG_FILE="/tmp/promtail-config.yaml"

# Check if LOKI_URL is set
if [ -z "$LOKI_URL" ]; then
    echo "ERROR: LOKI_URL environment variable is required"
    exit 1
fi

echo "Generating Promtail configuration..."

# Generate config with heredoc and variable substitution
if [ -n "$LOKI_USERNAME" ] && [ -n "$LOKI_PASSWORD" ]; then
    echo "Configuring basic authentication..."
    cat > "$CONFIG_FILE" << EOF
server:
  http_listen_port: 0
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: ${LOKI_URL}
    basic_auth:
      username: ${LOKI_USERNAME}
      password: ${LOKI_PASSWORD}

scrape_configs:
  # Development environment - logs directory in project root
  - job_name: tmiserver-dev
    static_configs:
      - targets:
          - localhost
        labels:
          job: tmiserver
          app: tmi-server
          environment: development
          source: docker-container
          __path__: /logs/tmi.log
      - targets:
          - localhost
        labels:
          job: tmiserver
          app: tmi-server
          environment: development
          source: docker-container
          log_type: server
          __path__: /logs/server.log

  # Production environment - /var/log/tmi directory
  - job_name: tmiserver-prod
    static_configs:
      - targets:
          - localhost
        labels:
          job: tmiserver
          app: tmi-server
          environment: production
          source: docker-container
          __path__: /var/log/tmi/tmi.log
EOF
else
    echo "No basic authentication configured (credentials may be embedded in URL)"
    cat > "$CONFIG_FILE" << EOF
server:
  http_listen_port: 0
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: ${LOKI_URL}

scrape_configs:
  # Development environment - logs directory in project root
  - job_name: tmiserver-dev
    static_configs:
      - targets:
          - localhost
        labels:
          job: tmiserver
          app: tmi-server
          environment: development
          source: docker-container
          __path__: /logs/tmi.log
      - targets:
          - localhost
        labels:
          job: tmiserver
          app: tmi-server
          environment: development
          source: docker-container
          log_type: server
          __path__: /logs/server.log

  # Production environment - /var/log/tmi directory
  - job_name: tmiserver-prod
    static_configs:
      - targets:
          - localhost
        labels:
          job: tmiserver
          app: tmi-server
          environment: production
          source: docker-container
          __path__: /var/log/tmi/tmi.log
EOF
fi

echo "Configuration generated successfully"
echo "Starting Promtail..."

# Execute promtail with generated config
exec /promtail -config.file="$CONFIG_FILE" "$@"
