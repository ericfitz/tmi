#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["requests>=2.32"]
# ///
"""Read and write system settings on a running TMI server.

Authenticates interactively via a browser OAuth flow (PKCE, RFC 7636) against
any configured provider, then drives GET/PUT/DELETE on /admin/settings/{key}.
Interactive auth is required by design: service-account (client-credentials)
tokens are categorically denied on /admin/* routes, see #399.

Usage:
    scripts/set-server-setting.py list
    scripts/set-server-setting.py get  auth.oauth.providers.google.client_id
    scripts/set-server-setting.py set  timmy.enabled true --type bool
    scripts/set-server-setting.py set  some.key --value-file /path/to/secret
    scripts/set-server-setting.py delete some.key

    # against a remote deployment, with a different provider
    scripts/set-server-setting.py --server https://api.tmi.dev --idp microsoft \
        get auth.oauth.providers.google.client_id

Secrets never go on the command line. Pass a setting's value with --value-file
and an existing bearer token with --token-file, so neither lands in argv, shell
history, or another user's `ps` output.

The browser login requires the callback to carry back the exact `state` this
process issued, and requires an authorization code: a token delivered straight
to the callback is not bound to this flow's PKCE verifier, so it is refused
rather than used.

A caveat this tool cannot work around: settings supplied through the
environment or a config file outrank the database (env > config > DB, see
api/settings_service.go). The server returns 409 for a PUT to any such key,
naming the source. On Kubernetes deployments that inject config via envFrom,
that includes every OAuth provider key -- change those at the source (e.g.
scripts/set-oauth-secret.sh) instead.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import http.server
import json
import secrets
import socket
import sys
import threading
import urllib.parse
import webbrowser
from typing import Any

import requests

DEFAULT_SERVER = "http://localhost:8080"
DEFAULT_IDP = "google"
# Bind the loopback address explicitly rather than the "localhost" name, so the
# listener cannot end up on a non-loopback interface if the name resolves oddly.
CALLBACK_HOST = "127.0.0.1"
AUTH_TIMEOUT_SECONDS = 300


# --- OAuth (PKCE) ------------------------------------------------------------


def _pkce_pair() -> tuple[str, str]:
    """Return (verifier, challenge) for PKCE S256."""
    verifier = secrets.token_urlsafe(64)[:128]
    digest = hashlib.sha256(verifier.encode("ascii")).digest()
    challenge = base64.urlsafe_b64encode(digest).decode("ascii").rstrip("=")
    return verifier, challenge


def _free_port() -> int:
    with socket.socket() as s:
        s.bind((CALLBACK_HOST, 0))
        return s.getsockname()[1]


class _CallbackHandler(http.server.BaseHTTPRequestHandler):
    """Single-shot handler capturing the redirect back from TMI."""

    result: dict[str, str] = {}
    done = threading.Event()

    # The state this flow issued. A callback that does not carry it back is not
    # this flow's callback and is ignored outright, so a forged request cannot
    # even reach the exchange logic.
    expected_state: str = ""

    def do_GET(self) -> None:  # noqa: N802 - stdlib naming
        params = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        flat = {k: v[0] for k, v in params.items() if v}

        # Reject anything not bound to this flow before looking at its contents.
        # secrets.compare_digest over a value we generated ourselves, so the
        # comparison is constant-time and a missing state fails like a wrong one.
        if not secrets.compare_digest(flat.get("state", ""), _CallbackHandler.expected_state):
            self.send_response(400)
            self.end_headers()
            return

        # TMI may hand back either an authorization code or, when it has
        # already completed the exchange itself, the tokens directly.
        if "code" in flat or "access_token" in flat or "error" in flat:
            _CallbackHandler.result = flat
            body = (
                b"<html><body><h2>Authentication complete.</h2>"
                b"<p>You can close this tab and return to the terminal.</p>"
                b"</body></html>"
            )
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            _CallbackHandler.done.set()
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, *_args: Any) -> None:
        """Silence the default stderr access log."""


def authenticate(server: str, idp: str) -> str:
    """Run the browser OAuth flow and return a TMI access token."""
    port = _free_port()
    callback = f"http://{CALLBACK_HOST}:{port}/"
    verifier, challenge = _pkce_pair()
    state = secrets.token_urlsafe(32)

    query = urllib.parse.urlencode(
        {
            "idp": idp,
            "client_callback": callback,
            "state": state,
            "code_challenge": challenge,
            "code_challenge_method": "S256",
        }
    )
    authorize_url = f"{server.rstrip('/')}/oauth2/authorize?{query}"

    _CallbackHandler.result = {}
    _CallbackHandler.done = threading.Event()
    _CallbackHandler.expected_state = state
    httpd = http.server.HTTPServer((CALLBACK_HOST, port), _CallbackHandler)
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()

    print(f"Opening browser to authenticate with '{idp}'...", file=sys.stderr)
    print(f"  If it does not open, visit:\n  {authorize_url}\n", file=sys.stderr)
    webbrowser.open(authorize_url)

    completed = _CallbackHandler.done.wait(timeout=AUTH_TIMEOUT_SECONDS)
    httpd.shutdown()
    if not completed:
        raise SystemExit(f"Timed out after {AUTH_TIMEOUT_SECONDS}s waiting for login.")

    result = _CallbackHandler.result
    if "error" in result:
        raise SystemExit(
            f"Authentication failed: {result['error']} "
            f"{result.get('error_description', '')}".strip()
        )
    # The handler already refused anything whose state did not match, so reaching
    # here means the callback is bound to this flow. Re-check rather than trust
    # that, since the two are far apart in the file.
    if not secrets.compare_digest(result.get("state", ""), state):
        raise SystemExit("State missing or mismatched on OAuth callback -- aborting.")

    # Only accept a token the server minted in exchange for OUR code and
    # verifier. A token arriving in the callback is not bound to the PKCE
    # verifier this process generated, so taking it on trust would let anyone
    # who reaches the listener choose the identity the rest of the run acts as.
    if "code" not in result:
        raise SystemExit(
            "OAuth callback carried no authorization code. Refusing to use a "
            "token handed straight to the callback: it is not bound to this "
            "flow's PKCE verifier."
        )

    resp = requests.post(
        f"{server.rstrip('/')}/oauth2/token",
        params={"idp": idp},
        json={
            "grant_type": "authorization_code",
            "code": result["code"],
            "code_verifier": verifier,
            "redirect_uri": callback,
            "state": state,
        },
        timeout=30,
    )
    if resp.status_code != 200:
        raise SystemExit(f"Token exchange failed: {resp.status_code} {resp.text}")
    token = resp.json().get("access_token")
    if not token:
        raise SystemExit(f"No access_token in token response: {resp.text}")
    return token


# --- Settings API ------------------------------------------------------------


def _explain(resp: requests.Response) -> str:
    """Turn an error response into something an operator can act on."""
    try:
        body = resp.json()
    except ValueError:
        return f"HTTP {resp.status_code}: {resp.text}"
    message = body.get("message") or body.get("error") or json.dumps(body)
    if resp.status_code == 409:
        message += (
            "\n\nThis key is supplied by the environment or a config file, which "
            "outranks the database. Change it at that source instead -- for "
            "Kubernetes OAuth keys, scripts/set-oauth-secret.sh."
        )
    elif resp.status_code == 403:
        message += "\n\nAdmin privileges are required, and /admin/* rejects service-account tokens (#399)."
    return f"HTTP {resp.status_code}: {message}"


def cmd_list(server: str, token: str, args: argparse.Namespace) -> int:
    resp = requests.get(
        f"{server}/admin/settings",
        headers={"Authorization": f"Bearer {token}"},
        timeout=30,
    )
    if resp.status_code != 200:
        print(_explain(resp), file=sys.stderr)
        return 1
    payload = resp.json()
    rows = payload if isinstance(payload, list) else payload.get("settings", payload)
    print(json.dumps(rows, indent=2))
    return 0


def cmd_get(server: str, token: str, args: argparse.Namespace) -> int:
    resp = requests.get(
        f"{server}/admin/settings/{urllib.parse.quote(args.key, safe='')}",
        headers={"Authorization": f"Bearer {token}"},
        timeout=30,
    )
    if resp.status_code != 200:
        print(_explain(resp), file=sys.stderr)
        return 1
    print(json.dumps(resp.json(), indent=2))
    return 0


def cmd_set(server: str, token: str, args: argparse.Namespace) -> int:
    if args.value_file:
        with open(args.value_file, encoding="utf-8") as fh:
            value = fh.read().rstrip("\n")
    elif args.value is not None:
        value = args.value
    else:
        print("error: provide a VALUE or --value-file", file=sys.stderr)
        return 2

    body: dict[str, Any] = {"value": value, "type": args.type}
    if args.description:
        body["description"] = args.description

    resp = requests.put(
        f"{server}/admin/settings/{urllib.parse.quote(args.key, safe='')}",
        headers={"Authorization": f"Bearer {token}"},
        json=body,
        timeout=30,
    )
    if resp.status_code not in (200, 201):
        print(_explain(resp), file=sys.stderr)
        return 1
    # Never echo the value back -- it may be a secret.
    print(f"Set '{args.key}' (type={args.type}, {len(value)} chars). Value not shown.")
    return 0


def cmd_delete(server: str, token: str, args: argparse.Namespace) -> int:
    resp = requests.delete(
        f"{server}/admin/settings/{urllib.parse.quote(args.key, safe='')}",
        headers={"Authorization": f"Bearer {token}"},
        timeout=30,
    )
    if resp.status_code not in (200, 204):
        print(_explain(resp), file=sys.stderr)
        return 1
    print(f"Deleted '{args.key}'.")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Read and write system settings on a running TMI server.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    parser.add_argument("--server", default=DEFAULT_SERVER, help=f"TMI base URL (default: {DEFAULT_SERVER})")
    parser.add_argument("--idp", default=DEFAULT_IDP, help=f"OAuth provider id (default: {DEFAULT_IDP})")
    # A token is a credential, so it is read from a file rather than argv: argv
    # is world-readable via ps and lands in shell history. Same reasoning as
    # --value-file below.
    parser.add_argument(
        "--token-file",
        help="File containing an existing bearer token; skips the browser login",
    )

    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("list", help="List all system settings")

    p_get = sub.add_parser("get", help="Get one setting")
    p_get.add_argument("key")

    p_set = sub.add_parser("set", help="Create or update one setting")
    p_set.add_argument("key")
    p_set.add_argument("value", nargs="?", help="Value (omit when using --value-file)")
    p_set.add_argument("--value-file", help="Read the value from this file (use for secrets)")
    p_set.add_argument("--type", default="string", choices=["string", "int", "bool", "json"])
    p_set.add_argument("--description", help="Human-readable description")

    p_del = sub.add_parser("delete", help="Delete one setting")
    p_del.add_argument("key")

    args = parser.parse_args()
    server = args.server.rstrip("/")

    if args.token_file:
        with open(args.token_file, encoding="utf-8") as fh:
            token = fh.read().strip()
        if not token:
            print(f"error: {args.token_file} is empty", file=sys.stderr)
            return 2
    else:
        token = authenticate(server, args.idp)

    handlers = {
        "list": cmd_list,
        "get": cmd_get,
        "set": cmd_set,
        "delete": cmd_delete,
    }
    return handlers[args.command](server, token, args)


if __name__ == "__main__":
    sys.exit(main())
