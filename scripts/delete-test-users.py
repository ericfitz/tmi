#!/usr/bin/env python3

# /// script
# dependencies = ["requests>=2.32.0"]
# ///

"""
TMI Test User, Group, and CATS Artifact Cleanup Script

Deletes all test users, groups, and CATS-seeded artifacts from the TMI database
via the admin API. Preserves the charlie@tmi.local admin account (authenticated
identity), any non-TMI provider users, and the "everyone" pseudo-group.

CATS artifacts are identified by the "CATS Test" name prefix used by cats-seed.
This includes threat models (and all sub-resources), webhooks, addons, surveys,
survey responses, and client credentials.

Prerequisites:
    1. TMI server must be running (make start-dev)
    2. OAuth callback stub must be running (make start-oauth-stub)
    3. charlie@tmi.local must exist and be an administrator

Authentication Flow:
    This script performs OAuth 2.0 authentication using the PKCE flow via
    the OAuth callback stub. See scripts/oauth-client-callback-stub.py for details.

API Endpoints Used:
    - GET    /admin/users           - List all users (paginated)
    - DELETE /admin/users/{uuid}    - Delete user and cascade all related data
    - GET    /admin/groups          - List all groups (paginated)
    - DELETE /admin/groups/{uuid}   - Delete group
    - GET    /threat_models         - List threat models (paginated)
    - DELETE /threat_models/{id}    - Delete threat model and sub-resources
    - GET    /admin/webhooks/subscriptions - List webhooks
    - DELETE /admin/webhooks/subscriptions/{id} - Delete webhook
    - GET    /addons                - List addons
    - DELETE /addons/{id}           - Delete addon
    - GET    /admin/surveys         - List surveys
    - DELETE /admin/surveys/{id}    - Delete survey
    - GET    /intake/survey_responses - List survey responses
    - DELETE /intake/survey_responses/{id} - Delete survey response
    - GET    /me/client_credentials - List client credentials
    - DELETE /me/client_credentials/{id} - Delete client credential

Usage:
    uv run scripts/delete-test-users.py
    uv run scripts/delete-test-users.py --dry-run
    uv run scripts/delete-test-users.py --users-only
    uv run scripts/delete-test-users.py --groups-only
    uv run scripts/delete-test-users.py --cats-only
"""

import argparse
import sys
from dataclasses import dataclass

import requests  # ty:ignore[unresolved-import]

# Configuration
API_BASE = "http://localhost:8080"
OAUTH_STUB = "http://localhost:8079"
ADMIN_USER = "charlie"
ADMIN_EMAIL = f"{ADMIN_USER}@tmi.local"
EVERYONE_GROUP = "everyone"

# Built-in pseudo-groups created by api/seed/seed.go. The seed function uses
# provider="tmi" for all of these, so naive provider-based filtering treats them
# as deletable user groups. Deleting any of them silently breaks the auth
# system: no Administrators -> no admin checks; no security-reviewers -> no
# triage role; no tmi-automation -> all automation calls fail; etc.
# Keep this set in sync with the seedXxxGroup functions in api/seed/seed.go.
BUILTIN_GROUPS: set[str] = {
    "everyone",
    "administrators",
    "security-reviewers",
    "confidential-project-reviewers",
    "tmi-automation",
    "embedding-automation",
}

