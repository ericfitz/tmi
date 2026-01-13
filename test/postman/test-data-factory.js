/**
 * TMI API Test Data Factory
 * 
 * Generates valid and invalid test data for comprehensive API testing
 * Based on OpenAPI schema requirements from tmi-openapi.json
 */

class TMITestDataFactory {
    constructor() {
        this.timestamp = new Date().toISOString();
        this.testRunId = Date.now();
    }

    // ========================================
    // HELPER METHODS
    // ========================================

    generateUUID() {
        return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
            var r = Math.random() * 16 | 0, v = c == 'x' ? r : (r & 0x3 | 0x8);
            return v.toString(16);
        });
    }

    generateTestUser(suffix = '') {
        return `test-${this.testRunId}${suffix ? '-' + suffix : ''}`;
    }

    // ========================================
    // THREAT MODEL TEST DATA
    // ========================================

    validThreatModel(options = {}) {
        return {
            name: options.name || `Test Threat Model ${this.testRunId}`,
            description: options.description || "A comprehensive threat model for testing purposes",
            threat_model_framework: options.framework || "STRIDE",
            issue_uri: options.issueUri || "https://github.com/example/project/issues/123",
            ...options.additional
        };
    }

    invalidThreatModelData() {
        return {
            // Missing required 'name' field - should trigger 400
            missing_name: {
                description: "Missing name field",
                threat_model_framework: "STRIDE"
            },
            // Empty name - should trigger 400
            empty_name: {
                name: "",
                threat_model_framework: "STRIDE"
            },
            // Name too long (> 256 chars) - should trigger 400
            name_too_long: {
                name: "x".repeat(257),
                threat_model_framework: "STRIDE"
            },
            // Description too long (> 1024 chars) - should trigger 400
            description_too_long: {
                name: "Valid Name",
                description: "x".repeat(1025),
                threat_model_framework: "STRIDE"
            },
            // Invalid issue URI - should trigger 400
            invalid_uri: {
                name: "Valid Name",
                threat_model_framework: "STRIDE",
                issue_uri: "not-a-url"
            },
            // Wrong data types - should trigger 400
            wrong_types: {
                name: 123, // should be string
                description: true, // should be string
                threat_model_framework: null // should be string
            }
        };
    }

    // ========================================
    // THREAT TEST DATA
    // ========================================

    validThreat(options = {}) {
        // threat_type is now an array per OpenAPI spec
        const threatType = options.threatType || options.threat_type || ["Spoofing"];
        return {
            name: options.name || `Test Threat ${this.testRunId}`,
            description: options.description || "A test threat for comprehensive API testing",
            threat_type: Array.isArray(threatType) ? threatType : [threatType],
            severity: options.severity || "High",
            priority: options.priority || "High",
            score: options.score || 8.5,
            status: options.status || "Open",
            mitigated: options.mitigated !== undefined ? options.mitigated : false,
            issue_uri: options.issueUri || options.issue_uri || null,
            ...options.additional
        };
    }

    invalidThreatData() {
        return {
            // Missing required fields - should trigger 400
            // Required: name and threat_type (as array)
            missing_name: {
                threat_type: ["Spoofing"],
                severity: "High",
                priority: "High",
                mitigated: false,
                status: "Open"
            },
            missing_threat_type: {
                name: "Valid Name",
                severity: "High",
                priority: "High",
                mitigated: false,
                status: "Open"
            },
            // Empty threat_type array - should trigger 400
            empty_threat_type: {
                name: "Valid Name",
                threat_type: [],
                severity: "High",
                priority: "High",
                mitigated: false,
                status: "Open"
            },
            // threat_type as string instead of array - should trigger 400
            threat_type_not_array: {
                name: "Valid Name",
                threat_type: "Spoofing",
                severity: "High",
                priority: "High",
                mitigated: false,
                status: "Open"
            },
            // Wrong data types - should trigger 400
            wrong_types: {
                name: 123, // should be string
                threat_type: "Spoofing", // should be array
                severity: true, // should be string
                mitigated: "true", // should be boolean
                score: "high" // should be number
            },
            // Score out of range - should trigger 400
            score_negative: {
                name: "Valid Name",
                threat_type: ["Spoofing"],
                severity: "High",
                priority: "High",
                mitigated: false,
                status: "Open",
                score: -1
            },
            score_too_high: {
                name: "Valid Name",
                threat_type: ["Spoofing"],
                severity: "High",
                priority: "High",
                mitigated: false,
                status: "Open",
                score: 11
            }
        };
    }

    // ========================================
    // DIAGRAM TEST DATA
    // ========================================

    validDiagram(options = {}) {
        return {
            name: options.name || `Test Diagram ${this.testRunId}`,
            description: options.description || "A test diagram for API testing",
            type: options.type || "DFD-1.0.0",
            cells: options.cells || this.generateBasicDiagramCells(),
            ...options.additional
        };
    }

    generateBasicDiagramCells() {
        return [
            {
                id: this.generateUUID(),
                shape: "threat-model-process",
                x: 100,
                y: 100, 
                width: 120,
                height: 80,
                label: "User Process"
            },
            {
                id: this.generateUUID(),
                shape: "threat-model-datastore",
                x: 300,
                y: 100,
                width: 120, 
                height: 80,
                label: "Database"
            }
        ];
    }

    invalidDiagramData() {
        return {
            // Missing required fields - should trigger 400
            missing_name: {
                type: "DFD-1.0.0"
            },
            missing_type: {
                name: "Valid Name"
            },
            // Invalid type - should trigger 400
            invalid_type: {
                name: "Valid Name",
                type: "InvalidType"
            },
            // Wrong data types - should trigger 400
            wrong_types: {
                name: 123, // should be string
                type: true, // should be string
                cells: "invalid" // should be array
            },
            // Invalid cell data - should trigger 400
            invalid_cells: {
                name: "Valid Name", 
                type: "DFD-1.0.0",
                cells: [
                    {
                        // Missing required id and shape
                        x: 100,
                        y: 100
                    }
                ]
            }
        };
    }

    // ========================================
    // DOCUMENT TEST DATA (first definition - kept for compatibility)
    // ========================================

    // Note: This is overridden by a more complete definition below

    // ========================================
    // SOURCE/REPOSITORY TEST DATA (first definition - kept for compatibility)
    // ========================================

    // Note: This is overridden by a more complete definition below

    // ========================================
    // METADATA TEST DATA  
    // ========================================

    validMetadata(options = {}) {
        return {
            key: options.key || `test-key-${this.testRunId}`,
            value: options.value || `test-value-${this.testRunId}`,
            ...options.additional
        };
    }

    bulkMetadata(count = 3) {
        const metadata = [];
        for (let i = 0; i < count; i++) {
            metadata.push({
                key: `bulk-key-${i}-${this.testRunId}`,
                value: `bulk-value-${i}-${this.testRunId}`
            });
        }
        return metadata;
    }

    invalidMetadataData() {
        return {
            // Missing required fields - should trigger 400
            missing_key: {
                value: "some value"
            },
            missing_value: {
                key: "some-key"
            },
            // Wrong data types - should trigger 400
            wrong_types: {
                key: 123, // should be string
                value: true // should be string
            },
            // Empty values - should trigger 400  
            empty_key: {
                key: "",
                value: "some value"
            },
            empty_value: {
                key: "some-key", 
                value: ""
            }
        };
    }

    // ========================================
    // COLLABORATION TEST DATA
    // ========================================

    validCollaborationSession(options = {}) {
        return {
            participants: options.participants || ["alice", "bob"],
            permissions: options.permissions || "writer",
            ...options.additional
        };
    }

    // ========================================
    // JSON PATCH TEST DATA
    // ========================================

    validJsonPatch(operations = []) {
        if (operations.length === 0) {
            operations = [
                {
                    op: "replace",
                    path: "/name", 
                    value: `Updated Name ${this.testRunId}`
                }
            ];
        }
        return operations;
    }

    invalidJsonPatchData() {
        return {
            // Missing required fields - should trigger 400
            missing_op: [
                {
                    path: "/name",
                    value: "Updated Name"
                }
            ],
            missing_path: [
                {
                    op: "replace",
                    value: "Updated Name"
                }
            ],
            // Invalid operation - should trigger 400
            invalid_op: [
                {
                    op: "invalid-operation",
                    path: "/name",
                    value: "Updated Name"
                }
            ],
            // Invalid path format - should trigger 400  
            invalid_path: [
                {
                    op: "replace",
                    path: "name", // should start with /
                    value: "Updated Name"
                }
            ]
        };
    }

    // ========================================
    // DOCUMENT TEST DATA
    // ========================================

    validDocument(options = {}) {
        return {
            name: options.name || `Test Document ${this.testRunId}`,
            uri: options.uri || options.url || `https://example.com/documents/test-${this.testRunId}.pdf`,
            description: options.description || "A test document for comprehensive API testing",
            ...options.additional
        };
    }

    invalidDocumentData() {
        return {
            // Missing required 'name' field - should trigger 400
            missing_name: {
                uri: "https://example.com/test.pdf",
                description: "Missing name field"
            },
            // Missing required 'uri' field - should trigger 400
            missing_uri: {
                name: "Test Document",
                description: "Missing uri field"
            },
            // Empty name - should trigger 400
            empty_name: {
                name: "",
                uri: "https://example.com/test.pdf"
            },
            // Name too long (> 256 chars) - should trigger 400
            name_too_long: {
                name: "x".repeat(257),
                uri: "https://example.com/test.pdf"
            },
            // URI too long (> 1000 chars) - should trigger 400
            uri_too_long: {
                name: "Valid Name",
                uri: "https://example.com/" + "x".repeat(1000)
            },
            // Invalid URI format - should trigger 400
            invalid_uri: {
                name: "Valid Name",
                uri: "not-a-valid-uri"
            },
            // Description too long (> 1024 chars) - should trigger 400
            description_too_long: {
                name: "Valid Name",
                uri: "https://example.com/test.pdf",
                description: "x".repeat(1025)
            },
            // Invalid characters in name - should trigger 400
            invalid_name_chars: {
                name: "Test<script>alert('xss')</script>",
                uri: "https://example.com/test.pdf"
            },
            // Wrong data types - should trigger 400
            wrong_types: {
                name: 123, // should be string
                uri: true, // should be string
                description: [] // should be string
            }
        };
    }

    generateDocumentList(count = 5, options = {}) {
        const documents = [];
        for (let i = 0; i < count; i++) {
            documents.push(this.validDocument({
                name: `Bulk Document ${i + 1} - ${this.testRunId}`,
                uri: `https://example.com/documents/bulk-${i + 1}-${this.testRunId}.pdf`,
                description: `Generated document ${i + 1} for bulk operations testing`,
                ...options
            }));
        }
        return documents;
    }

    // ========================================
    // REPOSITORY TEST DATA (renamed from SOURCE per OpenAPI spec)
    // ========================================

    validSource(options = {}) {
        return this.validRepository(options);
    }

    validRepository(options = {}) {
        return {
            name: options.name || `Test Repository ${this.testRunId}`,
            type: options.type || "git",
            uri: options.uri || options.url || `https://github.com/example/test-repo-${this.testRunId}.git`,
            description: options.description || "A test repository for comprehensive API testing",
            parameters: options.parameters || {
                refType: "branch",
                refValue: "main"
            },
            ...options.additional
        };
    }

    invalidSourceData() {
        return this.invalidRepositoryData();
    }

    invalidRepositoryData() {
        return {
            // Missing required 'uri' field - should trigger 400
            missing_uri: {
                name: "Test Repository",
                type: "git",
                description: "Missing uri field"
            },
            // Invalid URI format - should trigger 400
            invalid_uri: {
                name: "Test Repository",
                type: "git",
                uri: "not-a-valid-uri"
            },
            // Invalid type - should trigger 400
            invalid_type: {
                name: "Valid Name",
                uri: "https://github.com/example/test.git",
                type: "invalid-type"
            },
            // Name too long (> 256 chars) - should trigger 400
            name_too_long: {
                name: "x".repeat(257),
                type: "git",
                uri: "https://github.com/example/test.git"
            },
            // URI too long (> 1000 chars) - should trigger 400
            uri_too_long: {
                name: "Valid Name",
                type: "git",
                uri: "https://github.com/" + "x".repeat(1000)
            },
            // Description too long (> 1024 chars) - should trigger 400
            description_too_long: {
                name: "Valid Name",
                type: "git",
                uri: "https://github.com/example/test.git",
                description: "x".repeat(1025)
            },
            // Invalid parameters - should trigger 400
            invalid_parameters: {
                name: "Valid Name",
                type: "git",
                uri: "https://github.com/example/test.git",
                parameters: {
                    refType: "invalid-ref",
                    refValue: "main"
                }
            },
            // Missing required parameter fields - should trigger 400
            missing_param_fields: {
                name: "Valid Name",
                type: "git",
                uri: "https://github.com/example/test.git",
                parameters: {
                    refType: "branch"
                    // missing refValue
                }
            },
            // Wrong data types - should trigger 400
            wrong_types: {
                name: 123, // should be string
                type: true, // should be string
                uri: [], // should be string
                description: {} // should be string
            }
        };
    }

    generateSourceList(count = 5, options = {}) {
        return this.generateRepositoryList(count, options);
    }

    generateRepositoryList(count = 5, options = {}) {
        const repositories = [];
        const types = ["git", "svn", "mercurial", "other"];
        const refTypes = ["branch", "tag", "commit"];
        for (let i = 0; i < count; i++) {
            repositories.push(this.validRepository({
                name: `Bulk Repository ${i + 1} - ${this.testRunId}`,
                type: this.randomChoice(types),
                uri: `https://github.com/example/bulk-${i + 1}-${this.testRunId}.git`,
                description: `Generated repository ${i + 1} for bulk operations testing`,
                parameters: {
                    refType: this.randomChoice(refTypes),
                    refValue: i % 2 === 0 ? "main" : `v${i}.0`
                },
                ...options
            }));
        }
        return repositories;
    }

    // ========================================
    // TEST SCENARIO GENERATORS
    // ========================================

    generatePermissionTestMatrix() {
        return {
            alice: "owner",   // Creates resources, should have full access
            bob: "writer",    // Should have read/write but not delete
            charlie: "reader", // Should have read-only access
            diana: "none"     // Should have no access (403s)
        };
    }

    generateStatusCodeTestData() {
        return {
            "200": "Valid requests with proper authentication and permissions",
            "201": "Successful creation requests",
            "204": "Successful deletion requests", 
            "400": "Invalid request data, malformed JSON, missing required fields",
            "401": "Missing or invalid authentication token",
            "403": "Valid token but insufficient permissions for resource",
            "404": "Resource not found (non-existent IDs)",
            "409": "Conflict scenarios (collaboration sessions)",
            "422": "Validation errors (diagram patch failures)", 
            "500": "Server errors (edge cases, invalid states)"
        };
    }

    // ========================================
    // UTILITY METHODS FOR TESTS
    // ========================================

    randomChoice(array) {
        return array[Math.floor(Math.random() * array.length)];
    }

    generateInvalidUUID() {
        return "not-a-valid-uuid";
    }

    generateNonExistentUUID() {
        return "00000000-0000-0000-0000-000000000000";
    }

    generateLargePayload(size = 1000000) {
        return {
            name: "Large Payload Test",
            description: "x".repeat(size),
            threat_model_framework: "STRIDE"
        };
    }
}

// Export for use in Postman pre-request scripts
if (typeof module !== 'undefined') {
    module.exports = TMITestDataFactory;
}

// Global factory instance for Postman
if (typeof global !== 'undefined') {
    global.TMITestDataFactory = TMITestDataFactory;
}

// Browser global for testing
if (typeof window !== 'undefined') {
    window.TMITestDataFactory = TMITestDataFactory;
}