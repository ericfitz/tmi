# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "requests>=2.31",
# ]
# ///
"""
Add a user to the TMI administrators group.

Steps:
1. Start the OAuth client callback stub (if not running)
2. Authenticate as charlie via the TMI OAuth provider
3. Retrieve Charlie's JWT
4. Look up the target user by provider and email
5. Add the target user to the administrators group
"""

import subprocess
import sys
import time

import requests

TMI_BASE = "http://localhost:8080"
STUB_BASE = "http://localhost:8079"
LOGIN_USER = "charlie"
TARGET_PROVIDER = "google"
TARGET_EMAIL = "hobobarbarian@gmail.com"
ADMIN_GROUP_UUID = "00000000-0000-0000-0000-000000000002"


def wait_for_service(url: str, name: str, timeout: int = 15) -> bool:
    """Wait for a service to become available."""
    print(f"Waiting for {name} at {url}...")
    for i in range(timeout):
        try:
            r = requests.get(url, timeout=2)
            if r.status_code < 500:
                print(f"  {name} is ready")
                return True
        except requests.ConnectionError:
            pass
        time.sleep(1)
    print(f"  ERROR: {name} not available after {timeout}s")
    return False


def start_oauth_stub() -> bool:
    """Start the OAuth client callback stub via make target."""
    # Check if already running
    try:
        r = requests.get(STUB_BASE, timeout=2)
        if r.status_code < 500:
            print("OAuth stub already running")
            return True
    except requests.ConnectionError:
        pass

    print("Starting OAuth client callback stub...")
    subprocess.run(
        ["make", "start-oauth-stub"],
        cwd="/Users/efitz/Projects/tmi",
        capture_output=True,
        timeout=10,
    )
    return wait_for_service(STUB_BASE, "OAuth stub")


def login_as(user: str) -> str:
    """Authenticate via TMI OAuth provider and retrieve JWT."""
    print(f"\nAuthenticating as {user}...")

    # Start the automated flow via the stub
    r = requests.post(
        f"{STUB_BASE}/flows/start",
        json={"userid": user},
        timeout=10,
    )
    if r.status_code != 200:
        print(f"  ERROR starting flow: {r.status_code} {r.text}")
        sys.exit(1)

    flow = r.json()
    flow_id = flow.get("flow_id")
    print(f"  Flow started: {flow_id}")

    # Poll for completion
    for _ in range(30):
        time.sleep(1)
        r = requests.get(f"{STUB_BASE}/flows/{flow_id}", timeout=5)
        if r.status_code == 200:
            data = r.json()
            if data.get("tokens_ready") or data.get("status") == "authorization_completed":
                print(f"  Flow complete (status={data.get('status')})")
                break
    else:
        print("  ERROR: Flow did not complete in time")
        sys.exit(1)

    # Try to get token from flow response first, then fall back to creds endpoint
    token = None
    if data.get("tokens"):
        token = data["tokens"].get("access_token")

    if not token:
        r = requests.get(f"{STUB_BASE}/creds?userid={user}", timeout=5)
        if r.status_code != 200:
            print(f"  ERROR getting creds: {r.status_code} {r.text}")
            sys.exit(1)
        token = r.json().get("access_token")

    if not token:
        print("  ERROR: No access_token found")
        sys.exit(1)

    print(f"  Got JWT for {user} ({len(token)} chars)")
    return token


def lookup_user(token: str, provider: str, email: str) -> str:
    """Look up a user by provider and email, return their internal_uuid."""
    print(f"\nLooking up user: provider={provider}, email={email}")
    r = requests.get(
        f"{TMI_BASE}/admin/users",
        params={"provider": provider, "email": email},
        headers={"Authorization": f"Bearer {token}"},
        timeout=10,
    )
    if r.status_code != 200:
        print(f"  ERROR: {r.status_code} {r.text}")
        sys.exit(1)

    data = r.json()
    users = data.get("users", [])
    if not users:
        print(f"  ERROR: No user found with provider={provider} email={email}")
        sys.exit(1)

    user = users[0]
    uuid = user.get("internal_uuid")
    name = user.get("name", user.get("display_name", "unknown"))
    print(f"  Found: {name} (uuid={uuid})")
    return uuid


def add_to_administrators(token: str, user_uuid: str) -> None:
    """Add a user to the administrators group."""
    print(f"\nAdding user {user_uuid} to administrators group...")
    r = requests.post(
        f"{TMI_BASE}/admin/groups/{ADMIN_GROUP_UUID}/members",
        json={
            "user_internal_uuid": user_uuid,
            "subject_type": "user",
        },
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
        timeout=10,
    )
    if r.status_code == 201:
        member = r.json()
        print(f"  SUCCESS: Added to administrators group")
        print(f"    Member ID: {member.get('id')}")
        print(f"    User: {member.get('user_email', 'N/A')} ({member.get('user_name', 'N/A')})")
        print(f"    Added by: {member.get('added_by_email', 'N/A')}")
    elif r.status_code == 409:
        print(f"  User is already a member of the administrators group")
    else:
        print(f"  ERROR: {r.status_code} {r.text}")
        sys.exit(1)


def main():
    # Verify TMI server is running
    if not wait_for_service(f"{TMI_BASE}/", "TMI server"):
        print("\nTMI server is not running. Start it with: make start-dev")
        sys.exit(1)

    # Start OAuth stub
    if not start_oauth_stub():
        sys.exit(1)

    # Login as charlie
    token = login_as(LOGIN_USER)

    # Look up the target user
    user_uuid = lookup_user(token, TARGET_PROVIDER, TARGET_EMAIL)

    # Add to administrators
    add_to_administrators(token, user_uuid)

    print("\nDone!")


if __name__ == "__main__":
    main()
