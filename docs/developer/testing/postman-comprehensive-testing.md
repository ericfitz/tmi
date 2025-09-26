# TMI API Comprehensive Testing Suite

This directory contains a complete test suite for the TMI (Threat Modeling Improved) API that provides **100% endpoint coverage** and **comprehensive status code testing**.

## 🎯 Test Coverage

### Status Code Coverage
Tests **every supported HTTP status code** for each endpoint:
- **2xx Success**: 200 (OK), 201 (Created), 204 (No Content)  
- **4xx Client Errors**: 400 (Bad Request), 401 (Unauthorized), 403 (Forbidden), 404 (Not Found), 409 (Conflict), 422 (Unprocessable Entity)
- **5xx Server Errors**: 500 (Internal Server Error) edge cases

### Endpoint Coverage
- **Discovery & OAuth**: All `.well-known` endpoints, OAuth flow, token management
- **Threat Models**: Complete CRUD with validation testing
- **Threats**: CRUD, bulk operations, batch processing
- **Diagrams**: CRUD, collaboration sessions, real-time features  
- **Documents & Sources**: CRUD, bulk operations
- **Metadata**: CRUD for all entity types, bulk operations
- **Permissions**: Multi-user access control testing (owner/writer/reader/none)

## 🛠 Test Components

### Core Files

#### `comprehensive-test-collection.json`
Main Postman collection with structured test suites:
- **Setup & Authentication**: Multi-user OAuth setup
- **Discovery Endpoints**: Public API endpoints
- **CRUD Testing**: Complete lifecycle testing
- **Permission Matrix**: Multi-user access scenarios
- **Bulk Operations**: Performance and batch processing
- **Error Scenarios**: Comprehensive failure testing

#### `test-data-factory.js` 
JavaScript factory for generating test data:
- **Valid Data**: Proper schema-compliant objects
- **Invalid Data**: Various validation failure scenarios
- **Bulk Data**: Large datasets for performance testing
- **Edge Cases**: Boundary conditions and limits

#### `multi-user-auth.js`
Multi-user authentication helper:
- **User Management**: alice (owner), bob (writer), charlie (reader), diana (none)
- **Token Caching**: Efficient OAuth token management
- **Permission Testing**: Resource ownership tracking
- **Session Management**: User switching and cleanup

### Specialized Test Modules

#### `threat-crud-tests.json`
Comprehensive threat testing:
- All CRUD operations with success/failure scenarios
- Data validation (required fields, types, enums, ranges)
- JSON Patch operations
- Performance testing

#### `metadata-tests.json` 
Metadata operations for all entity types:
- Individual and bulk metadata operations
- Key-based CRUD operations
- Validation testing (empty keys/values, wrong types)
- Cross-entity metadata testing

#### `permission-matrix-tests.json`
Multi-user permission testing:
- **Owner permissions**: Full CRUD access
- **Writer permissions**: Read/write, no delete
- **Reader permissions**: Read-only access  
- **No permissions**: 403 Forbidden responses
- Cross-resource access testing

#### `bulk-operations-tests.json`
Bulk and batch processing:
- Bulk create/update operations
- Batch patch operations (JSON Patch)
- Batch delete operations
- Performance testing with large datasets
- Atomic operation testing (all-or-nothing)

#### `collaboration-tests.json`
Real-time collaboration features:
- Session management (start/stop/update)
- Participant permissions
- WebSocket URL generation
- Conflict resolution (409 errors)
- Multi-user collaboration scenarios

### Execution & Reporting

#### `run-tests.sh`
Enhanced test execution script:
- **OAuth Stub Management**: Background process startup with proper cleanup
- **Signal Handling**: Graceful cleanup on script interruption (Ctrl+C)
- **Health Checks**: Automatic verification of OAuth stub readiness
- **Multiple Reporters**: CLI, JSON, HTML reports
- **Detailed Analytics**: Status code coverage, failure analysis
- **Performance Metrics**: Response times, success rates
- **Error Reporting**: Failed test details and categorization

## 🚀 Quick Start

### Prerequisites
```bash
# Install Newman (Postman CLI)
npm install -g newman newman-reporter-htmlextra

# Install dependencies
npm install -g jq bc

# Start TMI development server
make start-dev
```

### Run Complete Test Suite
```bash
cd postman
./run-tests.sh
```

### Run Specific Test Categories
```bash
# Just authentication and discovery
newman run comprehensive-test-collection.json --folder "Setup & Authentication" --folder "Discovery"

# Permission testing only  
newman run comprehensive-test-collection.json --folder "Multi-User Permission Testing"

# Bulk operations performance
newman run comprehensive-test-collection.json --folder "Bulk Operations"
```

## 📊 Test Results & Analysis

