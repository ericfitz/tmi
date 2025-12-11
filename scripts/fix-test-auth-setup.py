#!/usr/bin/env python3
"""
Add c.Set("userID", ...) after every c.Set("userEmail", ...) in test files.
The userID should be derived from the email (e.g., "test@example.com" -> "test-provider-id")
"""

import re
import sys

def fix_test_file(filepath):
    """Add userID context variable after userEmail in test setup."""
    try:
        with open(filepath, 'r') as f:
            content = f.read()

        original_content = content
        lines = content.split('\n')
        new_lines = []

        for i, line in enumerate(lines):
            new_lines.append(line)

            # If this line sets userEmail, add userID on the next line (if not already present)
            if 'c.Set("userEmail",' in line:
                # Extract the email value from the line
                match = re.search(r'c\.Set\("userEmail",\s*([^)]+)\)', line)
                if match:
                    email_var = match.group(1).strip()

                    # Check if the next line already sets userID
                    if i + 1 < len(lines) and 'c.Set("userID"' in lines[i + 1]:
                        continue  # Already has userID, skip

                    # Generate a provider ID based on the email variable
                    # Extract indentation from current line
                    indent = re.match(r'^(\s*)', line).group(1)

                    # Add userID line after userEmail
                    # For test purposes, we'll create a simple provider ID based on the email
                    if email_var.startswith('"') and email_var.endswith('"'):
                        # It's a string literal like "test@example.com"
                        email = email_var.strip('"')
                        provider_id = email.split('@')[0] + "-provider-id"
                        user_id_line = f'{indent}c.Set("userID", "{provider_id}")'
                    else:
                        # It's a variable like TestFixtures.OwnerUser
                        # We'll use a generic pattern
                        user_id_line = f'{indent}c.Set("userID", {email_var}+"-provider-id")  // Provider ID for testing'

                    new_lines.append(user_id_line)

        new_content = '\n'.join(new_lines)

        if new_content != original_content:
            with open(filepath, 'w') as f:
                f.write(new_content)
            return True
        return False

    except Exception as e:
        print(f"Error processing {filepath}: {e}")
        return False

def main():
    """Process all test files."""
    import glob

    test_files = glob.glob('api/*_test.go')

    updated_count = 0
    for filepath in test_files:
        if fix_test_file(filepath):
            print(f"✓ Updated {filepath}")
            updated_count += 1

    print(f"\nUpdated {updated_count} file(s)")
    return 0

if __name__ == '__main__':
    sys.exit(main())
