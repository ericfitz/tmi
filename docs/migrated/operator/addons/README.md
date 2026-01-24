# Addon Configuration

<!-- Migrated from: docs/operator/addons/README.md on 2026-01-24 -->

This directory previously contained configuration and management documentation for TMI addons and extensions.

## Migration Notice

This documentation has been consolidated into the TMI wiki and migrated reference documentation:

- **User and Developer Guide**: [Addon System (Wiki)](https://github.com/ericfitz/tmi/wiki/Addon-System) - Complete addon documentation including user guides, administrator setup, and developer integration patterns
- **Operator Configuration**: [addon-configuration.md](addon-configuration.md) - Database schema, Redis configuration, quota management, and troubleshooting

## Addon Architecture Overview

Addons extend TMI functionality through:
- **REST API integration**: Addons can call TMI APIs with service account credentials
- **Webhook subscriptions**: Addons receive invocation requests from TMI for processing
- **Asynchronous status callbacks**: Addons report progress back to TMI via HMAC-authenticated callbacks

## Related Documentation (Migrated Locations)

- [Webhook Configuration](../webhook-configuration.md) - Event delivery to addons
- [OAuth Configuration](../oauth-environment-configuration.md) - Addon authentication
- [Client Integration](../../developer/integration/client-integration-guide.md) - API integration patterns

---

<!-- Verification Summary (2026-01-24):
VERIFIED against source code:
- REST API integration claim (api/addon_invocation_handlers.go - service account credentials supported)
- Webhook subscriptions claim (api/addon_invocation_worker.go - delivers invocation requests to webhooks)
- Async status callbacks (api/addon_invocation_handlers.go:UpdateInvocationStatus)

CORRECTED:
- Removed "Custom endpoints: Addons can register custom API endpoints" - NOT FOUND in source code
  (No evidence that addons can register custom API endpoints; addons work via webhook callbacks only)
- Updated all file references to point to migrated locations
- Changed "events from TMI" to "invocation requests from TMI" for accuracy

FILE REFERENCES VERIFIED:
- addon-configuration.md exists at docs/migrated/operator/addons/addon-configuration.md
- webhook-configuration.md exists at docs/migrated/operator/webhook-configuration.md
- oauth-environment-configuration.md exists at docs/migrated/operator/oauth-environment-configuration.md
- client-integration-guide.md exists at docs/migrated/developer/integration/client-integration-guide.md
- Wiki page Addon-System.md exists at /Users/efitz/Projects/tmi.wiki/Addon-System.md
-->
