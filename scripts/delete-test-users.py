#!/usr/bin/env python3

# /// script
# dependencies = ["requests>=2.32.0"]
# ///

"""
TMI Test Data Cleanup Script

Deletes test users, groups, and test-created artifacts (threat models, intake
surveys, survey responses, and CATS-seeded webhooks/addons/credentials) from the
TMI database via the admin API. Preserves the charlie@tmi.local admin account
(our authenticated identity), non-test users, the built-in pseudo-groups, and
all non-test data.

Test data is identified by these markers (the constants that SET them live in
test/integration/framework/fixtures.go and test/seeds/cats-seed-data.json):
  #1  users whose email is in a synthetic test domain (@tmi.local, @cats.io);
      deleting a user cascades every artifact it owns
  #2  artifact name starts with the CATS seed prefix ("CATS Test")
  #3  artifact description == the integration-framework default
      ("Created by integration test framework")
  #4  artifact description names the CATS/dbtool fuzz seed
      ("...comprehensive API fuzzing...")
  #5  artifact description names an integration test ("...integration test...")

Markers #2-#5 are owner-independent, so they also remove test artifacts owned by
a preserved account (e.g. charlie) that the owner cascade (marker #1) leaves
behind. They are applied to threat models, surveys, and survey responses;
webhooks/addons/client credentials remain matched by the CATS name prefix only
(any test-owned copies are already removed by the owner cascade).

Prerequisites:
    1. TMI server must be running (make dev-up)
    2. OAuth callback stub must be running (make start-oauth-stub)
    3. charlie@tmi.local must exist and be an administrator

Authentication Flow:
    This script performs OAuth 2.0 authentication using the PKCE flow via
    the OAuth callback stub. See scripts/oauth-client-callback-stub.py for details.

API Endpoints Used:
    - GET/DELETE /admin/users, /admin/groups
    - GET/DELETE /threat_models              (cascades sub-resources)
    - GET/DELETE /admin/surveys   (DELETE uses ?force=true to cascade responses)
    - GET/DELETE /intake/survey_responses
    - GET/DELETE /admin/webhooks/subscriptions, /addons, /me/client_credentials

Usage:
    uv run scripts/delete-test-users.py
    uv run scripts/delete-test-users.py --dry-run
    uv run scripts/delete-test-users.py --users-only
    uv run scripts/delete-test-users.py --groups-only
    uv run scripts/delete-test-users.py --artifacts-only   (alias: --cats-only)
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

# Marker #1: synthetic email domains used exclusively by test tooling. A user in
# one of these domains is a test user; deleting it cascades every artifact it
# owns. @tmi.local is the TMI OAuth provider's test domain; @cats.io is used by
# CATS fuzz identities. ADMIN_EMAIL is always preserved (see is_test_user).
TEST_EMAIL_DOMAINS = ("@tmi.local", "@cats.io")

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


# Content markers (owner-independent), set by TMI test tooling:
CATS_NAME_PREFIX = "CATS Test"  # marker #2 (name prefix; cats-seed-data.json)
# marker #3 — integration framework default description (fixtures.go), matched exactly:
INTEGRATION_FRAMEWORK_DESC = "created by integration test framework"
# marker #4 — CATS/dbtool fuzz seed description (cats-seed-data.json), matched as substring:
FUZZING_DESC_MARKER = "comprehensive api fuzzing"
# marker #5 — any integration-test description (superset of #3), matched as substring:
INTEGRATION_DESC_MARKER = "integration test"


@dataclass
class Stats:
    """Track deletion statistics."""

    users_deleted: int = 0
    users_failed: int = 0
    users_skipped: int = 0
    groups_deleted: int = 0
    groups_failed: int = 0
    groups_skipped: int = 0
    artifacts_deleted: int = 0
    artifacts_failed: int = 0
    artifacts_skipped: int = 0


def print_error(msg: str) -> None:
    """Print error message in red."""
    print(f"{RED}Error: {msg}{NC}", file=sys.stderr)


def print_success(msg: str) -> None:
    """Print success message in green."""
    print(f"{GREEN}{msg}{NC}")


def print_warning(msg: str) -> None:
    """Print warning message in yellow."""
    print(f"{YELLOW}{msg}{NC}")


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
        # Stub returns various codes, just check it responds
        requests.get(f"{OAUTH_STUB}/", timeout=5)
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
    Determine if a user is a test user that should be deleted (marker #1).

    Test users are identified by a synthetic test email domain (@tmi.local or
    @cats.io). The charlie@tmi.local admin account (our authenticated identity)
    is always preserved. Real OAuth-provider users (github/google/etc.) and
    synthetic system users (e.g. operator@tmi.system) are left untouched.
    """
    email = user.get("email", "")

    # Test users live in a synthetic test email domain
    if not email.endswith(TEST_EMAIL_DOMAINS):
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
    """Check if an item is a CATS-seeded artifact by name prefix (marker #2)."""
    name = item.get(name_field, "")
    return name.startswith(CATS_NAME_PREFIX)


