/**
 * Multi-User OAuth Authentication Helper for TMI API Testing
 * 
 * This script provides utilities for managing multiple user tokens
 * for comprehensive permission and access control testing
 */

class TMIMultiUserAuth {
    constructor() {
        this.TMI_URL = pm.variables.get('baseUrl') || 'http://127.0.0.1:8080';
        this.STUB_URL = pm.variables.get('oauthStubUrl') || 'http://127.0.0.1:8079';
        
        // Test users for different permission scenarios
        this.users = {
            alice: { role: 'owner', loginHint: 'alice' },
            bob: { role: 'writer', loginHint: 'bob' }, 
            charlie: { role: 'reader', loginHint: 'charlie' },
            diana: { role: 'none', loginHint: 'diana' }
        };
        
        // Cache tokens to avoid excessive OAuth calls
        this.tokenCache = {};
    }

    /**
     * Check if a JWT token is valid (not expired)
     */
    isTokenValid(token) {
        if (!token) return false;
        try {
            const parts = token.split('.');
            if (parts.length !== 3) return false;
            const payload = JSON.parse(Buffer.from(parts[1], 'base64').toString());
            const now = Math.floor(Date.now() / 1000);
            return payload.exp && now < payload.exp - 60; // 60 second buffer
        } catch (e) {
            return false;
        }
    }

    /**
     * Get cached token for user if valid, otherwise return null
     */
    getCachedToken(username) {
        const cacheKey = `token_${username}`;
        const token = pm.collectionVariables.get(cacheKey);
        return this.isTokenValid(token) ? token : null;
    }

    /**
     * Cache token for user
     */
    setCachedToken(username, token) {
        const cacheKey = `token_${username}`;
        pm.collectionVariables.set(cacheKey, token);
    }

    /**
     * Authenticate a specific user and get their token
     */
    async authenticateUser(username) {
        console.log(`🔐 Authenticating user: ${username}`);
        
        // Check cache first
        const cachedToken = this.getCachedToken(username);
        if (cachedToken) {
            console.log(`✓ Using cached token for ${username}`);
            return cachedToken;
        }

        const user = this.users[username];
        if (!user) {
            throw new Error(`Unknown user: ${username}`);
        }

        return new Promise((resolve, reject) => {
            // Step 1: Trigger OAuth flow
            pm.sendRequest({
                url: `${this.TMI_URL}/oauth2/authorize?idp=tmi&login_hint=${user.loginHint}&client_callback=${this.STUB_URL}/&scope=openid`,
                method: 'GET'
            }, (err, response) => {
                if (err) {
                    console.error(`❌ OAuth initiation failed for ${username}:`, err);
                    reject(err);
                    return;
                }
                
                // Step 2: Wait for redirect to complete
                setTimeout(() => {
                    // Step 3: Get credentials from stub
                    pm.sendRequest({
                        url: `${this.STUB_URL}/creds?userid=${user.loginHint}`,
                        method: 'GET'
                    }, (err, response) => {
                        if (err) {
                            console.error(`❌ Failed to get credentials for ${username}:`, err);
                            reject(err);
                            return;
                        }
                        
                        if (response.code === 404) {
                            console.error(`❌ No credentials found for ${username}`);
                            reject(new Error(`No credentials found for ${username}`));
                            return;
                        }
                        
                        const creds = response.json();
                        if (creds.access_token) {
                            console.log(`✓ Token obtained for ${username}`);
                            this.setCachedToken(username, creds.access_token);
                            resolve(creds.access_token);
                        } else {
                            console.error(`❌ No access token in response for ${username}:`, creds);
                            reject(new Error(`No access token for ${username}`));
                        }
                    });
                }, 2000); // Wait for OAuth redirect
            });
        });
    }

    /**
     * Authenticate all test users and cache their tokens
     */
    async authenticateAllUsers() {
        console.log('🔐 Authenticating all test users...');
        const tokens = {};
        
        for (const username of Object.keys(this.users)) {
            try {
                tokens[username] = await this.authenticateUser(username);
                // Small delay between user authentications
                await new Promise(resolve => setTimeout(resolve, 1000));
            } catch (error) {
                console.error(`❌ Failed to authenticate ${username}:`, error);
                tokens[username] = null;
            }
        }
        
        console.log('✓ User authentication completed');
        return tokens;
    }

