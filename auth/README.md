# Authentication and Authorization System for TMI

This package implements a secure, reliable, and robust authentication and authorization system for the TMI service, designed to work with the tmi-ux Angular web application.

## Features

- **Multiple OAuth 2.0 Providers**: Support for Google, GitHub, and Microsoft authentication
- **JWT-based Authentication**: Secure JWT tokens for API authentication
- **Refresh Token Mechanism**: Automatic token refresh without requiring re-authentication
- **Account Linking**: Link multiple OAuth providers to a single user account
- **Role-based Authorization**: Support for owner, writer, and reader roles
- **Redis Caching**: High-performance caching of authorization data
- **Database Migrations**: Automatic database schema management

## Architecture

The authentication system uses a hybrid database approach:

1. **PostgreSQL** as the primary persistent store for:

   - User accounts and profiles
   - OAuth provider configurations
   - Authorization data (roles, permissions)
   - Account linking information

2. **Redis** for:
   - Authorization cache
   - Token management
   - Rate limiting
   - Session data

## Setup

### Prerequisites

- PostgreSQL 12+
- Redis 6+
- Go 1.24+

### Configuration

1. Copy the `.env.example` file to `.env`:

```bash
cp .env.example .env
```

2. Edit the `.env` file with your configuration values:

```
# PostgreSQL Configuration
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=your_postgres_user
POSTGRES_PASSWORD=your_postgres_password
POSTGRES_DB=tmi
POSTGRES_SSLMODE=disable

# Redis Configuration
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=your_redis_password
REDIS_DB=0

# JWT Configuration
JWT_SECRET=your_jwt_secret_key
JWT_EXPIRATION_SECONDS=3600
JWT_SIGNING_METHOD=HS256

# OAuth Configuration
OAUTH_CALLBACK_URL=http://localhost:8080/oauth2/callback

# OAuth Provider Configuration
...
```

3. Set up OAuth providers:

   - **Google**: Create a project in the [Google Developer Console](https://console.developers.google.com/), enable the OAuth API, and create OAuth credentials.
   - **GitHub**: Register a new OAuth application in [GitHub Developer Settings](https://github.com/settings/developers).
   - **Microsoft**: Register an application in the [Azure Portal](https://portal.azure.com/).

### Integration

To integrate the authentication system into your application:

```go
package main

import (
	"github.com/ericfitz/tmi/auth"
	"github.com/gin-gonic/gin"
)

func main() {
	// Create a new Gin router
	router := gin.Default()

	// Initialize the authentication system
	if err := auth.InitAuth(router); err != nil {
		panic(err)
	}

	// Add your routes
	// ...

	// Start the server
	router.Run(":8080")
}
```

## API Endpoints

### OAuth Flow

- `GET /oauth2/providers` - List available OAuth providers
- `GET /oauth2/authorize/:provider` - Redirect to OAuth provider for authentication
- `GET /oauth2/callback` - Handle OAuth callback and issue JWT tokens

### Token Management

- `POST /oauth2/token/:provider` - Exchange authorization code for JWT tokens
- `POST /oauth2/refresh` - Refresh an expired JWT token
- `POST /oauth2/logout` - Revoke a refresh token

### User Information

- `GET /oauth2/me` - Get current user information (requires authentication)

## Authorization

To protect your API endpoints, use the authentication middleware:

```go
// Create a new router group with authentication
protected := router.Group("/api")
protected.Use(authMiddleware.AuthRequired())

// Add routes that require authentication
protected.GET("/resource", getResource)

// Add routes that require specific roles
protected.POST("/resource", authMiddleware.RequireWriter(), createResource)
protected.PUT("/resource/:id", authMiddleware.RequireWriter(), updateResource)
protected.DELETE("/resource/:id", authMiddleware.RequireOwner(), deleteResource)
```

## Database Migrations

The authentication system automatically runs database migrations on startup. The migration files are located in the `migrations` directory.

## Cache Rebuilding

The authentication system includes a background job that periodically rebuilds the Redis cache from PostgreSQL to handle any potential inconsistencies.

## Security Considerations

- JWT tokens are short-lived (1 hour by default)
- Refresh tokens are stored in Redis with automatic expiration
- CSRF protection is implemented using state parameters
- All sensitive data is stored securely
- OAuth provider credentials are stored in environment variables
