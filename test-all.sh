#!/bin/bash

# Colors for terminal output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Starting comprehensive test suite for TMI...${NC}"

# Run linting
echo -e "\n${YELLOW}Running lint check...${NC}"
if npm run lint; then
  echo -e "${GREEN}Lint check passed!${NC}"
else
  echo -e "${RED}Lint check failed!${NC}"
  exit 1
fi

# Run TypeScript type checking
echo -e "\n${YELLOW}Running type check...${NC}"
if npm run typecheck; then
  echo -e "${GREEN}Type check passed!${NC}"
else
  echo -e "${RED}Type check failed!${NC}"
  exit 1
fi

# Run all unit tests with coverage
echo -e "\n${YELLOW}Running unit tests with coverage...${NC}"
if npm test -- --no-watch --code-coverage; then
  echo -e "${GREEN}All tests passed!${NC}"
else
  echo -e "${RED}Some tests failed!${NC}"
  exit 1
fi

# Print a summary of the coverage report
echo -e "\n${YELLOW}Coverage summary:${NC}"
cat coverage/*/coverage-summary.json | grep "total"

echo -e "\n${GREEN}All checks passed successfully!${NC}"
exit 0