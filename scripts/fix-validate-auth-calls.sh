#!/bin/bash
# Fix all ValidateAuthenticatedUser calls to handle 4 return values

set -e

# Find all Go files in api/ directory (excluding tests)
files=$(find api/ -name "*.go" -not -name "*_test.go")

for file in $files; do
    # Skip if file doesn't contain ValidateAuthenticatedUser
    if ! grep -q "ValidateAuthenticatedUser(c)" "$file"; then
        continue
    fi

    echo "Processing $file..."

    # Create a backup
    cp "$file" "$file.bak"

    # Replace patterns:
    # Pattern 1: userEmail, _, err := ValidateAuthenticatedUser(c)
    # becomes:  userEmail, _, _, err := ValidateAuthenticatedUser(c)
    sed -i '' 's/userEmail, _, err := ValidateAuthenticatedUser(c)/userEmail, _, _, err := ValidateAuthenticatedUser(c)/g' "$file"

    # Pattern 2: userEmail, userRole, err := ValidateAuthenticatedUser(c)
    # becomes:  userEmail, _, userRole, err := ValidateAuthenticatedUser(c)
    sed -i '' 's/userEmail, userRole, err := ValidateAuthenticatedUser(c)/userEmail, _, userRole, err := ValidateAuthenticatedUser(c)/g' "$file"

    # Pattern 3: _, _, err := ValidateAuthenticatedUser(c)
    # becomes:  _, _, _, err := ValidateAuthenticatedUser(c)
    sed -i '' 's/_, _, err := ValidateAuthenticatedUser(c)/_, _, _, err := ValidateAuthenticatedUser(c)/g' "$file"

    # Pattern 4: userName, userRole, err := ValidateAuthenticatedUser(c)
    # becomes:  userName, _, userRole, err := ValidateAuthenticatedUser(c)
    sed -i '' 's/userName, userRole, err := ValidateAuthenticatedUser(c)/userName, _, userRole, err := ValidateAuthenticatedUser(c)/g' "$file"

    echo "  ✓ Updated $file"
done

# Also fix the test file
test_file="api/request_utils_test.go"
if [ -f "$test_file" ]; then
    echo "Processing $test_file..."
    cp "$test_file" "$test_file.bak"
    sed -i '' 's/userName, userRole, err := ValidateAuthenticatedUser(c)/userName, _, userRole, err := ValidateAuthenticatedUser(c)/g' "$test_file"
    echo "  ✓ Updated $test_file"
fi

echo ""
echo "All files updated successfully!"
echo "Backup files created with .bak extension"
echo ""
echo "Next steps:"
echo "1. Run: make lint"
echo "2. Run: make build-server"
echo "3. Run: make test-unit"
echo "4. If everything passes, remove .bak files with: find api/ -name '*.bak' -delete"