    /**
     * Set the active user for the current request
     */
    setActiveUser(username) {
        const token = this.getCachedToken(username);
        if (!token) {
            throw new Error(`No valid token for user: ${username}. Run authenticateUser(${username}) first.`);
        }
        
        pm.collectionVariables.set('access_token', token);
        pm.collectionVariables.set('currentUser', username);
        console.log(`✓ Active user set to: ${username}`);
        return token;
    }

    /**
     * Get current active user
     */
    getCurrentUser() {
        return pm.collectionVariables.get('currentUser') || 'unknown';
    }

    /**
     * Switch to a different user for testing
     */
    switchUser(username) {
        console.log(`🔄 Switching from ${this.getCurrentUser()} to ${username}`);
        return this.setActiveUser(username);
    }

    /**
     * Clear all cached tokens (force re-authentication)
     */
    clearTokenCache() {
        console.log('🧹 Clearing token cache');
        Object.keys(this.users).forEach(username => {
            pm.collectionVariables.unset(`token_${username}`);
        });
        pm.collectionVariables.unset('access_token');
        pm.collectionVariables.unset('currentUser');
    }

    /**
     * Get token for user without setting as active
     */
    getUserToken(username) {
        return this.getCachedToken(username);
    }

    /**
     * Create resource ownership matrix for testing
     */
    createPermissionTestData() {
        return {
            // Alice creates resources (becomes owner)
            resources_created_by_alice: [],
            // Bob creates resources (becomes owner) 
            resources_created_by_bob: [],
            // Permission test scenarios
            scenarios: {
                owner_full_access: {
                    user: 'alice',
                    resource_owner: 'alice',
                    expected_status: [200, 201, 204]
                },
                writer_read_write: {
                    user: 'bob', 
                    resource_owner: 'alice',
                    expected_status: [200, 201] // Can read/write, not delete
                },
                reader_read_only: {
                    user: 'charlie',
                    resource_owner: 'alice', 
                    expected_status: [200] // Can only read
                },
                no_access_forbidden: {
                    user: 'diana',
                    resource_owner: 'alice',
                    expected_status: [403] // No access
                }
            }
        };
    }

    /**
     * Prepare environment for multi-user testing
     */
    async prepareMultiUserTest() {
        console.log('🚀 Preparing multi-user test environment');
        
        // Clear any existing tokens
        this.clearTokenCache();
        
        // Authenticate all users
        const tokens = await this.authenticateAllUsers();
        
        // Set default user (Alice) as active
        if (tokens.alice) {
            this.setActiveUser('alice');
        }
        
        console.log('✅ Multi-user test environment ready');
        return tokens;
    }

    /**
     * Store resource ID for permission testing
     */
    storeResourceForPermissionTest(resourceType, resourceId, owner = null) {
        const currentUser = owner || this.getCurrentUser();
        const key = `${resourceType}_${currentUser}`;
        let resources = pm.collectionVariables.get(key);
        
        if (!resources) {
            resources = [];
        } else {
            resources = JSON.parse(resources);
        }
        
        resources.push(resourceId);
        pm.collectionVariables.set(key, JSON.stringify(resources));
        
        console.log(`📝 Stored ${resourceType} ${resourceId} owned by ${currentUser}`);
    }

    /**
     * Get resources created by a specific user
     */
    getResourcesByUser(resourceType, username) {
        const key = `${resourceType}_${username}`;
        const resources = pm.collectionVariables.get(key);
        return resources ? JSON.parse(resources) : [];
    }

    /**
     * Clean up test resources
     */
    cleanupTestResources() {
        console.log('🧹 Cleaning up test resources');
        const resourceTypes = ['threat_model', 'threat', 'diagram', 'document', 'source'];
        const users = Object.keys(this.users);
        
        resourceTypes.forEach(type => {
            users.forEach(user => {
                pm.collectionVariables.unset(`${type}_${user}`);
            });
        });
    }
}

// Global instance for use in Postman scripts
if (typeof global !== 'undefined') {
    global.tmiAuth = new TMIMultiUserAuth();
}

// Export for Node.js environments
if (typeof module !== 'undefined') {
    module.exports = TMIMultiUserAuth;
}