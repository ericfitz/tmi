#!/usr/bin/env python3
"""
TMI Heroku Environment Configuration Script

Automated configuration of Heroku environment variables for TMI server and client applications.
Uses uv for automatic dependency and virtual environment management.

Usage:
    uv run scripts/setup-heroku-env.py                          # Interactive mode
    uv run scripts/setup-heroku-env.py --dry-run                # Preview changes
    uv run scripts/setup-heroku-env.py --server-app tmi-server  # Specify server app
    uv run scripts/setup-heroku-env.py --help                   # Show help

Dependencies are automatically managed by uv using PEP 723 inline script metadata.
"""

# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "rich>=13.0.0",
# ]
# ///

import json
import subprocess
import sys
import argparse
import re
from getpass import getpass
from urllib.parse import urlparse
from typing import Optional, Dict, List, Tuple

from rich.console import Console  # pyright: ignore[reportMissingImports]  # ty:ignore[unresolved-import]
from rich.prompt import Prompt, Confirm  # pyright: ignore[reportMissingImports]  # ty:ignore[unresolved-import]
from rich.panel import Panel  # pyright: ignore[reportMissingImports]  # ty:ignore[unresolved-import]

console = Console()


def run_command(
    cmd: List[str], capture_output: bool = True, check: bool = True
) -> subprocess.CompletedProcess:
    """Run a shell command and return the result."""
    try:
        result = subprocess.run(
            cmd, capture_output=capture_output, text=True, check=check
        )
        return result
    except subprocess.CalledProcessError as e:
        console.print(f"[red]Error running command: {' '.join(cmd)}[/red]")
        console.print(f"[red]{e.stderr}[/red]")
        raise
    except FileNotFoundError:
        console.print(f"[red]Command not found: {cmd[0]}[/red]")
        raise


def check_heroku_cli() -> bool:
    """Check if Heroku CLI is installed and user is authenticated."""
    console.print("\n[bold]🔍 Checking Heroku CLI...[/bold]")

    # Check if heroku command exists
    try:
        run_command(["heroku", "--version"])
    except (subprocess.CalledProcessError, FileNotFoundError):
        console.print("[red]✗ Heroku CLI not found[/red]")
        console.print("\n[yellow]Please install Heroku CLI:[/yellow]")
        console.print("  https://devcenter.heroku.com/articles/heroku-cli")
        return False

    console.print("[green]✓ Heroku CLI installed[/green]")

    # Check authentication
    try:
        result = run_command(["heroku", "whoami"])
        user = result.stdout.strip()
        console.print(f"[green]✓ Authenticated as: {user}[/green]")
        return True
    except subprocess.CalledProcessError:
        console.print("[red]✗ Not authenticated[/red]")
        console.print("\n[yellow]Please login to Heroku:[/yellow]")
        console.print("  heroku login")
        return False


def list_heroku_apps() -> List[Tuple[str, str]]:
    """Get list of available Heroku apps with their URLs."""
    result = run_command(["heroku", "apps", "--json"])
    apps = json.loads(result.stdout)

    app_list = []
    for app in apps:
        name = app.get("name", "")
        web_url = app.get("web_url", f"https://{name}.herokuapp.com")
        app_list.append((name, web_url))

    return sorted(app_list)


