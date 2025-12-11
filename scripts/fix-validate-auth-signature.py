#!/usr/bin/env python3
"""
Fix ValidateAuthenticatedUser calls to handle the new 4-value return signature.

Old signature: (email, role, error)
New signature: (email, providerID, role, error)

This script handles all the different patterns:
- userEmail, _, err := ...        -> userEmail, _, _, err := ...
- userEmail, userRole, err := ... -> userEmail, _, userRole, err := ...
- _, _, err := ...                -> _, _, _, err := ...
"""

import re
import glob
import sys

def fix_file(filepath):
    """Fix ValidateAuthenticatedUser calls in a single file."""
    try:
        with open(filepath, 'r') as f:
            content = f.read()

        original_content = content

        # Pattern 1: userEmail, _, err := ValidateAuthenticatedUser(c)
        # Becomes: userEmail, _, _, err := ValidateAuthenticatedUser(c)
        content = re.sub(
            r'(\w+), _, err := ValidateAuthenticatedUser\(c\)',
            r'\1, _, _, err := ValidateAuthenticatedUser(c)',
            content
        )

        # Pattern 2: userEmail, userRole, err := ValidateAuthenticatedUser(c)
        # Becomes: userEmail, _, userRole, err := ValidateAuthenticatedUser(c)
        content = re.sub(
            r'(\w+), (\w+Role), err := ValidateAuthenticatedUser\(c\)',
            r'\1, _, \2, err := ValidateAuthenticatedUser(c)',
            content
        )

        # Pattern 3: _, _, err := ValidateAuthenticatedUser(c)
        # Becomes: _, _, _, err := ValidateAuthenticatedUser(c)
        content = re.sub(
            r'_, _, err := ValidateAuthenticatedUser\(c\)',
            r'_, _, _, err := ValidateAuthenticatedUser(c)',
            content
        )

        # Pattern 4: userEmail, _, err = ValidateAuthenticatedUser(c) (assignment without :=)
        content = re.sub(
            r'(\w+), _, err = ValidateAuthenticatedUser\(c\)',
            r'\1, _, _, err = ValidateAuthenticatedUser(c)',
            content
        )

        # Pattern 5: userEmail, userRole, err = ValidateAuthenticatedUser(c) (assignment without :=)
        content = re.sub(
            r'(\w+), (\w+Role), err = ValidateAuthenticatedUser\(c\)',
            r'\1, _, \2, err = ValidateAuthenticatedUser(c)',
            content
        )

        # Pattern 6: _, _, err = ValidateAuthenticatedUser(c) (assignment without :=)
        content = re.sub(
            r'_, _, err = ValidateAuthenticatedUser\(c\)',
            r'_, _, _, err = ValidateAuthenticatedUser(c)',
            content
        )

        if content != original_content:
            with open(filepath, 'w') as f:
                f.write(content)
            return True
        return False

    except Exception as e:
        print(f"Error processing {filepath}: {e}")
        return False

def main():
    """Process all Go files in api/ directory."""
    files = glob.glob('api/*.go')

    updated_count = 0
    for filepath in files:
        # Skip test files
        if '_test.go' in filepath:
            continue

        if fix_file(filepath):
            print(f"✓ Updated {filepath}")
            updated_count += 1

    # Also process the test file
    test_file = 'api/request_utils_test.go'
    if fix_file(test_file):
        print(f"✓ Updated {test_file}")
        updated_count += 1

    print(f"\nUpdated {updated_count} file(s)")
    return 0

if __name__ == '__main__':
    sys.exit(main())
