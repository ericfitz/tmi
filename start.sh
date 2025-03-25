#!/bin/bash

# Load environment variables from .env file if it exists
if [ -f .env ]; then
  echo "Loading environment variables from .env file"
  export $(grep -v '^#' .env | xargs)
fi

# Default values
PORT=${PORT:-4200}
HOST=${HOST:-localhost}
ENV=${ENV:-development}

echo "Starting TMI application in $ENV mode"
echo "Server will be available at http://$HOST:$PORT"

# Export variables for the Node.js process
export PORT=$PORT
export HOST=$HOST

if [ "$ENV" == "production" ]; then
  export NODE_ENV=production
  npm run start:prod
else
  export NODE_ENV=development
  npm run start:dev
fi