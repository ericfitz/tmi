#!/bin/bash

# Load environment variables from .env file if it exists
if [ -f .env ]; then
  echo "Loading environment variables from .env file"
  export $(grep -v '^#' .env | xargs)
fi

# Default values
PORT=${PORT:-4200}
HOST=${HOST:-localhost}
export NODE_ENV=development

echo "Starting TMI application in development mode with build watching"
echo "Server will be available at http://$HOST:$PORT"

# Check if nodemon is installed
if ! command -v nodemon &> /dev/null; then
  echo "nodemon is not installed, installing..."
  npm install -D nodemon
fi

# Start the Angular build in watch mode in the background
echo "Starting Angular build in watch mode..."
npm run watch &
WATCH_PID=$!

# Give the build a moment to start
sleep 2

# Start the server with nodemon
echo "Starting Express server with nodemon..."
npx nodemon --watch server.js --watch dist/tmi/ server.js

# If the script is interrupted, kill the watch process
trap "kill $WATCH_PID 2>/dev/null" EXIT

# Wait for the watch process to complete
wait $WATCH_PID