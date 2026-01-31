# ============================================================================
# Integration Test Framework - New OpenAPI-driven testing
# ============================================================================
# Location: test/integration/
# Output: test/outputs/integration/
# Documentation: test/integration/README.md
# ============================================================================

.PHONY: test-integration-new test-integration-workflow test-integration-quick test-clean test-outputs-clean
.PHONY: test-integration-tier1 test-integration-tier2 test-integration-tier3 test-integration-all

# Run all integration tests (new framework)
test-integration-new: start-oauth-stub
	$(call log_info,"Running integration tests (new framework)...")
	@mkdir -p test/outputs/integration
	@INTEGRATION_TESTS=true \
	TMI_SERVER_URL=http://localhost:8080 \
	OAUTH_STUB_URL=http://localhost:8079 \
	go test -v ./test/integration/workflows/... \
		-timeout=5m \
		2>&1 | tee test/outputs/integration/test-run-$$(date +%Y%m%d_%H%M%S).log
	$(call log_success,"Integration tests completed")

# ============================================================================
# Tier-Based Testing - Progressive Test Coverage
# ============================================================================

# Tier 1: Core Workflows (CI/CD - Every Commit)
# Target: < 2 minutes | 36 operations (21%)
test-integration-tier1: start-oauth-stub
	$(call log_info,"Running Tier 1 integration tests - Core Workflows")
	@mkdir -p test/outputs/integration
	@cd test/integration && INTEGRATION_TESTS=true \
	TMI_SERVER_URL=http://localhost:8080 \
	OAUTH_STUB_URL=http://localhost:8079 \
	go test -v ./workflows \
		-run 'TestOAuthFlow|TestThreatModelCRUD|TestDiagramCRUD|TestUserOperations' \
		-timeout=3m \
		2>&1 | tee ../../test/outputs/integration/tier1-$$(date +%Y%m%d_%H%M%S).log
	$(call log_success,"Tier 1 tests completed - OAuth ThreatModel CRUD Diagram CRUD User ops")

# Tier 2: Feature Tests (Nightly)
# Target: < 10 minutes | 105 operations (60%)
test-integration-tier2: start-oauth-stub
	$(call log_info,"Running Tier 2 integration tests - Feature Coverage")
	@mkdir -p test/outputs/integration
	@INTEGRATION_TESTS=true \
	TMI_SERVER_URL=http://localhost:8080 \
	OAUTH_STUB_URL=http://localhost:8079 \
	go test -v ./test/integration/workflows/tier2_features/... \
		-timeout=12m \
		2>&1 | tee test/outputs/integration/tier2-$$(date +%Y%m%d_%H%M%S).log
	$(call log_success,"Tier 2 tests completed - Metadata Assets Documents Webhooks etc")

# Tier 3: Edge Cases & Admin (Weekly)
# Target: < 15 minutes | 33 operations (19%)
test-integration-tier3: start-oauth-stub
	$(call log_info,"Running Tier 3 integration tests - Edge Cases and Admin")
	@mkdir -p test/outputs/integration
	@INTEGRATION_TESTS=true \
	TMI_SERVER_URL=http://localhost:8080 \
	OAUTH_STUB_URL=http://localhost:8079 \
	go test -v ./test/integration/workflows/tier3_edge_cases/... \
		-timeout=17m \
		2>&1 | tee test/outputs/integration/tier3-$$(date +%Y%m%d_%H%M%S).log
	$(call log_success,"Tier 3 tests completed - Admin Authorization Pagination Errors")

# Run all tiers sequentially (100% coverage)
# Target: < 27 minutes | 174 operations (100%)
test-integration-all: start-oauth-stub
	$(call log_info,"Running ALL integration tests - Tiers 1-3")
	@mkdir -p test/outputs/integration
	@echo -e "$(BLUE)[INFO]$(NC) Running Tier 1 - Core Workflows"
	@$(MAKE) -f $(MAKEFILE_LIST) test-integration-tier1
	@echo -e "$(BLUE)[INFO]$(NC) Running Tier 2 - Feature Coverage"
	@$(MAKE) -f $(MAKEFILE_LIST) test-integration-tier2
	@echo -e "$(BLUE)[INFO]$(NC) Running Tier 3 - Edge Cases and Admin"
	@$(MAKE) -f $(MAKEFILE_LIST) test-integration-tier3
	$(call log_success,"All integration tests completed - 100 percent coverage 174 operations")

# Run specific integration test workflow
test-integration-workflow: start-oauth-stub
	$(call log_info,"Running integration test workflow: $(WORKFLOW)...")
	@if [ -z "$(WORKFLOW)" ]; then \
		echo -e "$(RED)[ERROR]$(NC) WORKFLOW parameter required"; \
		echo -e "$(BLUE)[INFO]$(NC) Usage: make test-integration-workflow WORKFLOW=oauth_flow"; \
		exit 1; \
	fi
	@mkdir -p test/outputs/integration
	@INTEGRATION_TESTS=true \
	TMI_SERVER_URL=http://localhost:8080 \
	OAUTH_STUB_URL=http://localhost:8079 \
	go test -v ./test/integration/workflows -run Test$(WORKFLOW) \
		-timeout=2m \
		2>&1 | tee test/outputs/integration/$(WORKFLOW)-$$(date +%Y%m%d_%H%M%S).log
	$(call log_success,"Workflow test completed")

# Quick integration test (just example test, no full setup)
test-integration-quick: start-oauth-stub
	$(call log_info,"Running quick integration test...")
	@INTEGRATION_TESTS=true \
	TMI_SERVER_URL=http://localhost:8080 \
	OAUTH_STUB_URL=http://localhost:8079 \
	go test -v ./test/integration/workflows -run TestExample -timeout=1m