### Generated Reports
- **HTML Report**: `test-results/test-report-{timestamp}.html`
- **JSON Results**: `test-results/newman-results-{timestamp}.json` 
- **Execution Log**: `test-results/test-log-{timestamp}.txt`

### Key Metrics Tracked
- **Request Success Rate**: % of HTTP requests that succeeded
- **Assertion Success Rate**: % of test assertions that passed  
- **Status Code Coverage**: Count of each HTTP status code tested
- **Performance Metrics**: Response times and throughput
- **Error Analysis**: Categorized failure details

### Sample Output
```
📊 Test Statistics:
   Total requests: 156
   Failed requests: 2
   Success rate: 98.72%
   Total assertions: 312
   Failed assertions: 3
   Assertion success rate: 99.04%
   Total time: 45680ms

📈 Status Code Coverage:
   200: 89 requests
   201: 23 requests  
   204: 12 requests
   400: 15 requests
   401: 8 requests
   403: 6 requests
   404: 3 requests
```

## 🔧 Customization & Extension

### Adding New Test Scenarios

1. **Create New Test File**:
```json
{
  "name": "My Custom Tests",
  "item": [
    {
      "name": "Custom Test Case",
      "request": { ... },
      "event": [
        {
          "listen": "test",
          "script": {
            "exec": ["pm.test('My assertion', ...)"]
          }
        }
      ]
    }
  ]
}
```

2. **Use Test Data Factory**:
```javascript
// In pre-request script
const factory = new TMITestDataFactory();
const testData = factory.validThreatModel({
  name: 'Custom test data',
  customField: 'custom value'
});
```

3. **Multi-User Testing**:
```javascript
// Switch users for permission testing
if (typeof tmiAuth !== 'undefined') {
    tmiAuth.setActiveUser('bob');
}
```

### Environment Configuration

#### Variables
- `baseUrl`: TMI server URL (default: http://127.0.0.1:8080)
- `oauthStubUrl`: OAuth stub URL (default: http://127.0.0.1:8079)
- `loginHint`: Test user identifier
- `access_token`: Current authentication token

#### Custom Environments
```json
{
  "name": "TMI Staging",
  "values": [
    {"key": "baseUrl", "value": "https://staging-tmi.example.com"},
    {"key": "loginHint", "value": "staging-test-user"}
  ]
}
```

## 🐛 Troubleshooting

### Common Issues

#### Authentication Failures
```bash
# Ensure OAuth stub is running
make start-oauth-stub

# Check stub status  
curl http://127.0.0.1:8079/latest
```

#### Server Connection Issues
```bash
# Verify TMI server is running
curl http://127.0.0.1:8080/

# Check server logs
make logs
```

#### Performance Issues
```bash
# Run subset of tests
newman run comprehensive-test-collection.json --folder "Discovery"

# Increase timeouts
newman run --timeout-request 10000 --timeout-script 5000
```

#### Test Failures Analysis
1. Check HTML report for detailed failure analysis
2. Review JSON results for specific assertion failures  
3. Examine execution log for network/OAuth issues
4. Verify test data factory generates valid schemas

### Debug Mode
```bash
# Enable verbose logging
newman run comprehensive-test-collection.json --verbose

# Export debug data
newman run --export-globals globals.json --export-environment env.json
```

## 🎯 Test Strategy

### Validation Testing
- **Schema Compliance**: All requests match OpenAPI specification
- **Required Fields**: Missing field validation (400 errors)
- **Data Types**: Wrong type validation (400 errors)  
- **Enum Values**: Invalid enum validation (400 errors)
- **Constraints**: Length, range, format validation (400 errors)

### Permission Testing  
- **Ownership**: Resource owners have full access
- **Role-Based**: Writers can modify, readers cannot
- **Isolation**: Users cannot access others' resources
- **Authorization**: Missing/invalid tokens (401 errors)

### Performance Testing
- **Bulk Operations**: Large dataset handling
- **Response Times**: Acceptable performance thresholds
- **Concurrent Access**: Multi-user scenarios
- **Resource Limits**: Memory and processing constraints

### Error Handling
- **4xx Errors**: Proper client error responses
- **5xx Errors**: Graceful server error handling
- **Edge Cases**: Boundary conditions and limits
- **Recovery**: System stability after errors

## 📚 References

- [TMI API Documentation](../docs/TMI-API-v1_0.md)
- [OpenAPI Specification](../shared/api-specs/tmi-openapi.json) 
- [Newman Documentation](https://learning.postman.com/docs/running-collections/using-newman-cli/)
- [Postman Testing Guide](https://learning.postman.com/docs/writing-scripts/test-scripts/)

---

**💡 Pro Tips:**
- Run tests frequently during development to catch regressions early
- Use the permission matrix tests to validate access control changes
- Monitor performance metrics to identify bottlenecks
- Leverage bulk operations for load testing scenarios
- Check HTML reports for visual failure analysis and trends