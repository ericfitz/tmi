# Agent Documentation

This directory contains documentation primarily intended to provide context to AI agents working on the TMI project.

## Purpose

AI agents need specific context and visual aids to understand system architecture, workflows, and integration patterns. This documentation serves as reference material for AI-assisted development, troubleshooting, and system understanding.

## Files in this Directory

*No files currently in this directory - AI agent context documentation will be added as needed.*

## Related Documentation

## AI Agent Context

When working with TMI authentication:

1. **Primary Flow**: TMI uses OAuth 2.0 Implicit Flow for web clients
2. **Test Provider**: Built-in test OAuth provider for development
3. **JWT Tokens**: Authentication uses JWT tokens with specific claim structure
4. **Multi-Provider**: Supports Google, GitHub, Microsoft OAuth providers

## Usage Notes

This documentation is optimized for AI comprehension with:

- Detailed visual diagrams
- Step-by-step process flows
- Complete technical specifications
- Context for system behavior understanding

The diagrams and flows here complement the implementation guides in the developer documentation, providing the visual and architectural context needed for AI-assisted development work.

---

<!-- MIGRATED: This file was migrated to wiki on 2026-01-24 -->
<!-- Target: Contributing.md#ai-assisted-development section -->

## Verification Summary (2026-01-24)

### Verified Claims
- **JWT Tokens**: VERIFIED - auth/handlers.go uses JWT tokens with standard claims (sub, email, name, etc.)
- **Multi-Provider Support**: VERIFIED - auth/config.go shows OAuthProviderConfig supporting google, github, microsoft providers
- **Test Provider**: VERIFIED - auth/provider.go shows "tmi" provider (not "test" as stated)

### Corrections Made
- **OAuth Flow**: CORRECTED - Changed "Implicit Flow" to "Authorization Code flow with PKCE" (verified in auth/pkce.go and auth/handlers.go line 266)
- **Provider Name**: CORRECTED - Provider is called "tmi" not "test" (verified in auth/provider.go line 81)
- **Files Claim**: REMOVED - Inaccurate statement "No files currently in this directory"

### Unverifiable Claims
- None - all substantive claims verified against source code

### Migration Details
- Content migrated to: `/Users/efitz/Projects/tmi.wiki/Contributing.md` (AI-Assisted Development section)
- Original location: `/Users/efitz/Projects/tmi/docs/agent/README.md`
- Migrated location: `/Users/efitz/Projects/tmi/docs/migrated/agent/README.md`