# ANSI colors
RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[1;33m"
CYAN = "\033[0;36m"
NC = "\033[0m"  # No Color


CATS_NAME_PREFIX = "CATS Test"


@dataclass
class Stats:
    """Track deletion statistics."""

    users_deleted: int = 0
    users_failed: int = 0
    users_skipped: int = 0
    groups_deleted: int = 0
    groups_failed: int = 0
    groups_skipped: int = 0
    cats_deleted: int = 0
    cats_failed: int = 0
    cats_skipped: int = 0


def print_error(msg: str) -> None:
    """Print error message in red."""
    print(f"{RED}Error: {msg}{NC}", file=sys.stderr)


def print_success(msg: str) -> None:
    """Print success message in green."""
    print(f"{GREEN}{msg}{NC}")


def print_warning(msg: str) -> None:
    """Print warning message in yellow."""
    print(f"{YELLOW}{msg}{NC}")


def print_info(msg: str) -> None:
    """Print info message in cyan."""
    print(f"{CYAN}{msg}{NC}")


def check_server_running() -> bool:
    """Check if TMI server is running."""
    try:
        response = requests.get(f"{API_BASE}/", timeout=5)
        return response.status_code == 200
    except requests.RequestException:
        return False


def check_oauth_stub_running() -> bool:
    """Check if OAuth stub is running."""
    try:
        response = requests.get(f"{OAUTH_STUB}/", timeout=5)
        # Stub returns various codes, just check it responds
        return True
    except requests.RequestException:
        return False


def authenticate_as_charlie() -> str | None:
    """
    Authenticate as charlie@tmi.local via OAuth PKCE flow.

    Returns the access token or None on failure.
    """
    print(f"Authenticating as {ADMIN_EMAIL}...")

    # Step 1: Initialize OAuth flow (generates PKCE code_verifier/code_challenge)
    try:
        init_response = requests.post(
            f"{OAUTH_STUB}/oauth/init",
            json={"userid": ADMIN_USER},
            timeout=10,
        )
        init_response.raise_for_status()
        init_data = init_response.json()
    except requests.RequestException as e:
        print_error(f"Failed to initialize OAuth flow: {e}")
        return None

    auth_url = init_data.get("authorization_url")
    if not auth_url:
        print_error("No authorization_url in OAuth init response")
        return None

    # Step 2: Execute authorization request (stub receives callback with code)
    try:
        requests.get(auth_url, timeout=30, allow_redirects=True)
    except requests.RequestException as e:
        print_error(f"Authorization request failed: {e}")
        return None

    # Step 3: Retrieve the access token (stub already exchanged code for tokens)
    try:
        creds_response = requests.get(
            f"{OAUTH_STUB}/creds",
            params={"userid": ADMIN_USER},
            timeout=10,
        )
        creds_response.raise_for_status()
        creds_data = creds_response.json()
    except requests.RequestException as e:
        print_error(f"Failed to retrieve credentials: {e}")
        return None

    token = creds_data.get("access_token")
    if not token:
        print_error("No access_token in credentials response")
        return None

    print_success("Authenticated successfully")
    return token


def is_test_user(user: dict) -> bool:
    """
    Determine if a user is a test user that should be deleted.

    Test users are identified by email ending in @tmi.local.
    The charlie@tmi.local admin account is excluded.
    """
    email = user.get("email", "")

    # Test users have @tmi.local email addresses
    if not email.endswith("@tmi.local"):
        return False

    # Never delete the admin account we're authenticated as
    if email == ADMIN_EMAIL:
        return False

    return True


def is_test_group(group: dict) -> bool:
    """
    Determine if a group is a test group that should be deleted.

    Test groups are TMI-managed groups (provider="tmi" or no provider) that
    are NOT in the BUILTIN_GROUPS allowlist. Built-in pseudo-groups (created
    by api/seed/seed.go) are protected — deleting any of them silently breaks
    the auth system.
    """
    group_name = group.get("group_name", "")
    provider = group.get("provider", "")

    # Never delete a built-in pseudo-group (everyone, administrators, etc.)
    if group_name in BUILTIN_GROUPS:
        return False

    # Test groups are TMI-managed (provider is "tmi" or empty/*)
    # Groups from external providers (github, google, etc.) are not test groups
    if provider and provider not in ("tmi", "*"):
        return False

    return True


def fetch_all_users(token: str) -> list[dict]:
    """
    Fetch all users from the admin API, handling pagination.

    Returns a list of all user records.
    """
    users = []
    offset = 0
    limit = 50

    headers = {"Authorization": f"Bearer {token}"}

    while True:
        try:
            response = requests.get(
                f"{API_BASE}/admin/users",
                headers=headers,
                params={"limit": limit, "offset": offset},
                timeout=30,
            )
            response.raise_for_status()
            data = response.json()
        except requests.RequestException as e:
            print_error(f"Failed to fetch users (offset={offset}): {e}")
            break

        batch = data.get("users", [])
        users.extend(batch)

        # Check if there are more pages
        total = data.get("total", 0)
        if offset + len(batch) >= total:
            break
        offset += limit

    return users


def fetch_all_groups(token: str) -> list[dict]:
    """
    Fetch all groups from the admin API, handling pagination.

    Returns a list of all group records.
    """
    groups = []
    offset = 0
    limit = 50

    headers = {"Authorization": f"Bearer {token}"}

    while True:
        try:
            response = requests.get(
                f"{API_BASE}/admin/groups",
                headers=headers,
                params={"limit": limit, "offset": offset},
                timeout=30,
            )
            response.raise_for_status()
            data = response.json()
        except requests.RequestException as e:
            print_error(f"Failed to fetch groups (offset={offset}): {e}")
            break

        batch = data.get("groups", [])
        groups.extend(batch)

        # Check if there are more pages
        total = data.get("total", 0)
        if offset + len(batch) >= total:
            break
        offset += limit

    return groups


def delete_user(token: str, user: dict, dry_run: bool = False) -> bool:
    """
    Delete a user via the admin API.

    Returns True on success, False on failure.
    """
    uuid = user.get("internal_uuid")
    email = user.get("email", "unknown")

    if dry_run:
        print(f"  [DRY RUN] Would delete user: {email}")
        return True

    print(f"  Deleting user: {email}... ", end="", flush=True)

    headers = {"Authorization": f"Bearer {token}"}

    try:
        response = requests.delete(
            f"{API_BASE}/admin/users/{uuid}",
            headers=headers,
            timeout=300,  # User deletion cascades through many child entities
        )

        if response.status_code == 204:
            print_success("OK")
            return True
        else:
            print(f"{RED}FAILED (HTTP {response.status_code}){NC}")
            if response.text:
                print(f"    Response: {response.text}")
            return False

    except requests.RequestException as e:
        print(f"{RED}FAILED ({e}){NC}")
        return False


def delete_group(token: str, group: dict, dry_run: bool = False) -> bool:
    """
    Delete a group via the admin API.

    Returns True on success, False on failure.
    """
    uuid = group.get("internal_uuid")
    group_name = group.get("group_name", "unknown")

    if dry_run:
        print(f"  [DRY RUN] Would delete group: {group_name}")
        return True

    print(f"  Deleting group: {group_name}... ", end="", flush=True)

    headers = {"Authorization": f"Bearer {token}"}

    try:
        response = requests.delete(
            f"{API_BASE}/admin/groups/{uuid}",
            headers=headers,
            timeout=30,
        )

        if response.status_code == 204:
            print_success("OK")
            return True
        elif response.status_code == 403:
            print(f"{YELLOW}PROTECTED{NC}")
            return False
        else:
            print(f"{RED}FAILED (HTTP {response.status_code}){NC}")
            if response.text:
                print(f"    Response: {response.text}")
            return False

    except requests.RequestException as e:
        print(f"{RED}FAILED ({e}){NC}")
        return False


def cleanup_users(token: str, dry_run: bool = False) -> tuple[int, int, int]:
    """
    Delete all test users.

    Returns tuple of (deleted, failed, skipped) counts.
    """
    print("\nFetching users...")
    users = fetch_all_users(token)
    print(f"Found {len(users)} total users")

    test_users = [u for u in users if is_test_user(u)]
    skipped = len(users) - len(test_users)

    if not test_users:
        print_success("No test users to delete")
        return 0, 0, skipped

    print(f"Found {len(test_users)} test users to delete")
    print()

    deleted = 0
    failed = 0

    for user in test_users:
        if delete_user(token, user, dry_run):
            deleted += 1
        else:
            failed += 1

    return deleted, failed, skipped


def cleanup_groups(token: str, dry_run: bool = False) -> tuple[int, int, int]:
    """
    Delete all test groups.

    Returns tuple of (deleted, failed, skipped) counts.
    """
    print("\nFetching groups...")
    groups = fetch_all_groups(token)
    print(f"Found {len(groups)} total groups")

    test_groups = [g for g in groups if is_test_group(g)]
    skipped = len(groups) - len(test_groups)

    if not test_groups:
        print_success("No test groups to delete")
        return 0, 0, skipped

    print(f"Found {len(test_groups)} test groups to delete")
    print()

    deleted = 0
    failed = 0

    for group in test_groups:
        if delete_group(token, group, dry_run):
            deleted += 1
        else:
            failed += 1

    return deleted, failed, skipped


def is_cats_artifact(item: dict, name_field: str = "name") -> bool:
    """Check if an item is a CATS-seeded artifact by name prefix."""
    name = item.get(name_field, "")
    return name.startswith(CATS_NAME_PREFIX)


def fetch_paginated(token: str, endpoint: str, items_key: str) -> list[dict]:
    """Fetch all items from a paginated API endpoint."""
    items = []
    offset = 0
    limit = 50
    headers = {"Authorization": f"Bearer {token}"}

    while True:
        try:
            response = requests.get(
                f"{API_BASE}{endpoint}",
                headers=headers,
                params={"limit": limit, "offset": offset},
                timeout=30,
            )
            response.raise_for_status()
            data = response.json()
        except requests.RequestException as e:
            print_error(f"Failed to fetch {endpoint} (offset={offset}): {e}")
            break

        batch = data.get(items_key, [])
        items.extend(batch)

        total = data.get("total", len(batch))
        if offset + len(batch) >= total:
            break
        offset += limit

    return items


def delete_resource(
    token: str, endpoint: str, resource_id: str, label: str, dry_run: bool = False
) -> bool:
    """Delete a resource via the API. Returns True on success."""
    if dry_run:
        print(f"  [DRY RUN] Would delete {label}")
        return True

    print(f"  Deleting {label}... ", end="", flush=True)
    headers = {"Authorization": f"Bearer {token}"}

    try:
        response = requests.delete(
            f"{API_BASE}{endpoint}/{resource_id}",
            headers=headers,
            timeout=30,
        )
        if response.status_code in (200, 204):
            print_success("OK")
            return True
        else:
            print(f"{RED}FAILED (HTTP {response.status_code}){NC}")
            if response.text:
                print(f"    Response: {response.text[:200]}")
            return False
    except requests.RequestException as e:
        print(f"{RED}FAILED ({e}){NC}")
        return False


def cleanup_cats_artifacts(
    token: str, dry_run: bool = False
) -> tuple[int, int, int]:
    """
    Delete all CATS-seeded artifacts.

    Deletion order matters due to dependencies:
    1. Addons (depend on webhooks and threat models)
    2. Client credentials
    3. Survey responses (depend on surveys)
    4. Surveys
    5. Webhooks
    6. Threat models (cascade deletes sub-resources)

    Returns tuple of (deleted, failed, skipped) counts.
    """
    print("\nCleaning up CATS artifacts...")
    deleted = 0
    failed = 0
    skipped = 0

    # Define resource types in dependency order
    resource_types = [
        {
            "name": "addons",
            "endpoint": "/addons",
            "items_key": "addons",
            "name_field": "name",
        },
        {
            "name": "client credentials",
            "endpoint": "/me/client_credentials",
            "items_key": "credentials",
            "name_field": "name",
        },
        {
            "name": "survey responses",
            "endpoint": "/intake/survey_responses",
            "items_key": "survey_responses",
            "name_field": "survey_id",  # handled specially below
        },
        {
            "name": "surveys",
            "endpoint": "/admin/surveys",
            "items_key": "surveys",
            "name_field": "name",
        },
        {
            "name": "webhooks",
            "endpoint": "/admin/webhooks/subscriptions",
            "items_key": "subscriptions",
            "name_field": "name",
        },
        {
            "name": "threat models",
            "endpoint": "/threat_models",
            "items_key": "threat_models",
            "name_field": "name",
        },
    ]

    # Collect CATS survey IDs so we can match survey responses
    cats_survey_ids: set[str] = set()

    for rt in resource_types:
        print(f"\n  Fetching {rt['name']}...")
        items = fetch_paginated(token, rt["endpoint"], rt["items_key"])

        # For surveys, collect their IDs for survey response matching
        if rt["name"] == "surveys":
            for item in items:
                if is_cats_artifact(item, rt["name_field"]):
                    cats_survey_ids.add(item.get("id", ""))

        # Filter to CATS artifacts
        cats_items = []
        for item in items:
            if rt["name"] == "survey responses":
                # Match survey responses by their survey_id
                if item.get("survey_id", "") in cats_survey_ids:
                    cats_items.append(item)
                else:
                    skipped += 1
            elif is_cats_artifact(item, rt["name_field"]):
                cats_items.append(item)
            else:
                skipped += 1

        if not cats_items:
            print(f"  No CATS {rt['name']} found")
            continue

        print(f"  Found {len(cats_items)} CATS {rt['name']} to delete")

        for item in cats_items:
            item_id = item.get("id", "")
            item_label = item.get(rt["name_field"], item_id)
            if rt["name"] == "survey responses":
                item_label = f"survey response {item_id[:8]}..."
            label = f"{rt['name'].rstrip('s')}: {item_label}"
            if delete_resource(token, rt["endpoint"], item_id, label, dry_run):
                deleted += 1
            else:
                failed += 1

    return deleted, failed, skipped


def print_summary(stats: Stats, dry_run: bool = False) -> None:
    """Print summary of cleanup operations."""
    print()
    print("=" * 50)
    if dry_run:
        print_warning("DRY RUN - No changes were made")
    print("Summary")
    print("=" * 50)

    print("\nUsers:")
    print(f"  Deleted: {GREEN}{stats.users_deleted}{NC}")
    print(f"  Failed:  {RED}{stats.users_failed}{NC}")
    print(f"  Skipped: {stats.users_skipped} (non-test users + {ADMIN_EMAIL})")

    print("\nGroups:")
    print(f"  Deleted: {GREEN}{stats.groups_deleted}{NC}")
    print(f"  Failed:  {RED}{stats.groups_failed}{NC}")
    print(f"  Skipped: {stats.groups_skipped} (non-test groups + {len(BUILTIN_GROUPS)} built-in pseudo-groups)")

    print("\nCATS Artifacts:")
    print(f"  Deleted: {GREEN}{stats.cats_deleted}{NC}")
    print(f"  Failed:  {RED}{stats.cats_failed}{NC}")
    print(f"  Skipped: {stats.cats_skipped} (non-CATS resources)")


def main() -> int:
    """Main entry point."""
    parser = argparse.ArgumentParser(
        description="Delete test users and groups from TMI via the admin API",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  uv run scripts/delete-test-users.py              # Delete all test data and CATS artifacts
  uv run scripts/delete-test-users.py --dry-run    # Show what would be deleted
  uv run scripts/delete-test-users.py --users-only # Delete only test users
  uv run scripts/delete-test-users.py --groups-only # Delete only test groups
  uv run scripts/delete-test-users.py --cats-only  # Delete only CATS-seeded artifacts
        """,
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be deleted without making changes",
    )
    parser.add_argument(
        "--users-only",
        action="store_true",
        help="Only delete test users, skip groups",
    )
    parser.add_argument(
        "--groups-only",
        action="store_true",
        help="Only delete test groups, skip users",
    )
    parser.add_argument(
        "--cats-only",
        action="store_true",
        help="Only delete CATS-seeded artifacts, skip users and groups",
    )

    args = parser.parse_args()

    # Validate arguments
    only_flags = sum([args.users_only, args.groups_only, args.cats_only])
    if only_flags > 1:
        print_error("Cannot specify more than one --*-only flag")
        return 1

    print("=" * 50)
    print("TMI Test User and Group Cleanup Script")
    print("=" * 50)

    if args.dry_run:
        print_warning("\nDRY RUN MODE - No changes will be made\n")

    # Check prerequisites
    print("Checking prerequisites...")

    if not check_server_running():
        print_error(f"TMI server is not running at {API_BASE}")
        print("Start it with: make start-dev")
        return 1
    print_success(f"  TMI server running at {API_BASE}")

    if not check_oauth_stub_running():
        print_error(f"OAuth stub is not running at {OAUTH_STUB}")
        print("Start it with: make start-oauth-stub")
        return 1
    print_success(f"  OAuth stub running at {OAUTH_STUB}")

    # Authenticate
    print()
    token = authenticate_as_charlie()
    if not token:
        return 1

    # Perform cleanup
    stats = Stats()

    if not args.groups_only and not args.cats_only:
        deleted, failed, skipped = cleanup_users(token, args.dry_run)
        stats.users_deleted = deleted
        stats.users_failed = failed
        stats.users_skipped = skipped

    if not args.users_only and not args.cats_only:
        deleted, failed, skipped = cleanup_groups(token, args.dry_run)
        stats.groups_deleted = deleted
        stats.groups_failed = failed
        stats.groups_skipped = skipped

    if not args.users_only and not args.groups_only:
        deleted, failed, skipped = cleanup_cats_artifacts(token, args.dry_run)
        stats.cats_deleted = deleted
        stats.cats_failed = failed
        stats.cats_skipped = skipped

    # Print summary
    print_summary(stats, args.dry_run)

    # Return non-zero if any failures
    if stats.users_failed > 0 or stats.groups_failed > 0 or stats.cats_failed > 0:
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