def select_or_create_app(
    role: str, non_interactive: bool = False, app_name: Optional[str] = None
) -> Tuple[Optional[str], Optional[str]]:
    """
    Select or create a Heroku app.

    Args:
        role: "server" or "client"
        non_interactive: If True, skip prompts
        app_name: Pre-specified app name

    Returns:
        Tuple of (app_name, app_url) or (None, None) if skipped
    """
    role_display = (
        "Server (TMI API Backend)" if role == "server" else "Client (Frontend/UX)"
    )

    if non_interactive and not app_name:
        console.print(
            f"[yellow]⚠ {role_display}: No app specified in non-interactive mode[/yellow]"
        )
        return None, None

    if app_name:
        # Verify app exists
        try:
            result = run_command(["heroku", "apps:info", "--app", app_name, "--json"])
            app_info = json.loads(result.stdout)
            app_url = app_info.get("web_url", f"https://{app_name}.herokuapp.com")
            console.print(f"[green]✓ {role_display}: {app_name} ({app_url})[/green]")
            return app_name, app_url
        except subprocess.CalledProcessError:
            console.print(f"[red]✗ App '{app_name}' not found or not accessible[/red]")
            if non_interactive:
                return None, None

    # Interactive mode
    console.print(f"\n[bold]{role_display}:[/bold]")

    apps = list_heroku_apps()
    if apps:
        console.print("\n[cyan]Available Heroku Apps:[/cyan]")
        for idx, (name, url) in enumerate(apps, 1):
            console.print(f"  {idx}. {name} ({url})")
        console.print()

    if role == "client":
        prompt_text = "Select from list, type new app name, or 'skip'"
    else:
        prompt_text = "Select from list or type new app name"

    choice = Prompt.ask(
        f"  → {prompt_text}", default="skip" if role == "client" else ""
    )

    if choice.lower() == "skip":
        console.print(f"[yellow]⊘ Skipped {role} app selection[/yellow]")
        return None, None

    # Check if numeric selection
    if choice.isdigit():
        idx = int(choice) - 1
        if 0 <= idx < len(apps):
            selected_name, selected_url = apps[idx]
            console.print(f"[green]✓ Selected: {selected_name}[/green]")
            return selected_name, selected_url

    # Treat as app name
    chosen_name: str = choice

    # Check if app exists
    try:
        result = run_command(["heroku", "apps:info", "--app", chosen_name, "--json"])
        app_info = json.loads(result.stdout)
        app_url = app_info.get("web_url", f"https://{chosen_name}.herokuapp.com")
        console.print(f"[green]✓ Found: {chosen_name} ({app_url})[/green]")
        return chosen_name, app_url
    except subprocess.CalledProcessError:
        # App doesn't exist, offer to create
        if Confirm.ask(f"App '{chosen_name}' doesn't exist. Create it?"):
            console.print(f"[yellow]Creating app '{chosen_name}'...[/yellow]")
            try:
                run_command(["heroku", "create", chosen_name])
                app_url = f"https://{chosen_name}.herokuapp.com"
                console.print(f"[green]✓ Created: {chosen_name}[/green]")
                return chosen_name, app_url
            except subprocess.CalledProcessError as e:
                console.print(f"[red]✗ Failed to create app: {e}[/red]")
                return None, None
        else:
            return None, None


def detect_addons(app_name: str) -> Dict[str, bool]:
    """Detect which addons are provisioned for the app."""
    try:
        result = run_command(["heroku", "addons", "--app", app_name, "--json"])
        addons = json.loads(result.stdout)

        has_postgres = any(
            "postgresql" in addon.get("addon_service", {}).get("name", "").lower()
            for addon in addons
        )
        has_redis = any(
            "redis" in addon.get("addon_service", {}).get("name", "").lower()
            for addon in addons
        )

        return {"postgres": has_postgres, "redis": has_redis}
    except subprocess.CalledProcessError:
        return {"postgres": False, "redis": False}


def extract_postgres_credentials(app_name: str) -> Optional[Dict[str, str]]:
    """Extract PostgreSQL credentials from Heroku Postgres addon.

    Returns DATABASE_URL (preferred 12-factor app pattern) and legacy individual vars.
    TMI supports both patterns - DATABASE_URL takes precedence when set.
    """
    try:
        result = run_command(
            ["heroku", "pg:credentials:url", "DATABASE", "--app", app_name]
        )
        output = result.stdout

        # Parse connection string from output
        # Format: postgres://user:password@host:port/database
        match = re.search(r"(postgres://[^:]+:[^@]+@[^:]+:\d+/[^\s]+)", output)
        if match:
            database_url = match.group(1)
            # Add sslmode=require for Heroku
            if "?" in database_url:
                database_url += "&sslmode=require"
            else:
                database_url += "?sslmode=require"

            # Return DATABASE_URL as the primary configuration
            # TMI now prefers DATABASE_URL over individual vars (12-factor app pattern)
            return {
                "TMI_DATABASE_URL": database_url,
                # Legacy vars (deprecated) - TMI will use DATABASE_URL if set
                # These are kept for backward compatibility
            }
    except subprocess.CalledProcessError:
        pass

    return None


