#!/bin/bash

# Colorful output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}┌──────────────────────────────────────┐${NC}"
echo -e "${BLUE}│       TMI - Test All                 │${NC}"
echo -e "${BLUE}└──────────────────────────────────────┘${NC}"

# Function to run a command and check if it succeeded
run_step() {
  local step_name=$1
  local command=$2
  
  echo -e "\n${YELLOW}Running ${step_name}...${NC}"
  eval $command
  
  if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ ${step_name} passed!${NC}"
    return 0
  else
    echo -e "${RED}✗ ${step_name} failed!${NC}"
    return 1
  fi
}

# Start with a clean slate
echo -e "\n${BLUE}Starting test sequence...${NC}"

# Step 1: Format code
run_step "Code formatting" "npm run format"
FORMAT_RESULT=$?

# Step 2: Run linting
run_step "ESLint" "npm run lint"
LINT_RESULT=$?

# Step 3: Run type checking
run_step "TypeScript type checking" "npm run typecheck"
TYPECHECK_RESULT=$?

# Step 4: Run unit tests with Cypress component testing
run_step "Unit tests with Cypress" "npm run test:unit"
TEST_RESULT=$?

# Step 5: Run e2e tests
run_step "E2E tests" "npm run e2e"
E2E_RESULT=$?

# Summary
echo -e "\n${BLUE}┌──────────────────────────────────────┐${NC}"
echo -e "${BLUE}│       Test Summary                   │${NC}"
echo -e "${BLUE}└──────────────────────────────────────┘${NC}"

if [ $FORMAT_RESULT -eq 0 ]; then
  echo -e "${GREEN}✓ Formatting: Passed${NC}"
else
  echo -e "${RED}✗ Formatting: Failed${NC}"
fi

if [ $LINT_RESULT -eq 0 ]; then
  echo -e "${GREEN}✓ Linting: Passed${NC}"
else
  echo -e "${RED}✗ Linting: Failed${NC}"
fi

if [ $TYPECHECK_RESULT -eq 0 ]; then
  echo -e "${GREEN}✓ Type checking: Passed${NC}"
else
  echo -e "${RED}✗ Type checking: Failed${NC}"
fi

if [ $TEST_RESULT -eq 0 ]; then
  echo -e "${GREEN}✓ Unit tests: Passed${NC}"
else
  echo -e "${RED}✗ Unit tests: Failed${NC}"
fi

if [ $E2E_RESULT -eq 0 ]; then
  echo -e "${GREEN}✓ E2E tests: Passed${NC}"
else
  echo -e "${RED}✗ E2E tests: Failed${NC}"
fi

# Note: Cypress coverage reporting can be added later if needed
echo -e "\n${BLUE}Note: Test coverage reporting will be configured in Cypress${NC}"

# Final result
if [ $FORMAT_RESULT -eq 0 ] && [ $LINT_RESULT -eq 0 ] && [ $TYPECHECK_RESULT -eq 0 ] && [ $TEST_RESULT -eq 0 ] && [ $E2E_RESULT -eq 0 ]; then
  echo -e "\n${GREEN}All tests passed successfully!${NC}"
  exit 0
else
  echo -e "\n${RED}Some tests failed. Please fix the issues before committing.${NC}"
  exit 1
fi