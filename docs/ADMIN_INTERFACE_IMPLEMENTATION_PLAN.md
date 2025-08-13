# TMI Admin Interface Implementation Plan

## **Overview**

This document outlines the implementation plan for adding an admin web interface to the TMI (Threat Modeling Interface) system. The admin interface will provide session management capabilities and system configuration viewing through a secure, web-based interface.

## **Architecture Summary**

- **Authentication**: Existing OAuth flow + email-based admin check
- **Session Management**: Enhanced Redis session storage with admin metadata  
- **Web Interface**: Vanilla HTML/CSS/JavaScript using Gin templates
- **API**: RESTful endpoints for session operations
- **Dependencies**: Zero new external dependencies

## **Key Features**

1. **Admin Authentication**: Email-based admin authorization using existing OAuth
2. **Session Management**: View and invalidate user sessions via token blacklisting
3. **System Configuration**: Display current system configuration (filtered)
4. **Security**: Admin-only access with audit logging and CSRF protection

## **Implementation Phases**

### **Phase 1: Configuration & Authentication (Week 1)**

#### **Objective**: Set up admin configuration and authentication mechanism

#### **Files to Modify:**
- `internal/config/config.go` - Add admin configuration structure
- `auth/middleware.go` - Add admin authorization middleware

#### **Tasks:**
1. Add admin configuration structure to config system
2. Implement email-based admin authorization
3. Create admin middleware for route protection
4. Add configuration validation for admin settings

#### **Configuration Structure:**
```yaml
admin:
  enabled: true
  users:
    primary_email: "admin@company.com"
    additional_emails: []
  session:
    timeout_minutes: 240
    reauth_required: true
  security:
    audit_logging: true
    rate_limit_enabled: true
```

#### **Environment Variables:**
```bash
TMI_ADMIN_ENABLED=true
TMI_ADMIN_USERS_PRIMARY_EMAIL=admin@company.com
TMI_ADMIN_USERS_ADDITIONAL_EMAILS=admin2@company.com,admin3@company.com
```

### **Phase 2: Session Data Enhancement (Week 2)**

#### **Objective**: Enhance session tracking for admin visibility

#### **Files to Create/Modify:**
- `api/admin_session_store.go` - Enhanced session metadata storage
- `auth/service.go` - Add session metadata to existing auth flow
- `api/admin_types.go` - Admin data structures

#### **Session Data Structure:**
```go
type AdminSessionMetadata struct {
    SessionID     string    `json:"session_id"`
    UserEmail     string    `json:"user_email"`
    UserName      string    `json:"user_name"`
    CreatedAt     time.Time `json:"created_at"`
    LastActivity  time.Time `json:"last_activity"`
    IPAddress     string    `json:"ip_address"`
    UserAgent     string    `json:"user_agent"`
    JWTTokenID    string    `json:"jwt_token_id"`
    Status        string    `json:"status"`
}
```

#### **Redis Storage Enhancement:**
- `admin:session:{userID}:{sessionID}` - Session metadata
- `admin:active_sessions` - Global active sessions index
- `admin:session_by_jwt:{jwtID}` - JWT to session lookup

### **Phase 3: Admin API Endpoints (Week 2)**

#### **Objective**: Create API endpoints for admin operations

#### **Files to Create:**
- `api/admin_handlers.go` - Admin web page handlers
- `api/admin_api_handlers.go` - JSON API handlers
- `api/admin_middleware.go` - Admin-specific middleware

#### **Core Endpoints:**
```
GET  /admin                     -> Dashboard (HTML)
GET  /admin/sessions           -> Sessions page (HTML)  
GET  /admin/config             -> Configuration page (HTML)
GET  /api/admin/sessions       -> List sessions (JSON)
GET  /api/admin/sessions/{id}  -> Get session details (JSON)
DELETE /api/admin/sessions/{id} -> Invalidate session (JSON)
POST /api/admin/sessions/bulk/invalidate -> Bulk invalidate (JSON)
GET  /api/admin/system/config  -> Show configuration (JSON)
```

#### **Key Functions:**
- `ListActiveSessions()` - Query Redis for active sessions
- `InvalidateSession(sessionID)` - Add JWT to blacklist + delete session  
- `GetSessionDetail(sessionID)` - Get full session metadata
- `GetSystemConfiguration()` - Return filtered system config

### **Phase 4: Web Interface (Week 3)**

#### **Objective**: Build the admin web interface with minimal dependencies

#### **Directory Structure:**
```
templates/admin/
├── base.html          # Base template with layout
├── dashboard.html     # Main admin dashboard
├── sessions.html      # Session management page  
└── config.html        # Configuration view

static/admin/
├── css/
│   ├── admin.css      # Main admin styles
│   └── components.css # Reusable components
└── js/
    ├── admin.js       # Core admin functionality
    └── sessions.js    # Session management JS
```