# Full integration test setup (server + oauth-stub + tests)
test-integration-full:
	$(call log_info,"Starting full integration test suite...")
	@trap '$(MAKE) -f $(MAKEFILE_LIST) clean-everything; make stop-oauth-stub' EXIT; \
	$(MAKE) -f $(MAKEFILE_LIST) clean-everything && \
	$(MAKE) -f $(MAKEFILE_LIST) start-database && \
	$(MAKE) -f $(MAKEFILE_LIST) start-redis && \
	$(MAKE) -f $(MAKEFILE_LIST) wait-database && \
	$(MAKE) -f $(MAKEFILE_LIST) migrate-database && \
	$(MAKE) -f $(MAKEFILE_LIST) start-oauth-stub && \
	sleep 2 && \
	SERVER_CONFIG_FILE=config-development.yml $(MAKE) -f $(MAKEFILE_LIST) start-server && \
	sleep 3 && \
	$(MAKE) -f $(MAKEFILE_LIST) test-integration-new
	$(call log_success,"Full integration test suite completed")

# Clean all test outputs
test-clean:
	$(call log_info,"Cleaning test outputs...")
	@rm -rf test/outputs/integration/*
	@rm -rf test/outputs/unit/*
	@rm -rf test/outputs/cats/*
	@rm -rf test/outputs/newman/*
	@rm -rf test/outputs/security/*
	@rm -rf test/outputs/wstest/*
	$(call log_success,"Test outputs cleaned")

# Clean only integration test outputs
test-outputs-clean-integration:
	$(call log_info,"Cleaning integration test outputs...")
	@rm -rf test/outputs/integration/*
	$(call log_success,"Integration test outputs cleaned")

# Validate integration test framework setup
test-framework-check:
	$(call log_info,"Validating integration test framework...")
	@echo -e "$(BLUE)[INFO]$(NC) Checking directory structure..."
	@test -d test/integration/framework || (echo -e "$(RED)[ERROR]$(NC) test/integration/framework missing" && exit 1)
	@test -d test/integration/workflows || (echo -e "$(RED)[ERROR]$(NC) test/integration/workflows missing" && exit 1)
	@test -d test/integration/spec || (echo -e "$(RED)[ERROR]$(NC) test/integration/spec missing" && exit 1)
	@test -d test/outputs/integration || mkdir -p test/outputs/integration
	@echo -e "$(BLUE)[INFO]$(NC) Checking OAuth stub..."
	@if ! pgrep -f "oauth-client-callback-stub.py" > /dev/null; then \
		echo -e "$(YELLOW)[WARNING]$(NC) OAuth stub not running (run 'make start-oauth-stub')"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) OAuth stub is running"; \
	fi
	@echo -e "$(BLUE)[INFO]$(NC) Checking server..."
	@if ! curl -s http://localhost:8080/ > /dev/null 2>&1; then \
		echo -e "$(YELLOW)[WARNING]$(NC) Server not running (run 'make start-dev')"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) Server is running"; \
	fi
	@echo -e "$(BLUE)[INFO]$(NC) Checking OpenAPI spec..."
	@test -f docs/reference/apis/tmi-openapi.json || (echo -e "$(RED)[ERROR]$(NC) OpenAPI spec missing" && exit 1)
	@echo -e "$(GREEN)[SUCCESS]$(NC) Integration test framework setup validated"

# Help for integration test commands
test-help:
	@echo "Integration Test Framework Commands:"
	@echo ""
	@echo "  Setup:"
	@echo "    make test-framework-check    - Validate framework setup"
	@echo "    make start-oauth-stub        - Start OAuth callback stub"
	@echo "    make stop-oauth-stub         - Stop OAuth callback stub"
	@echo ""
	@echo "  Running Tests (Tier-Based):"
	@echo "    make test-integration-tier1  - Tier 1: Core workflows (< 2 min, 36 ops)"
	@echo "    make test-integration-tier2  - Tier 2: Feature tests (< 10 min, 105 ops)"
	@echo "    make test-integration-tier3  - Tier 3: Edge cases & admin (< 15 min, 33 ops)"
	@echo "    make test-integration-all    - Run all tiers (< 27 min, 174 ops - 100%)"
	@echo ""
	@echo "  Running Tests (Legacy):"
	@echo "    make test-integration-new    - Run all integration tests"
	@echo "    make test-integration-quick  - Run quick example test"
	@echo "    make test-integration-full   - Full setup + run all tests"
	@echo "    make test-integration-workflow WORKFLOW=name - Run specific workflow"
	@echo ""
	@echo "  Tier Descriptions:"
	@echo "    Tier 1: OAuth, ThreatModel CRUD, Diagram CRUD, User operations"
	@echo "    Tier 2: Metadata, Assets, Documents, Webhooks, Addons, SAML, etc."
	@echo "    Tier 3: Admin operations, Authorization, Pagination, Error handling"
	@echo ""
	@echo "  Examples:"
	@echo "    make test-integration-tier1"
	@echo "    make test-integration-workflow WORKFLOW=OAuthFlow"
	@echo "    make test-integration-workflow WORKFLOW=ThreatModelCRUD"
	@echo ""
	@echo "  Cleanup:"
	@echo "    make test-clean              - Clean all test outputs"
	@echo "    make test-outputs-clean-integration - Clean integration outputs only"
	@echo ""
	@echo "  Documentation:"
	@echo "    cat test/integration/README.md"
	@echo "    cat docs/developer/testing/integration-test-plan.md"