def extract_redis_credentials(app_name: str) -> Optional[Dict[str, str]]:
    """Extract Redis credentials from Heroku Redis addon.

    Returns TMI_ prefixed variables (new standard).
    """
    try:
        result = run_command(
            ["heroku", "redis:credentials", "--app", app_name, "--json"]
        )
        creds: Dict = json.loads(result.stdout)

        if isinstance(creds, list) and len(creds) > 0:
            creds = creds[0]

        host: str = creds.get("host", "")
        port: str = str(creds.get("port", "6379"))
        password: str = creds.get("password", "")

        # Use TMI_ prefixed variables (new standard)
        return {
            "TMI_REDIS_HOST": host,
            "TMI_REDIS_PORT": str(port),
            "TMI_REDIS_PASSWORD": password,
        }
    except (subprocess.CalledProcessError, json.JSONDecodeError, KeyError):
        pass

    return None


def generate_jwt_secret() -> str:
    """Generate a secure random JWT secret."""
    result = run_command(["openssl", "rand", "-base64", "32"])
    return result.stdout.strip()


def get_existing_config(app_name: str, key: str) -> Optional[str]:
    """Get existing config variable value."""
    try:
        result = run_command(["heroku", "config:get", key, "--app", app_name])
        value = result.stdout.strip()
        return value if value else None
    except subprocess.CalledProcessError:
        return None


def prompt_oauth_provider(
    provider_name: str, provider_display: str, skip_oauth: bool
) -> Optional[Dict[str, str]]:
    """Prompt for OAuth provider configuration."""
    if skip_oauth:
        return None

    console.print(f"\n[bold cyan]OAuth Provider: {provider_display}[/bold cyan]")

    choice = Prompt.ask(
        "  Enable this provider?", choices=["y", "n", "skip"], default="skip"
    )

    if choice == "skip" or choice == "n":
        return None

    client_id = Prompt.ask(f"  {provider_display} Client ID")
    client_secret = getpass(f"  {provider_display} Client Secret (hidden): ")

    return {
        f"OAUTH_PROVIDERS_{provider_name.upper()}_ENABLED": "true",
        f"OAUTH_PROVIDERS_{provider_name.upper()}_CLIENT_ID": client_id,
        f"OAUTH_PROVIDERS_{provider_name.upper()}_CLIENT_SECRET": client_secret,
    }


def set_config_vars(
    app_name: str, config_vars: Dict[str, str], dry_run: bool = False
) -> bool:
    """Apply configuration variables to Heroku app."""
    if not config_vars:
        console.print("[yellow]⚠ No configuration variables to set[/yellow]")
        return True

    # Build heroku config:set command
    cmd = ["heroku", "config:set"]
    for key, value in config_vars.items():
        cmd.append(f"{key}={value}")
    cmd.extend(["--app", app_name])

    if dry_run:
        console.print(
            "\n[bold yellow]🔍 DRY RUN MODE - Command that would be executed:[/bold yellow]"
        )
        console.print()

        # Display with redacted secrets
        display_cmd = ["heroku", "config:set"]
        for key, value in config_vars.items():
            if any(secret in key.upper() for secret in ["SECRET", "PASSWORD", "TOKEN"]):
                display_cmd.append(f"{key}=***REDACTED***")
            else:
                display_cmd.append(f"{key}={value}")
        display_cmd.extend(["--app", app_name])

        console.print("  " + " \\\n    ".join(display_cmd))
        console.print()
        return True

    # Apply configuration
    console.print(f"\n[bold]⚙️  Applying configuration to {app_name}...[/bold]")
    try:
        run_command(cmd, capture_output=False)
        console.print("[green]✓ Configuration applied successfully[/green]")
        return True
    except subprocess.CalledProcessError:
        console.print("[red]✗ Failed to apply configuration[/red]")
        return False


