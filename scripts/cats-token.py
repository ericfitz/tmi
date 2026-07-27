#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""CATS `token` hook: authenticate via the OAuth stub and print an access token.

Extracted from the OAuth flow in the former run-cats-fuzz.py so the cats
plugin can invoke it as an opaque `token` hook command. The plugin runs
this script, captures stdout, strips it, and uses the result as a bearer
token — so stdout MUST contain the token and nothing else. Every log line
is written to stderr instead.

Usage:
    uv run scripts/cats-token.py
    uv run scripts/cats-token.py --user alice --server http://localhost:8080
"""

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

# ---------------------------------------------------------------------------
# Import shared helpers from scripts/lib/tmi_common.py
# ---------------------------------------------------------------------------
sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from tmi_common import ensure_oauth_stub, log_error  # noqa: E402

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

OAUTH_STUB_PORT = 8079
OAUTH_STUB_URL = f"http://localhost:{OAUTH_STUB_PORT}"


# ---------------------------------------------------------------------------
# stderr-only logging
#
# tmi_common.log_info/log_success print to stdout by design (it's the right
# default for interactive scripts). This hook can't use them directly since
# stdout is reserved for the token; these local wrappers write the same
# messages to stderr instead. log_error already targets stderr, so it's
# imported as-is.
# ---------------------------------------------------------------------------


def log_info(msg: str) -> None:
    """Print an informational message to stderr (stdout is reserved for the token)."""
    print(f"[INFO] {msg}", file=sys.stderr, flush=True)


def log_success(msg: str) -> None:
    """Print a success message to stderr (stdout is reserved for the token)."""
    print(f"[SUCCESS] {msg}", file=sys.stderr, flush=True)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Authenticate via the OAuth stub and print an access token "
        "(cats plugin `token` hook).",
    )
    parser.add_argument(
        "--user",
        metavar="USER",
        default="charlie",
        help="OAuth user login hint (default: charlie)",
    )
    parser.add_argument(
        "--server",
        metavar="URL",
        default="http://localhost:8080",
        help="TMI server URL (default: http://localhost:8080)",
    )
    parser.add_argument(
        "--idp",
        metavar="IDP",
        default="tmi",
        help="OAuth identity provider (default: tmi)",
    )
    return parser.parse_args()


# ---------------------------------------------------------------------------
# OAuth authentication
# ---------------------------------------------------------------------------


def authenticate_user(user: str, server: str, idp: str) -> str:
    """Authenticate via the OAuth stub's automated flow and return an access token."""
    log_info(f"Authenticating user: {user}")

    # Start the automated OAuth flow
    start_url = f"{OAUTH_STUB_URL}/flows/start"
    flow_body = json.dumps({
        "userid": user,
        "idp": idp,
        "tmi_server": server,
    }).encode()

    req = urllib.request.Request(
        start_url,
        data=flow_body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:  # noqa: S310
            start_data = json.loads(resp.read())
    except (urllib.error.URLError, OSError) as exc:
        log_error(f"Failed to start OAuth flow: {exc}")
        sys.exit(1)

    flow_id = start_data.get("flow_id")
    if not flow_id:
        log_error(f"No flow_id in response: {start_data}")
        sys.exit(1)

    log_info(f"Flow started with ID: {flow_id}")

    # Poll for completion
    poll_url = f"{OAUTH_STUB_URL}/flows/{flow_id}"
    max_attempts = 10
    poll_data: dict = {}

    for attempt in range(1, max_attempts + 1):
        log_info(f"Polling flow status (attempt {attempt}/{max_attempts})...")
        try:
            with urllib.request.urlopen(poll_url, timeout=10) as resp:  # noqa: S310
                poll_data = json.loads(resp.read())
        except (urllib.error.URLError, OSError) as exc:
            log_error(f"Failed to poll flow status: {exc}")
            sys.exit(1)

        if poll_data.get("tokens_ready") is True:
            log_info("Flow completed successfully")
            break

        status = poll_data.get("status", "")
        if status in ("error", "failed"):
            log_error(f"Flow failed: {poll_data.get('error', 'Unknown error')}")
            sys.exit(1)

        time.sleep(2)
    else:
        log_error(f"Flow did not complete within {max_attempts} attempts")
        log_error(f"Last status: {poll_data.get('status')}")
        sys.exit(1)

    token: str | None = (poll_data.get("tokens") or {}).get("access_token")
    if not token:
        log_error(f"No access token in flow response: {poll_data}")
        sys.exit(1)

    log_success(f"Authentication successful for user: {user}")
    return str(token)


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main() -> None:
    args = parse_args()

    # ensure_oauth_stub() may shell out to manage-oauth-stub.py on a cold
    # start (tmi_common.run_cmd's `capture` defaults to False there), so that
    # child process inherits fd 1 directly and prints several lines
    # (log_info/log_success go to stdout by default) straight past any
    # Python-level redirect. contextlib.redirect_stdout only rebinds the
    # `sys.stdout` object, not fd 1 itself — confirmed empirically that a
    # subprocess still writes to the real stdout underneath it — so it
    # cannot stop this leak. Redirect at the fd level instead: point fd 1 at
    # fd 2 for the rest of the process (every writer, Python or subprocess,
    # now lands on stderr), and write the token directly to the saved real
    # stdout fd at the end.
    token_fd = os.dup(1)
    os.dup2(2, 1)

    ensure_oauth_stub(OAUTH_STUB_PORT)
    token = authenticate_user(args.user, args.server, args.idp)

    os.write(token_fd, (token + "\n").encode())
    os.close(token_fd)


if __name__ == "__main__":
    main()