def is_test_artifact(item: dict, name_field: str = "name") -> bool:
    """Check if an item was created by TMI test tooling (markers #2-#5).

    Owner-independent, so it catches test artifacts owned by a preserved account
    (e.g. charlie) that the owner cascade (marker #1) leaves behind. Markers:
      #2  name starts with the CATS seed prefix ("CATS Test")
      #3  description == the integration-framework default
      #4  description names the CATS/dbtool fuzz seed ("comprehensive API fuzzing")
      #5  description names an integration test (superset of #3)
    #3 is retained explicitly for traceability even though #5 subsumes it.
    """
    name = item.get(name_field) or ""
    desc = (item.get("description") or "").lower()
    if name.startswith(CATS_NAME_PREFIX):          # marker #2
        return True
    if desc == INTEGRATION_FRAMEWORK_DESC:         # marker #3
        return True
    if FUZZING_DESC_MARKER in desc:                # marker #4
        return True
    if INTEGRATION_DESC_MARKER in desc:            # marker #5
        return True
    return False


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
    token: str, endpoint: str, resource_id: str, label: str,
    dry_run: bool = False, params: dict | None = None
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
            params=params,
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


def cleanup_test_artifacts(
    token: str, dry_run: bool = False
) -> tuple[int, int, int]:
    """
    Delete test-created artifacts that survived the owner cascade.

    Threat models, surveys, and survey responses are matched by the full test
    markers (#2-#5, plus parent-survey membership for responses); webhooks,
    addons, and client credentials are matched by the CATS name prefix only.

    Deletion order matters due to dependencies:
    1. Addons (depend on webhooks and threat models)
    2. Client credentials
    3. Survey responses (children — deleted before their parent surveys)
    4. Surveys
    5. Webhooks
    6. Threat models (cascade deletes sub-resources)

    Returns tuple of (deleted, failed, skipped) counts.
    """
    print("\nCleaning up test artifacts...")
    deleted = 0
    failed = 0
    skipped = 0

    # Pre-pass: identify test surveys (by markers #2-#5) up front so survey
    # RESPONSES — which must be deleted BEFORE their parent surveys (FK child
    # first) — can be matched to their parent even though surveys are processed
    # later in the loop. (Previously survey responses were matched against an
    # empty set because surveys were collected after them, so none were caught.)
    test_survey_ids: set[str] = {
        s.get("id", "")
        for s in fetch_paginated(token, "/admin/surveys", "surveys")
        if is_test_artifact(s)
    }

    # Define resource types in dependency order. `match` selects the predicate:
    #   "markers"  -> is_test_artifact (markers #2-#5)
    #   "response" -> parent survey in test_survey_ids OR survey_name marker
    #   "cats"     -> is_cats_artifact (CATS name prefix only)
    resource_types = [
        {"name": "addons", "endpoint": "/addons", "items_key": "addons",
         "name_field": "name", "match": "cats"},
        {"name": "client credentials", "endpoint": "/me/client_credentials",
         "items_key": "credentials", "name_field": "name", "match": "cats"},
        {"name": "survey responses", "endpoint": "/intake/survey_responses",
         "items_key": "survey_responses", "name_field": "survey_name",
         "match": "response"},
        {"name": "surveys", "endpoint": "/admin/surveys", "items_key": "surveys",
         "name_field": "name", "match": "markers", "params": {"force": "true"}},
        {"name": "webhooks", "endpoint": "/admin/webhooks/subscriptions",
         "items_key": "subscriptions", "name_field": "name", "match": "cats"},
        {"name": "threat models", "endpoint": "/threat_models",
         "items_key": "threat_models", "name_field": "name", "match": "markers"},
    ]

    for rt in resource_types:
        print(f"\n  Fetching {rt['name']}...")
        items = fetch_paginated(token, rt["endpoint"], rt["items_key"])

        test_items = []
        for item in items:
            if rt["match"] == "response":
                # A response is test data if its parent survey is a test survey,
                # or its (denormalized) survey_name carries a name marker.
                matched = (
                    item.get("survey_id", "") in test_survey_ids
                    or is_test_artifact(item, "survey_name")
                )
            elif rt["match"] == "markers":
                matched = is_test_artifact(item, rt["name_field"])
            else:  # "cats"
                matched = is_cats_artifact(item, rt["name_field"])

            if matched:
                test_items.append(item)
            else:
                skipped += 1

        if not test_items:
            print(f"  No test {rt['name']} found")
            continue

        print(f"  Found {len(test_items)} test {rt['name']} to delete")

        for item in test_items:
            item_id = item.get("id", "")
            item_label = item.get(rt["name_field"], item_id)
            if rt["match"] == "response":
                item_label = f"survey response {item_id[:8]}..."
            label = f"{rt['name'].rstrip('s')}: {item_label}"
            if delete_resource(token, rt["endpoint"], item_id, label, dry_run,
                               params=rt.get("params")):
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

    print("\nTest Artifacts (threat models, surveys, responses, webhooks, addons, credentials):")
    print(f"  Deleted: {GREEN}{stats.artifacts_deleted}{NC}")
    print(f"  Failed:  {RED}{stats.artifacts_failed}{NC}")
    print(f"  Skipped: {stats.artifacts_skipped} (non-test resources)")


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
  uv run scripts/delete-test-users.py --artifacts-only # Delete only test artifacts (alias: --cats-only)
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
        "--artifacts-only",
        "--cats-only",
        dest="artifacts_only",
        action="store_true",
        help="Only delete test artifacts (threat models, surveys, responses, "
             "CATS webhooks/addons/credentials), skip users and groups",
    )

    args = parser.parse_args()

    # Validate arguments
    only_flags = sum([args.users_only, args.groups_only, args.artifacts_only])
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

    if not args.groups_only and not args.artifacts_only:
        deleted, failed, skipped = cleanup_users(token, args.dry_run)
        stats.users_deleted = deleted
        stats.users_failed = failed
        stats.users_skipped = skipped

    if not args.users_only and not args.artifacts_only:
        deleted, failed, skipped = cleanup_groups(token, args.dry_run)
        stats.groups_deleted = deleted
        stats.groups_failed = failed
        stats.groups_skipped = skipped

    if not args.users_only and not args.groups_only:
        deleted, failed, skipped = cleanup_test_artifacts(token, args.dry_run)
        stats.artifacts_deleted = deleted
        stats.artifacts_failed = failed
        stats.artifacts_skipped = skipped

    # Print summary
    print_summary(stats, args.dry_run)

    # Return non-zero if any failures
    if stats.users_failed > 0 or stats.groups_failed > 0 or stats.artifacts_failed > 0:
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