def display_summary(app_name: str, config_vars: Dict[str, str]):
    """Display configuration summary with redacted secrets."""
    console.print(f"\n[bold]📋 Configuration Summary for {app_name}[/bold]")
    console.print("=" * 60)

    # Group by category - support both TMI_ prefixed and legacy vars
    categories = {
        "🗄️  Database": ["TMI_DATABASE_", "DATABASE_URL", "POSTGRES_", "TMI_REDIS_", "REDIS_"],
        "🔐 Authentication": ["TMI_JWT_", "JWT_", "TMI_OAUTH_", "OAUTH_"],
        "🌐 WebSocket": ["TMI_WEBSOCKET_", "WEBSOCKET_"],
        "⚙️  Server": ["TMI_SERVER_", "SERVER_", "TMI_LOG_", "LOGGING_"],
        "📇 Operator": ["TMI_OPERATOR_", "OPERATOR_"],
    }

    for category, prefixes in categories.items():
        matching_vars = {
            k: v
            for k, v in config_vars.items()
            if any(k.startswith(p) for p in prefixes)
        }
        if matching_vars:
            console.print(f"\n[bold]{category}[/bold]")
            for key, value in sorted(matching_vars.items()):
                if any(
                    secret in key.upper() for secret in ["SECRET", "PASSWORD", "TOKEN"]
                ):
                    display_value = "********"
                else:
                    display_value = value[:50] + "..." if len(value) > 50 else value
                console.print(f"  {key}: {display_value}")

    console.print(f"\n[green]✅ Total: {len(config_vars)} variables configured[/green]")
    console.print("=" * 60)


def display_next_steps(server_app: str, server_url: str, client_url: Optional[str]):
    """Display next steps after configuration."""
    console.print("\n[bold green]🚀 Next Steps:[/bold green]\n")

    console.print("[bold]1. Deploy your server application:[/bold]")
    console.print("   git push heroku main")
    console.print(
        "   [dim]Note: Database migrations run automatically on deployment via release phase[/dim]\n"
    )

    console.print("[bold]2. Monitor deployment:[/bold]")
    console.print(f"   heroku logs --tail --app {server_app}\n")

    console.print("[bold]3. Test your deployment:[/bold]")
    console.print(f"   curl {server_url}version\n")

    console.print("[bold]4. Test WebSocket connectivity:[/bold]")
    console.print(
        f'   wscat -c "wss://{urlparse(server_url).netloc}/ws/diagrams/{{id}}" \\'
    )
    console.print('     -H "Authorization: Bearer YOUR_JWT_TOKEN"\n')

    if client_url:
        console.print("[bold]5. Configure your client app to use:[/bold]")
        console.print(f"   API URL: {server_url}")
        console.print(f"   WebSocket URL: wss://{urlparse(server_url).netloc}\n")

    console.print("[bold]📚 Documentation:[/bold]")
    console.print("   docs/operator/deployment/heroku-deployment.md\n")


