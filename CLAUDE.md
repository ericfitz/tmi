# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
This repository contains API documentation for a Collaborative Threat Modeling Interface (TMI).

## Key Files
- TMI-API-v1_0.md - API documentation in Markdown
- TMI-OpenAPI_3_1.json - OpenAPI 3.1 specification

## Style Guidelines
- Follow OpenAPI 3.1 specification standards for API schema modifications
- Use JSON Schema formatting for data models
- Maintain consistent property naming (snake_case)
- Include comprehensive descriptions for all properties and endpoints
- Follow RESTful API design patterns for endpoints
- Document error responses for all endpoints (401, 403, 404)
- Preserve required field specifications in schemas

## Validation
- UUID format for IDs
- ISO8601 format for timestamps
- Email format for user identifiers
- Role-based access control with reader/writer/owner permissions

## API Conventions
- Bearer token auth with JWT
- JSON Patch for partial updates
- WebSocket for real-time collaboration
- Consistent pagination parameters (limit, offset)