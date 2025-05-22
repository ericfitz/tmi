# TMI Development Setup

This document describes how to set up and run the TMI service for local development.

## Prerequisites

- Go 1.16 or higher
- Docker
- PostgreSQL Docker container named `tmi-postgresql` with ID `b63643222cd56b8893eb1c8cb830b8ef4bb8da065744d121403b6079b503f1a4`
- Redis Docker container named `tmi-redis` with ID `fc9b4ed4d1d3d71d50244d34a348655ea1e6cfb6f294deaacf207386c955a1b7`

## Development Environment Configuration

The development environment uses a local PostgreSQL Docker container for the database and a Redis Docker container for caching. The configuration is stored in the `.env.dev` file, which contains sensitive information like database and Redis passwords and should not be committed to the repository.

### Setting Up the Development Environment

1. Copy the example environment file to create your development configuration:

   ```bash
   cp .env.example .env.dev
   ```

2. Edit `.env.dev` and update the database and Redis configuration to match your local Docker containers:

   ```
   # PostgreSQL configuration
   POSTGRES_HOST=localhost
   POSTGRES_PORT=5432
   POSTGRES_USER=postgres
   POSTGRES_PASSWORD=your-postgres-password-here
   POSTGRES_DB=tmi
   POSTGRES_SSLMODE=disable

   # Redis configuration
   REDIS_HOST=localhost
   REDIS_PORT=6379
   REDIS_PASSWORD=your-redis-password-here
   REDIS_DB=0
   ```

3. Make sure the scripts are executable:
   ```bash
   chmod +x scripts/start-dev-db.sh scripts/start-dev-redis.sh scripts/start-dev.sh
   ```

## Starting the Development Server

To start the TMI service with the development configuration, you can use either the script directly or the Makefile target:

```bash
# Using the script directly
./scripts/start-dev.sh

# Using the Makefile target
make dev
```

This script will:

1. Check if the PostgreSQL Docker container is running and start it if needed
2. Check if the Redis Docker container is running and start it if needed
3. Start the TMI service with the development configuration from `.env.dev`

## Manual Steps

If you prefer to start the services manually:

1. Ensure the PostgreSQL Docker container is running:

   ```bash
   # Using the script directly
   ./scripts/start-dev-db.sh

   # Using the Makefile target
   make dev-db
   ```

2. Ensure the Redis Docker container is running:

   ```bash
   # Using the script directly
   ./scripts/start-dev-redis.sh

   # Using the Makefile target
   make dev-redis
   ```

3. Start the TMI service with the development configuration:
   ```bash
   go run cmd/server/main.go --env=.env.dev
   ```

## Troubleshooting

### Database Connection Issues

If you encounter database connection issues:

1. Verify the PostgreSQL Docker container is running:

   ```bash
   docker ps | grep tmi-postgresql
   ```

2. Check the container logs for any errors:

   ```bash
   docker logs b63643222cd56b8893eb1c8cb830b8ef4bb8da065744d121403b6079b503f1a4
   ```

3. Verify the database connection settings in `.env.dev` match the Docker container configuration.

### Redis Connection Issues

If you encounter Redis connection issues:

1. Verify the Redis Docker container is running:

   ```bash
   docker ps | grep tmi-redis
   ```

2. Check the container logs for any errors:

   ```bash
   docker logs fc9b4ed4d1d3d71d50244d34a348655ea1e6cfb6f294deaacf207386c955a1b7
   ```

3. Verify the Redis connection settings in `.env.dev` match the Docker container configuration.

4. Test the Redis connection:

   ```bash
   docker exec fc9b4ed4d1d3d71d50244d34a348655ea1e6cfb6f294deaacf207386c955a1b7 redis-cli ping
   ```

### Service Startup Issues

If the TMI service fails to start:

1. Check the logs for any error messages
2. Verify that all required environment variables are set in `.env.dev`
3. Ensure the database migrations can run successfully