def main():
    parser = argparse.ArgumentParser(
        description="Configure Heroku environment variables for TMI server and client apps",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--server-app", help="Server app name (skip selection)")
    parser.add_argument("--client-app", help="Client app name (skip selection)")
    parser.add_argument(
        "--no-client-app", action="store_true", help="Skip client app configuration"
    )
    parser.add_argument(
        "--websocket-origins",
        help="Override WebSocket allowed origins (comma-separated)",
    )
    parser.add_argument(
        "--dry-run", action="store_true", help="Show commands without executing"
    )
    parser.add_argument(
        "--non-interactive", action="store_true", help="Batch mode, no prompts"
    )
    parser.add_argument(
        "--skip-oauth", action="store_true", help="Skip OAuth provider configuration"
    )
    parser.add_argument(
        "--skip-addons", action="store_true", help="Don't provision missing addons"
    )

    args = parser.parse_args()

    # Display header
    console.print(
        Panel.fit(
            "[bold cyan]🎯 TMI Heroku Configuration Setup[/bold cyan]",
            border_style="cyan",
        )
    )

    # Check prerequisites
    if not check_heroku_cli():
        sys.exit(1)

    # Phase 1: App Selection
    console.print("\n[bold]📱 STEP 1: Select Applications[/bold]")
    console.print("=" * 60)

    server_app, server_url = select_or_create_app(
        "server", non_interactive=args.non_interactive, app_name=args.server_app
    )

    if not server_app or not server_url:
        console.print("[red]✗ Server app is required[/red]")
        sys.exit(1)

    # Type narrowing: server_app and server_url are now guaranteed to be str
    assert server_app is not None
    assert server_url is not None

    client_app, client_url = None, None
    if not args.no_client_app:
        client_app, client_url = select_or_create_app(
            "client", non_interactive=args.non_interactive, app_name=args.client_app
        )

    # Auto-configure URLs
    oauth_callback_url = f"{server_url.rstrip('/')}/oauth2/callback"

    if args.websocket_origins:
        websocket_origins = args.websocket_origins
    elif client_url:
        websocket_origins = client_url.rstrip("/")
    else:
        if not args.non_interactive:
            websocket_origins = Prompt.ask(
                "\nWebSocket Allowed Origins (comma-separated URLs)", default=""
            )
        else:
            websocket_origins = ""

    console.print("\n[bold green]Auto-configured:[/bold green]")
    console.print(f"  ✓ OAUTH_CALLBACK_URL → {oauth_callback_url}")
    if websocket_origins:
        console.print(f"  ✓ WEBSOCKET_ALLOWED_ORIGINS → {websocket_origins}")

    # Phase 2: Auto-Configuration
    console.print("\n[bold]⚙️  STEP 2: Auto-Configuration[/bold]")
    console.print("=" * 60)

    config_vars = {}

    # Check addons
    console.print("\n[cyan]Checking addons...[/cyan]")
    addons = detect_addons(server_app)

    if addons["postgres"]:
        console.print("[green]✓ PostgreSQL addon found[/green]")
        pg_creds = extract_postgres_credentials(server_app)
        if pg_creds:
            config_vars.update(pg_creds)
            console.print(
                f"  [dim]Extracted {len(pg_creds)} PostgreSQL variables[/dim]"
            )
    else:
        console.print("[yellow]⚠ PostgreSQL addon not found[/yellow]")
        if not args.skip_addons and not args.non_interactive:
            if Confirm.ask("  Provision Heroku Postgres (essential-0)?"):
                console.print("  [yellow]Provisioning PostgreSQL...[/yellow]")
                try:
                    run_command(
                        [
                            "heroku",
                            "addons:create",
                            "heroku-postgresql:essential-0",
                            "--app",
                            server_app,
                        ]
                    )
                    console.print("  [green]✓ PostgreSQL provisioned[/green]")
                    # Extract credentials
                    pg_creds = extract_postgres_credentials(server_app)
                    if pg_creds:
                        config_vars.update(pg_creds)
                except subprocess.CalledProcessError:
                    console.print("  [red]✗ Failed to provision PostgreSQL[/red]")

    if addons["redis"]:
        console.print("[green]✓ Redis addon found[/green]")
        redis_creds = extract_redis_credentials(server_app)
        if redis_creds:
            config_vars.update(redis_creds)
            console.print(f"  [dim]Extracted {len(redis_creds)} Redis variables[/dim]")
    else:
        console.print("[yellow]⚠ Redis addon not found[/yellow]")
        if not args.skip_addons and not args.non_interactive:
            if Confirm.ask("  Provision Heroku Redis (mini)?"):
                console.print("  [yellow]Provisioning Redis...[/yellow]")
                try:
                    run_command(
                        [
                            "heroku",
                            "addons:create",
                            "heroku-redis:mini",
                            "--app",
                            server_app,
                        ]
                    )
                    console.print("  [green]✓ Redis provisioned[/green]")
                    # Extract credentials
                    redis_creds = extract_redis_credentials(server_app)
                    if redis_creds:
                        config_vars.update(redis_creds)
                except subprocess.CalledProcessError:
                    console.print("  [red]✗ Failed to provision Redis[/red]")

    # JWT Secret - check both new TMI_ prefix and legacy
    console.print("\n[cyan]Configuring JWT secret...[/cyan]")
    existing_jwt = get_existing_config(server_app, "TMI_JWT_SECRET")
    existing_jwt_legacy = get_existing_config(server_app, "JWT_SECRET")
    if existing_jwt or existing_jwt_legacy:
        console.print("[yellow]⚠ JWT secret already exists[/yellow]")
        if not args.non_interactive:
            if Confirm.ask("  Generate new JWT secret?", default=False):
                jwt_secret = generate_jwt_secret()
                config_vars["TMI_JWT_SECRET"] = jwt_secret
                console.print("  [green]✓ Generated new JWT secret[/green]")
                console.print(f"  [yellow]⚠ SAVE THIS: {jwt_secret}[/yellow]")
            else:
                console.print("  [dim]Keeping existing JWT secret[/dim]")
        else:
            console.print(
                "  [dim]Keeping existing JWT secret (non-interactive mode)[/dim]"
            )
    else:
        jwt_secret = generate_jwt_secret()
        config_vars["TMI_JWT_SECRET"] = jwt_secret
        console.print("  [green]✓ Generated JWT secret[/green]")
        console.print(f"  [yellow]⚠ SAVE THIS: {jwt_secret}[/yellow]")

    # OAuth and WebSocket - use TMI_ prefixed variables
    config_vars["TMI_OAUTH_CALLBACK_URL"] = oauth_callback_url
    if websocket_origins:
        config_vars["TMI_WEBSOCKET_ALLOWED_ORIGINS"] = websocket_origins

    # Server defaults - use TMI_ prefixed variables
    # These map to struct tags via envAliases in internal/config/config.go
    config_vars.update(
        {
            "TMI_SERVER_INTERFACE": "0.0.0.0",
            "TMI_LOG_LEVEL": "info",
            "TMI_LOG_IS_DEV": "false",
            "TMI_SERVER_TLS_ENABLED": "false",
            "TMI_LOG_API_REQUESTS": "true",
            "TMI_LOG_REDACT_AUTH_TOKENS": "true",
            "TMI_LOG_WEBSOCKET_MESSAGES": "false",
            "TMI_BUILD_MODE": "production",
        }
    )

    # Phase 3: User Input
    if not args.non_interactive and not args.skip_oauth:
        console.print("\n[bold]🔐 STEP 3: OAuth Providers[/bold]")
        console.print("=" * 60)

        for provider, display in [
            ("google", "Google"),
            ("github", "GitHub"),
            ("microsoft", "Microsoft"),
        ]:
            oauth_config = prompt_oauth_provider(provider, display, args.skip_oauth)
            if oauth_config:
                config_vars.update(oauth_config)

    # Operator information - use TMI_ prefix
    if not args.non_interactive:
        console.print("\n[bold]📇 STEP 4: Operator Information (Optional)[/bold]")
        console.print("=" * 60)

        operator_name = Prompt.ask("\nOperator Name", default="")
        if operator_name:
            config_vars["TMI_OPERATOR_NAME"] = operator_name

        operator_contact = Prompt.ask("Operator Contact (email/URL)", default="")
        if operator_contact:
            config_vars["TMI_OPERATOR_CONTACT"] = operator_contact

    # Phase 4: Apply configuration
    console.print("\n[bold]✨ STEP 5: Apply Configuration[/bold]")
    console.print("=" * 60)

    display_summary(server_app, config_vars)

    if not args.non_interactive and not args.dry_run:
        if not Confirm.ask("\n[bold]Apply this configuration?[/bold]", default=True):
            console.print("[yellow]Configuration cancelled[/yellow]")
            sys.exit(0)

    success = set_config_vars(server_app, config_vars, dry_run=args.dry_run)

    if success and not args.dry_run:
        display_next_steps(server_app, server_url, client_url)
    elif args.dry_run:
        console.print("\n[yellow]Dry run complete. No changes were applied.[/yellow]")
        console.print("[yellow]Run without --dry-run to apply configuration.[/yellow]")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        console.print("\n[yellow]Configuration cancelled by user[/yellow]")
        sys.exit(1)
    except Exception as e:
        console.print(f"\n[red]Unexpected error: {e}[/red]")
        raise
