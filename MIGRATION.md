# TMI Migration Notes

## Server Architecture Migration

The TMI application has been migrated to use a unified Express server architecture.

### Changes Made

1. **Server Architecture:**
   - Migrated from Angular CLI's built-in dev server to using Express for both API and static file serving
   - This ensures that security features like CSRF protection work correctly
   - Express server now handles both development and production builds

2. **Security Improvements:**
   - CSRF token generation and validation now happens in a single server
   - Content Security Policy (CSP) headers are correctly configured
   - Added more comprehensive security headers via Helmet

3. **Development Workflow:**
   - Added a `dev.sh` script for a better development experience with auto-rebuilding
   - Updated npm scripts to use the new server architecture

4. **Test Infrastructure:**
   - Migrated from Karma/Jasmine to Cypress for component testing
   - Removed unnecessary Protractor dependencies
   - Updated test scripts and configuration

5. **Dependency Cleanup:**
   - Removed unused Karma and Jasmine related packages
   - Added dependency overrides to prevent vulnerability issues
   - Optimized package.json
   - Explicitly installed @esbuild/darwin-arm64 to support Apple Silicon

## Running the Application

### Development Mode

For active development with auto-rebuild:
```bash
./dev.sh
```

For basic development:
```bash
npm start
```

### Production Mode
```bash
npm run start:prod
```

## Testing

Run unit tests (component tests with Cypress):
```bash
npm test
```

Run end-to-end tests:
```bash
npm run e2e
```

Run all tests and checks:
```bash
./test-all.sh
```

## Future Improvements

1. Configure Cypress to generate test coverage reports
2. Implement watch mode for test files
3. Further optimize Express server configuration
4. Consider implementing hot module replacement for development