#### **UI Components:**
- **Dashboard**: Active session count, system health, recent activity
- **Sessions Page**: Filterable table with session data and actions
- **Session Actions**: "Force Logout" buttons → calls DELETE API
- **Config View**: Display filtered system configuration
- **Navigation**: Simple sidebar with admin sections

#### **Technologies Used:**
- **HTML5** with semantic markup
- **CSS Grid/Flexbox** for responsive layout  
- **Vanilla JavaScript** for interactivity
- **Go html/template** for server-side rendering

### **Phase 5: Integration & Testing (Week 1)**

#### **Objective**: Integrate components and test functionality

#### **Server Integration:**
```go
// In cmd/server/main.go
if config.Admin.Enabled {
    admin := r.Group("/admin")
    admin.Use(RequireAdminAuth())
    
    adminHandler := NewAdminHandler(authService, sessionStore)
    adminHandler.RegisterRoutes(admin)
}
```

#### **Testing Plan:**
1. **Unit Tests**: Admin authorization, session operations
2. **Integration Tests**: Full admin workflow testing
3. **Security Tests**: Unauthorized access attempts, CSRF protection
4. **Manual Testing**: UI functionality, responsive design
5. **Performance Tests**: Session listing with high session counts

## **Security Considerations**

### **Access Control**
- Admin interface disabled by default (`admin.enabled: false`)
- Email-based authorization with configurable admin list
- Session timeout for admin operations
- Re-authentication required for sensitive operations

### **Security Features**
- **CSRF Protection**: Form tokens for destructive actions
- **Audit Logging**: Log all admin session management actions  
- **Rate Limiting**: Prevent abuse of admin endpoints
- **Input Validation**: Validate all session IDs and user inputs
- **IP Restrictions**: Optional IP allowlist for admin access

### **Data Protection**
- **Filtered Configuration**: Sensitive config values hidden
- **Session Privacy**: Only show necessary session metadata
- **Secure Cookies**: HttpOnly, Secure flags for admin sessions

## **Configuration Examples**

### **Development Configuration**
```yaml
# config-dev.yaml
admin:
  enabled: true
  users:
    primary_email: "admin@example.com"
    additional_emails: 
      - "dev@example.com"
    auto_promote_first: true
  session:
    timeout_minutes: 480  # 8 hours for development
    reauth_required: false
  security:
    audit_logging: true
    rate_limit_enabled: false
    ip_allowlist: []
```

### **Production Configuration**
```yaml
# config-prod.yaml
admin:
  enabled: false  # Must be explicitly enabled
  users:
    primary_email: ""  # Must be configured
    additional_emails: []
    auto_promote_first: false
  session:
    timeout_minutes: 240  # 4 hours
    reauth_required: true
  security:
    audit_logging: true
    rate_limit_enabled: true
    ip_allowlist: ["10.0.0.0/8"]  # Internal network only
```

## **Implementation Timeline**

| Phase | Duration | Key Deliverables |
|-------|----------|-----------------|
| Phase 1 | Week 1 | Configuration system, admin auth |
| Phase 2 | Week 2 | Enhanced session storage |  
| Phase 3 | Week 2 | API endpoints, session management |
| Phase 4 | Week 3 | Web interface, templates, CSS/JS |
| Phase 5 | Week 1 | Integration, testing, documentation |
| **Total** | **5 weeks** | **Complete admin interface** |

## **Success Criteria**

### **Functional Requirements**
- ✅ Admin can authenticate using existing OAuth
- ✅ Admin can view all active user sessions
- ✅ Admin can invalidate individual sessions
- ✅ Admin can view system configuration  
- ✅ Interface works on desktop and mobile devices

### **Security Requirements**
- ✅ Only configured admin emails can access interface
- ✅ All admin actions are logged for audit
- ✅ Session invalidation uses existing blacklist mechanism
- ✅ Admin interface is disabled by default
- ✅ CSRF protection on destructive operations

### **Performance Requirements**
- ✅ Session listing loads in <2 seconds with 1000+ sessions
- ✅ Session invalidation completes in <500ms
- ✅ Interface remains responsive on mobile devices
- ✅ No impact on existing API performance

## **Maintenance Considerations**

### **Monitoring**
- Admin login attempts and failures
- Session invalidation frequency and patterns
- Admin interface performance metrics
- Security events (unauthorized access attempts)

### **Documentation**
- Admin user setup and configuration guide
- Session management procedures  
- Security best practices for admin access
- Troubleshooting guide for common issues

## **Future Enhancements**

### **Phase 6 (Optional)**
- **User Management**: Create/disable user accounts
- **Threat Model Management**: View and manage threat models
- **System Metrics**: Performance dashboards and alerts
- **Multi-Factor Authentication**: Enhanced admin security
- **Role-Based Admin Access**: Different admin permission levels

This plan provides a secure, functional admin interface using minimal dependencies while leveraging the existing TMI architecture and security patterns.