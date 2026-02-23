#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = []
# ///

"""
CATS Test Results Parser

Parses CATS fuzzer test result JSON files into a normalized SQLite database
for efficient analysis and reporting.

Usage:
    uv run scripts/parse-cats-results.py --input test/outputs/cats/report/ --output test/outputs/cats/cats-results.db
"""

import sqlite3
import json
import logging
import argparse
import sys
from pathlib import Path
from typing import Dict, Optional, Tuple, Union
from contextlib import contextmanager

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S'
)
logger = logging.getLogger(__name__)


class CATSResultsParser:
    """Parse CATS test results into normalized SQLite database"""

    # Schema SQL statements
    SCHEMA_SQL = """
    -- Lookup tables
    CREATE TABLE IF NOT EXISTS result_types (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL UNIQUE
    );

    CREATE TABLE IF NOT EXISTS fuzzers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL UNIQUE
    );

    CREATE TABLE IF NOT EXISTS servers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        base_url TEXT NOT NULL UNIQUE
    );

    CREATE TABLE IF NOT EXISTS paths (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        path TEXT NOT NULL UNIQUE,
        contract_path TEXT
    );

    CREATE TABLE IF NOT EXISTS http_methods (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        method TEXT NOT NULL UNIQUE
    );

    -- Main tables
    CREATE TABLE IF NOT EXISTS tests (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        test_id TEXT NOT NULL UNIQUE,
        test_number INTEGER NOT NULL,
        trace_id TEXT NOT NULL,
        scenario TEXT NOT NULL,
        expected_result TEXT NOT NULL,
        result_type_id INTEGER NOT NULL,
        fuzzer_id INTEGER NOT NULL,
        server_id INTEGER NOT NULL,
        path_id INTEGER NOT NULL,
        result_reason TEXT,
        result_details TEXT,
        source_file TEXT NOT NULL,
        is_false_positive BOOLEAN DEFAULT 0,
        fp_rule TEXT,
        FOREIGN KEY (result_type_id) REFERENCES result_types(id),
        FOREIGN KEY (fuzzer_id) REFERENCES fuzzers(id),
        FOREIGN KEY (server_id) REFERENCES servers(id),
        FOREIGN KEY (path_id) REFERENCES paths(id)
    );

    CREATE TABLE IF NOT EXISTS requests (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        test_id INTEGER NOT NULL UNIQUE,
        http_method_id INTEGER NOT NULL,
        url TEXT NOT NULL,
        timestamp TEXT NOT NULL,
        FOREIGN KEY (test_id) REFERENCES tests(id) ON DELETE CASCADE,
        FOREIGN KEY (http_method_id) REFERENCES http_methods(id)
    );

    CREATE TABLE IF NOT EXISTS responses (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        test_id INTEGER NOT NULL UNIQUE,
        http_method_id INTEGER NOT NULL,
        response_code INTEGER NOT NULL,
        response_time_ms INTEGER,
        num_words INTEGER,
        num_lines INTEGER,
        content_length_bytes INTEGER,
        response_content_type TEXT,
        FOREIGN KEY (test_id) REFERENCES tests(id) ON DELETE CASCADE,
        FOREIGN KEY (http_method_id) REFERENCES http_methods(id)
    );

    CREATE TABLE IF NOT EXISTS request_headers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        request_id INTEGER NOT NULL,
        header_key TEXT NOT NULL,
        header_value TEXT NOT NULL,
        header_order INTEGER NOT NULL,
        FOREIGN KEY (request_id) REFERENCES requests(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS response_headers (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        response_id INTEGER NOT NULL,
        header_key TEXT NOT NULL,
        header_value TEXT NOT NULL,
        header_order INTEGER NOT NULL,
        FOREIGN KEY (response_id) REFERENCES responses(id) ON DELETE CASCADE
    );
    """

    INDEX_SQL = """
    -- Indexes on tests table
    CREATE INDEX IF NOT EXISTS idx_tests_result_type ON tests(result_type_id);
    CREATE INDEX IF NOT EXISTS idx_tests_fuzzer ON tests(fuzzer_id);
    CREATE INDEX IF NOT EXISTS idx_tests_path ON tests(path_id);
    CREATE INDEX IF NOT EXISTS idx_tests_test_number ON tests(test_number);
    CREATE INDEX IF NOT EXISTS idx_tests_result_fuzzer ON tests(result_type_id, fuzzer_id);
    CREATE INDEX IF NOT EXISTS idx_tests_fuzzer_path ON tests(fuzzer_id, path_id);
    CREATE INDEX IF NOT EXISTS idx_tests_false_positive ON tests(is_false_positive);
    CREATE INDEX IF NOT EXISTS idx_tests_fp_rule ON tests(fp_rule);

    -- Indexes on requests table
    CREATE INDEX IF NOT EXISTS idx_requests_test_id ON requests(test_id);
    CREATE INDEX IF NOT EXISTS idx_requests_method ON requests(http_method_id);

    -- Indexes on responses table
    CREATE INDEX IF NOT EXISTS idx_responses_test_id ON responses(test_id);
    CREATE INDEX IF NOT EXISTS idx_responses_code ON responses(response_code);
    CREATE INDEX IF NOT EXISTS idx_responses_time ON responses(response_time_ms);
    CREATE INDEX IF NOT EXISTS idx_responses_code_time ON responses(response_code, response_time_ms);

    -- Indexes on headers tables
    CREATE INDEX IF NOT EXISTS idx_req_headers_request_id ON request_headers(request_id);
    CREATE INDEX IF NOT EXISTS idx_req_headers_key ON request_headers(header_key);
    CREATE INDEX IF NOT EXISTS idx_req_headers_key_value ON request_headers(header_key, header_value);
    CREATE INDEX IF NOT EXISTS idx_resp_headers_response_id ON response_headers(response_id);
    CREATE INDEX IF NOT EXISTS idx_resp_headers_key ON response_headers(header_key);
    CREATE INDEX IF NOT EXISTS idx_resp_headers_key_value ON response_headers(header_key, header_value);
    """

    VIEWS_SQL = """
    -- Simplified test results view (includes all tests)
    CREATE VIEW IF NOT EXISTS test_results_view AS
    SELECT
        t.test_id,
        t.test_number,
        t.trace_id,
        rt.name AS result,
        f.name AS fuzzer,
        p.path,
        p.contract_path,
        s.base_url AS server,
        m.method AS http_method,
        r.response_code,
        r.response_time_ms,
        t.scenario,
        t.expected_result,
        t.result_reason,
        t.source_file,
        t.is_false_positive,
        t.fp_rule
    FROM tests t
    JOIN result_types rt ON t.result_type_id = rt.id
    JOIN fuzzers f ON t.fuzzer_id = f.id
    JOIN paths p ON t.path_id = p.id
    JOIN servers s ON t.server_id = s.id
    JOIN requests req ON t.id = req.test_id
    JOIN http_methods m ON req.http_method_id = m.id
    JOIN responses r ON t.id = r.test_id;

    -- Filtered test results view (excludes false positives)
    CREATE VIEW IF NOT EXISTS test_results_filtered_view AS
    SELECT *
    FROM test_results_view
    WHERE is_false_positive = 0;

    -- False positive statistics by rule
    CREATE VIEW IF NOT EXISTS fp_rule_stats_view AS
    SELECT
        fp_rule,
        COUNT(*) AS count,
        ROUND(100.0 * COUNT(*) / (SELECT COUNT(*) FROM tests), 2) AS pct_of_total,
        ROUND(100.0 * COUNT(*) / (SELECT COUNT(*) FROM tests WHERE is_false_positive = 1), 2) AS pct_of_fps
    FROM tests
    WHERE is_false_positive = 1
    GROUP BY fp_rule
    ORDER BY count DESC;

    -- Fuzzer statistics view
    CREATE VIEW IF NOT EXISTS fuzzer_stats_view AS
    SELECT
        f.name AS fuzzer,
        rt.name AS result,
        COUNT(*) AS count,
        ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (PARTITION BY f.name), 2) AS percentage,
        AVG(r.response_time_ms) AS avg_response_time_ms
    FROM tests t
    JOIN fuzzers f ON t.fuzzer_id = f.id
    JOIN result_types rt ON t.result_type_id = rt.id
    JOIN responses r ON t.id = r.test_id
    GROUP BY f.name, rt.name;

    -- Path error analysis view
    CREATE VIEW IF NOT EXISTS path_error_analysis_view AS
    SELECT
        p.path,
        m.method AS http_method,
        COUNT(*) AS total_tests,
        SUM(CASE WHEN rt.name = 'error' THEN 1 ELSE 0 END) AS errors,
        SUM(CASE WHEN rt.name = 'warn' THEN 1 ELSE 0 END) AS warnings,
        SUM(CASE WHEN rt.name = 'success' THEN 1 ELSE 0 END) AS successes,
        ROUND(100.0 * SUM(CASE WHEN rt.name = 'error' THEN 1 ELSE 0 END) / COUNT(*), 2) AS error_rate
    FROM tests t
    JOIN paths p ON t.path_id = p.id
    JOIN result_types rt ON t.result_type_id = rt.id
    JOIN requests req ON t.id = req.test_id
    JOIN http_methods m ON req.http_method_id = m.id
    GROUP BY p.path, m.method;

    -- Response code distribution view
    CREATE VIEW IF NOT EXISTS response_code_stats_view AS
    SELECT
        r.response_code,
        rt.name AS result,
        COUNT(*) AS count,
        AVG(r.response_time_ms) AS avg_time_ms,
        MIN(r.response_time_ms) AS min_time_ms,
        MAX(r.response_time_ms) AS max_time_ms
    FROM responses r
    JOIN tests t ON r.test_id = t.id
    JOIN result_types rt ON t.result_type_id = rt.id
    GROUP BY r.response_code, rt.name
    ORDER BY r.response_code;
    """

    def __init__(self, db_path: str):
        self.db_path = db_path
        self._conn: Optional[sqlite3.Connection] = None

        # Lookup caches to avoid repeated DB queries
        self.result_type_cache: Dict[str, int] = {}
        self.fuzzer_cache: Dict[str, int] = {}
        self.server_cache: Dict[str, int] = {}
        self.path_cache: Dict[Tuple[str, str], int] = {}  # (path, contract_path) -> id
        self.method_cache: Dict[str, int] = {}

        # Statistics
        self.stats = {
            'processed': 0,
            'errors': 0,
            'skipped': 0
        }

    @property
    def conn(self) -> sqlite3.Connection:
        """Get the database connection, raising if not connected."""
        if self._conn is None:
            raise RuntimeError("Database not connected. Call connect() first.")
        return self._conn

    def connect(self):
        """Establish database connection with optimizations"""
        logger.info(f"Connecting to database: {self.db_path}")
        self._conn = sqlite3.connect(self.db_path)
        self._conn.execute("PRAGMA foreign_keys = ON")
        self._conn.execute("PRAGMA journal_mode = WAL")
        self._conn.execute("PRAGMA synchronous = NORMAL")
        self._conn.execute("PRAGMA cache_size = -64000")  # 64MB cache
        self._conn.execute("PRAGMA temp_store = MEMORY")
        logger.info("Database connection established with optimizations")

    def close(self):
        """Close database connection"""
        if self._conn:
            self._conn.close()
            self._conn = None
            logger.info("Database connection closed")

    @contextmanager
    def transaction(self):
        """Context manager for database transactions"""
        try:
            yield self.conn
            self.conn.commit()
        except Exception as e:
            self.conn.rollback()
            logger.error(f"Transaction rolled back: {e}")
            raise

    def create_schema(self):
        """Create all tables, indexes, and views"""
        logger.info("Creating database schema...")

        # Create tables (executescript auto-commits, so run separately)
        self.conn.executescript(self.SCHEMA_SQL)
        logger.info("Tables created")

        # Create indexes (executescript auto-commits, so run separately)
        self.conn.executescript(self.INDEX_SQL)
        logger.info("Indexes created")

        # Create views (executescript auto-commits, so run separately)
        self.conn.executescript(self.VIEWS_SQL)
        logger.info("Views created")

        logger.info("Schema creation complete")

    def _load_caches(self):
        """Load lookup tables into memory caches"""
        cursor = self.conn.cursor()

        # Load result types
        cursor.execute("SELECT id, name FROM result_types")
        self.result_type_cache = {row[1]: row[0] for row in cursor.fetchall()}

        # Load fuzzers
        cursor.execute("SELECT id, name FROM fuzzers")
        self.fuzzer_cache = {row[1]: row[0] for row in cursor.fetchall()}

        # Load servers
        cursor.execute("SELECT id, base_url FROM servers")
        self.server_cache = {row[1]: row[0] for row in cursor.fetchall()}

        # Load paths
        cursor.execute("SELECT id, path, contract_path FROM paths")
        self.path_cache = {
            (row[1], row[2] or ''): row[0] for row in cursor.fetchall()
        }

        # Load methods
        cursor.execute("SELECT id, method FROM http_methods")
        self.method_cache = {row[1]: row[0] for row in cursor.fetchall()}

        logger.info(f"Loaded caches: {len(self.result_type_cache)} result types, "
                   f"{len(self.fuzzer_cache)} fuzzers, {len(self.server_cache)} servers, "
                   f"{len(self.path_cache)} paths, {len(self.method_cache)} methods")

    def get_or_create_result_type(self, name: str) -> int:
        """Get or create result type, using cache"""
        if name in self.result_type_cache:
            return self.result_type_cache[name]

        cursor = self.conn.execute(
            "INSERT OR IGNORE INTO result_types (name) VALUES (?)",
            (name,)
        )
        cursor = self.conn.execute(
            "SELECT id FROM result_types WHERE name = ?",
            (name,)
        )
        result_id = cursor.fetchone()[0]
        self.result_type_cache[name] = result_id
        return result_id

    def get_or_create_fuzzer(self, name: str) -> int:
        """Get or create fuzzer, using cache"""
        if name in self.fuzzer_cache:
            return self.fuzzer_cache[name]

        self.conn.execute(
            "INSERT OR IGNORE INTO fuzzers (name) VALUES (?)",
            (name,)
        )
        cursor = self.conn.execute(
            "SELECT id FROM fuzzers WHERE name = ?",
            (name,)
        )
        fuzzer_id = cursor.fetchone()[0]
        self.fuzzer_cache[name] = fuzzer_id
        return fuzzer_id

    def get_or_create_server(self, url: str) -> int:
        """Get or create server, using cache"""
        if url in self.server_cache:
            return self.server_cache[url]

        self.conn.execute(
            "INSERT OR IGNORE INTO servers (base_url) VALUES (?)",
            (url,)
        )
        cursor = self.conn.execute(
            "SELECT id FROM servers WHERE base_url = ?",
            (url,)
        )
        server_id = cursor.fetchone()[0]
        self.server_cache[url] = server_id
        return server_id

    def get_or_create_path(self, path: str, contract_path: str = '') -> int:
        """Get or create path, using cache"""
        key = (path, contract_path or '')
        if key in self.path_cache:
            return self.path_cache[key]

        self.conn.execute(
            "INSERT OR IGNORE INTO paths (path, contract_path) VALUES (?, ?)",
            (path, contract_path or None)
        )
        cursor = self.conn.execute(
            "SELECT id FROM paths WHERE path = ? AND (contract_path = ? OR (contract_path IS NULL AND ? IS NULL))",
            (path, contract_path or None, contract_path or None)
        )
        path_id = cursor.fetchone()[0]
        self.path_cache[key] = path_id
        return path_id

    def get_or_create_method(self, method: str) -> int:
        """Get or create HTTP method, using cache"""
        if method in self.method_cache:
            return self.method_cache[method]

        self.conn.execute(
            "INSERT OR IGNORE INTO http_methods (method) VALUES (?)",
            (method,)
        )
        cursor = self.conn.execute(
            "SELECT id FROM http_methods WHERE method = ?",
            (method,)
        )
        method_id = cursor.fetchone()[0]
        self.method_cache[method] = method_id
        return method_id

    def parse_json_file(self, filepath: Path) -> Optional[Dict]:
        """Parse single JSON file"""
        try:
            with open(filepath, 'r', encoding='utf-8') as f:
                data = json.load(f)

            # Validate required fields
            required_fields = [
                'testId', 'traceId', 'scenario', 'expectedResult',
                'result', 'fuzzer', 'path', 'server', 'request', 'response'
            ]
            missing = [f for f in required_fields if f not in data]
            if missing:
                logger.warning(f"Missing fields in {filepath.name}: {missing}")
                return None

            return data

        except json.JSONDecodeError as e:
            logger.error(f"Invalid JSON in {filepath.name}: {e}")
            return None
        except Exception as e:
            logger.error(f"Error parsing {filepath.name}: {e}")
            return None

    def extract_test_number(self, test_id: str) -> int:
        """Extract numeric ID from test_id like 'Test 10002'"""
        try:
            return int(test_id.replace('Test ', '').strip())
        except ValueError:
            return 0

    # False positive rule IDs - used in fp_rule column for tracking
    FP_RULE_RATE_LIMIT = "RATE_LIMIT_429"
    FP_RULE_OAUTH_AUTH = "OAUTH_AUTH_401_403"
    FP_RULE_VALIDATION_400 = "VALIDATION_400"
    FP_RULE_NOT_FOUND_404 = "NOT_FOUND_404"
    FP_RULE_IDOR_ADMIN = "IDOR_ADMIN"
    FP_RULE_IDOR_LIST = "IDOR_LIST"
    FP_RULE_IDOR_OPTIONAL = "IDOR_OPTIONAL"
    FP_RULE_HTTP_METHODS = "HTTP_METHODS"
    FP_RULE_RESPONSE_CONTRACT = "RESPONSE_CONTRACT"
    FP_RULE_CONFLICT_409 = "CONFLICT_409"
    FP_RULE_CONTENT_TYPE_GO_HTTP = "CONTENT_TYPE_GO_HTTP"
    FP_RULE_INJECTION_JSON_API = "INJECTION_JSON_API"
    FP_RULE_XSS_QUERY_PARAMS = "XSS_QUERY_PARAMS"
    FP_RULE_HEADER_VALIDATION = "HEADER_VALIDATION_400"
    FP_RULE_LEADING_ZEROS = "LEADING_ZEROS_400"
    FP_RULE_CONNECTION_ERROR = "CONNECTION_ERROR_999"
    FP_RULE_STRING_BOUNDARY_OPTIONAL = "STRING_BOUNDARY_OPTIONAL"
    FP_RULE_TRANSFER_ENCODING = "TRANSFER_ENCODING_501"
    FP_RULE_DELETED_RESOURCE_LIST = "DELETED_RESOURCE_LIST"
    FP_RULE_FORM_URLENCODED_JSON_TEST = "FORM_URLENCODED_JSON_TEST"
    FP_RULE_DELETE_ME_CHALLENGE = "DELETE_ME_CHALLENGE"
    FP_RULE_ADMIN_SETTINGS_RESERVED = "ADMIN_SETTINGS_RESERVED"
    FP_RULE_PATH_PARAM_VALIDATION = "PATH_PARAM_VALIDATION"
    FP_RULE_EMPTY_BODY_REQUIRED = "EMPTY_BODY_REQUIRED_FIELDS"
    FP_RULE_EMPTY_PATH_PARAM = "EMPTY_PATH_PARAM_405"
    FP_RULE_EMPTY_JSON_BODY_NOT_FOUND = "EMPTY_JSON_BODY_NOT_FOUND"
    FP_RULE_STRING_BOUNDARY_EMPTY_PATH = "STRING_BOUNDARY_EMPTY_PATH"
    FP_RULE_NO_BODY_ENDPOINT = "NO_BODY_ENDPOINT"
    FP_RULE_SCHEMA_MISMATCH_VALID_ERROR = "SCHEMA_MISMATCH_VALID_ERROR"
    FP_RULE_SURVEY_VALIDATION_400 = "SURVEY_VALIDATION_400"
    FP_RULE_SURVEY_DELETE_CONFLICT_409 = "SURVEY_DELETE_CONFLICT_409"
    FP_RULE_JSONPATCH_INVALID_400 = "JSONPATCH_INVALID_400"
    FP_RULE_SURVEY_RESPONSE_VALIDATION_400 = "SURVEY_RESPONSE_VALIDATION_400"
    FP_RULE_METADATA_BULK_VALIDATION_400 = "METADATA_BULK_VALIDATION_400"
    FP_RULE_METADATA_LIST_RANDOM_200 = "METADATA_LIST_RANDOM_200"
    FP_RULE_SSRF_VALIDATION_400 = "SSRF_VALIDATION_400"
    FP_RULE_SURVEY_EXAMPLES_CONFLICT_409 = "SURVEY_EXAMPLES_CONFLICT_409"
    FP_RULE_SURVEY_METADATA_CONFLICT_409 = "SURVEY_METADATA_CONFLICT_409"
    FP_RULE_EMPTY_ARRAY_BODY_200 = "EMPTY_ARRAY_BODY_200"
    FP_RULE_SAML_ACS_NO_IDP = "SAML_ACS_NO_IDP"
    FP_RULE_SURVEY_RESPONSE_SCHEMA_ALLOF = "SURVEY_RESPONSE_SCHEMA_ALLOF"

    def detect_false_positive(self, data: Dict) -> Tuple[bool, Optional[str]]:
        """
        Detect false positives from CATS fuzzing.

        False positives are legitimate API responses that CATS flags as errors
        but are actually correct behavior for the API being tested.

        Returns:
            Tuple of (is_false_positive: bool, rule_id: Optional[str])
            - If false positive detected, returns (True, rule_id)
            - If not a false positive, returns (False, None)

        Rule IDs:
        - RATE_LIMIT_429: Rate limiting responses (infrastructure)
        - OAUTH_AUTH_401_403: Expected auth failures during fuzzing
        - VALIDATION_400: API correctly rejects malformed input
        - NOT_FOUND_404: Expected 404s from random resource testing
        - IDOR_ADMIN: IDOR tests on admin endpoints (admin has full access)
        - IDOR_LIST: IDOR tests on list endpoints (filter params)
        - IDOR_OPTIONAL: IDOR tests with optional ID fields
        - HTTP_METHODS: Unsupported HTTP methods correctly rejected
        - RESPONSE_CONTRACT: Header mismatches (spec issues)
        - CONFLICT_409: Duplicate name conflicts from fuzzed values
        - CONTENT_TYPE_GO_HTTP: Go HTTP layer transport errors
        - INJECTION_JSON_API: Injection tests on JSON API (not exploitable)
        - XSS_QUERY_PARAMS: XSS on query params (JSON API, not exploitable)
        - HEADER_VALIDATION_400: Malformed headers correctly rejected
        - LEADING_ZEROS_400: Invalid JSON numbers correctly rejected
        - ONEOF_VALIDATION_400: Incomplete oneOf bodies correctly rejected
        - CONNECTION_ERROR_999: Network/CATS issues, not API bugs
        - STRING_BOUNDARY_OPTIONAL: Empty optional fields accepted
        - TRANSFER_ENCODING_501: Unsupported transfer encoding per RFC 7230
        - DELETED_RESOURCE_LIST: List endpoints return 200 with empty array after deletion
        - REMOVE_FIELDS_ONEOF: RemoveFields on oneOf endpoints correctly returns 400
        - FORM_URLENCODED_JSON_TEST: JSON validation tests on form-urlencoded endpoints
        - DELETE_ME_CHALLENGE: DELETE /me requires challenge param, 400 without it is correct
        - ADMIN_SETTINGS_RESERVED: Reserved setting keys (e.g., "migrate") return 400
        - PATH_PARAM_VALIDATION: Path parameter regex validation failures (CATS uses hyphens)
        - EMPTY_BODY_REQUIRED_FIELDS: EmptyJsonBody with missing required properties
        - EMPTY_PATH_PARAM_405: Empty path parameter causing route mismatch (405)
        - EMPTY_JSON_BODY_NOT_FOUND: EmptyJsonBody with random UUIDs returns 404 (resource doesn't exist)
        - STRING_BOUNDARY_EMPTY_PATH: Empty path param causes route mismatch (200/405)
        - NO_BODY_ENDPOINT: Endpoints that don't accept request body correctly return 400
        - SCHEMA_MISMATCH_VALID_ERROR: Valid Error responses flagged as schema mismatch (warn)
        - SURVEY_VALIDATION_400: Survey POST/PUT returning 400 for fuzzed survey_json/status
        - SURVEY_DELETE_CONFLICT_409: DELETE on surveys returning 409 (has responses)
        - JSONPATCH_INVALID_400: PATCH returning 400 for malformed JSON Patch ops
        - SURVEY_RESPONSE_VALIDATION_400: Survey response endpoints returning 400 for fuzzed input
        - METADATA_BULK_VALIDATION_400: Bulk metadata endpoints returning 400 for malformed payloads
        - METADATA_LIST_RANDOM_200: GET metadata list returning 200 with empty array for random resources
        - SURVEY_EXAMPLES_CONFLICT_409: ExamplesFields sending example data that collides with seed data
        - SURVEY_METADATA_CONFLICT_409: Survey metadata POST 409 from seed data key collision
        - EMPTY_ARRAY_BODY_200: Empty JSON array body correctly returns 200 (no-op)
        - SAML_ACS_NO_IDP: SAML ACS returns 400 when no real IdP is configured
        - SURVEY_RESPONSE_SCHEMA_ALLOF: Survey response schema mismatch from allOf+nullable
        """
        response_code = data.get('response', {}).get('responseCode', 0)
        result_reason = (data.get('resultReason') or '').lower()
        result_details = (data.get('resultDetails') or '').lower()
        result = data.get('result', '').lower()
        fuzzer = data.get('fuzzer', '')
        scenario = (data.get('scenario') or '').lower()

        # Only check errors and warnings, not successes
        if result not in ['error', 'warn']:
            return (False, None)

        # 1. Rate Limit False Positives (429)
        # Rate limiting is infrastructure protection, not API behavior
        if response_code == 429:
            return (True, self.FP_RULE_RATE_LIMIT)

        # 2. OAuth/Auth False Positives (401/403)
        # These are expected auth failures during fuzzing
        if response_code in [401, 403]:
            # Keywords that indicate legitimate auth responses
            oauth_keywords = [
                'unauthorized', 'forbidden', 'invalidtoken', 'invalidgrant',
                'authenticationfailed', 'authenticationerror', 'authorizationerror',
                'invalid_token', 'invalid_grant', 'access_denied',
                'unexpected response code: 401', 'unexpected response code: 403'
            ]
            text_to_check = f"{result_reason} {result_details}"
            for keyword in oauth_keywords:
                if keyword in text_to_check:
                    return (True, self.FP_RULE_OAUTH_AUTH)

        # 3. Validation False Positives (400)
        # API correctly rejects malformed input from injection/boundary testing
        if response_code == 400:
            # Fuzzers that intentionally send malformed data
            # TMI has explicit security hardening middleware that rejects:
            # - Zero-width characters (invisible chars in filenames/URLs)
            # - Bidirectional overrides (can make malicious text appear safe)
            # - Hangul filler characters
            # - Combining diacritical marks (Zalgo text - DoS via rendering)
            # - Control characters
            # Returning 400 for these is CORRECT security behavior
            validation_fuzzers = [
                'ZeroWidthCharsInValuesFields', 'ZeroWidthCharsInNamesFields',
                'HangulCharsInStringFields', 'ArabicCharsInStringFields',
                'HangulFillerFields',  # TMI explicitly rejects Hangul fillers
                'BidirectionalOverrideFields',  # TMI explicitly rejects bidi overrides
                'ZalgoTextInFields',  # TMI explicitly rejects combining marks
                'AbugidasInStringFields',  # TMI rejects problematic unicode
                'FullwidthBracketsFields',  # TMI rejects fullwidth structural chars
                'SpecialCharsInStringFields', 'ControlCharsInStringFields',
                'EmojisInStringFields', 'TrailingSpacesInFields',
                'LeadingSpacesInFields', 'OverflowArraySizeFields',
                'ExtremeNegativeNumbersInNumericFields', 'ExtremePositiveNumbersInNumericFields',
                'VeryLargeStrings', 'VeryLargeUnicodeStrings', 'MalformedJson',
                'DuplicateKeysFields', 'InvalidJsonInRequestBody',
                'NullValuesInFields', 'EmptyStringsInFields',
                'SQLInjection', 'XSSInjection', 'PathTraversal'
            ]
            if fuzzer in validation_fuzzers:
                return (True, self.FP_RULE_VALIDATION_400)
            # Also check for fuzzer names containing injection patterns
            fuzzer_lower = fuzzer.lower()
            if any(pattern in fuzzer_lower for pattern in ['injection', 'overflow', 'chars', 'unicode', 'malformed', 'hangul', 'zalgo', 'bidirectional', 'fullwidth', 'abugida']):
                return (True, self.FP_RULE_VALIDATION_400)

        # 4. Not Found False Positives (404)
        # Expected when fuzzing with random/invalid resource IDs
        # All implemented endpoints correctly return 404 for non-existent resources
        if response_code == 404:
            not_found_fuzzers = [
                'RandomResourcesFuzzer', 'InsecureDirectObjectReferences',
                'RandomForeignKeyReference', 'NonExistentResource',
                'RandomResources',  # Sends random IDs for path parameters
                'AcceptLanguageHeaders',  # Can trigger 404 with random resource IDs
                'CheckSecurityHeaders',  # Can trigger 404 with random resource IDs
                'DuplicateHeaders',  # Can trigger 404 with random resource IDs
                'ExtraHeaders',  # Can trigger 404 with random resource IDs
                'HappyPath',  # Can trigger 404 if test data doesn't exist
                'LargeNumberOfRandomAlphanumericHeaders',  # Can trigger 404 with random IDs
                'NewFields',  # Can trigger 404 with random resource IDs
                'InvalidReferencesFields',  # Sends invalid path traversal attempts
                # Fuzzers that modify path parameters with special characters
                # causing UUID parsing to fail, resulting in 404 for non-existent resources
                'ZeroWidthCharsInValuesFields',  # Injects invisible chars in UUIDs
                'HangulFillerFields',  # Injects Hangul fillers in UUIDs
                'AbugidasInStringFields',  # Injects Abugida chars in UUIDs
                'FullwidthBracketsFields',  # Injects fullwidth chars in UUIDs
                'IterateThroughEnumValuesFields',  # Tests enum variations
                'RemoveFields',  # Removes required fields
                'DefaultValuesInFields',  # Uses default/empty values
                'ExtremePositiveNumbersInIntegerFields',  # Large numbers
                'ExtremeNegativeNumbersInIntegerFields',  # Negative numbers
                # Boundary testing fuzzers that may modify path parameters
                'IntegerFieldsRightBoundary',  # Max integer values
                'LeadingSpacesInHeaders',  # Spaces in headers affecting path
                'MaxLengthExactValuesInStringFields',  # Max length strings
                'TrailingSpacesInHeaders',  # Trailing spaces
                'EmptyStringsInFields',  # Empty strings in path params
                'ExamplesFields',  # Example values that may not exist
                'MaximumExactNumbersInNumericFields',  # Max numeric values
                'MinLengthExactValuesInStringFields',  # Min length strings
                'MinimumExactNumbersInNumericFields',  # Min numeric values
                'NullValuesInFields',  # Null values in path params
                'RemoveHeaders',  # Missing required headers
                'StringFieldsLeftBoundary',  # Left boundary strings
                'StringFieldsRightBoundary',  # Right boundary strings
                'VeryLargeStringsInFields',  # Very large strings
            ]
            if fuzzer in not_found_fuzzers:
                return (True, self.FP_RULE_NOT_FOUND_404)
            # Also catch general "not found" reasons
            if 'unexpected response code: 404' in result_reason:
                return (True, self.FP_RULE_NOT_FOUND_404)
            # Catch legitimate "not found" responses from implemented endpoints
            # These endpoints are fully implemented but return 404 for non-existent resources
            response_body = (data.get('response', {}).get('responseBody') or '').lower()
            legitimate_not_found_messages = [
                'not found',  # Generic
                'add-on not found',  # /addons/{id}
                'invocation not found',  # /invocations/{id}
                'user not found',  # /admin/users/{id}
                'group not found',  # /admin/groups/{id}
                'webhook not found',  # /webhooks/{id}
                'threat model not found',  # /threat_models/{id}
                'diagram not found',  # /diagrams/{id}
                'document not found',  # Document endpoints
                'threat not found',  # Threat endpoints
                'not defined in the api specification',  # Invalid paths from path traversal
            ]
            if any(msg in response_body for msg in legitimate_not_found_messages):
                return (True, self.FP_RULE_NOT_FOUND_404)
            if any(msg in result_details for msg in legitimate_not_found_messages):
                return (True, self.FP_RULE_NOT_FOUND_404)

        # 4b. IDOR False Positives for admin-only and list endpoints
        # InsecureDirectObjectReferences fuzzer replaces ID fields with alternative values
        # For admin-only endpoints and list endpoints, this is expected behavior:
        # - Admin endpoints: user is an admin, so they have access regardless of ID
        # - List endpoints: returning empty results (200) with non-matching filters is correct
        # - Optional ID fields: using non-existent IDs in optional fields is harmless
        if fuzzer == 'InsecureDirectObjectReferences':
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')

            # Admin endpoints - admin user has full access
            if path.startswith('/admin/'):
                return (True, self.FP_RULE_IDOR_ADMIN)

            # DELETE /addons/{id} is admin-only by design (see api/addon_handlers.go:207)
            # Administrators can delete any addon, so IDOR tests showing 204 are expected
            if path.startswith('/addons/') and request_method == 'DELETE':
                return (True, self.FP_RULE_IDOR_ADMIN)

            # List/collection endpoints that use filter parameters
            # GET requests to these paths return 200 with filtered (possibly empty) results
            # Changing filter IDs returns different results, not unauthorized access
            # Examples: GET /addons?threat_model_id=xxx, GET /invocations?addon_id=xxx
            list_endpoints = ['/addons', '/invocations', '/webhooks', '/threat_models',
                              '/webhooks/subscriptions']
            if response_code == 200 and request_method == 'GET':
                # Match exact path or path with query string
                if any(path == ep or path.startswith(ep + '?') or path.startswith(ep + '/') for ep in list_endpoints):
                    return (True, self.FP_RULE_IDOR_LIST)
                # Also catch 'list' in path for other list endpoints
                if 'list' in path.lower():
                    return (True, self.FP_RULE_IDOR_LIST)

            # POST/PUT with optional ID fields - non-existent IDs are ignored or create new associations
            if response_code in [200, 201, 204] and request_method in ['POST', 'PUT']:
                # Check if the scenario involves optional filter/association fields
                if any(field in scenario for field in ['threat_model_id', 'webhook_id', 'addon_id']):
                    return (True, self.FP_RULE_IDOR_OPTIONAL)

        # 5. HTTP Methods False Positives
        # HttpMethods fuzzer tests unsupported HTTP methods on endpoints
        # Returning 400/405 for unsupported methods is correct behavior
        if fuzzer in ['HttpMethods', 'NonRestHttpMethods', 'CustomHttpMethods']:
            if response_code in [400, 405]:
                return (True, self.FP_RULE_HTTP_METHODS)

        # 6. Response Contract False Positives
        # Header mismatches are spec issues, not security issues
        contract_fuzzers = [
            'ResponseHeadersMatchContractHeaders',
            'ResponseContentTypeMatchesContract'
        ]
        if fuzzer in contract_fuzzers:
            return (True, self.FP_RULE_RESPONSE_CONTRACT)

        # 7. 409 Conflict False Positives on POST /admin/groups
        # When fuzzers modify field values (zero-width chars, etc.), the modified
        # group name may still collide with existing groups. The API correctly
        # returns 409 Conflict for duplicate names. This is expected behavior.
        if response_code == 409:
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')
            # POST to /admin/groups with duplicate name triggers 409
            if path == '/admin/groups' and request_method == 'POST':
                return (True, self.FP_RULE_CONFLICT_409)

        # 8. Non-JSON Content Type False Positives from Go HTTP layer
        # When fuzzers send malformed requests, Go's net/http package may reject
        # the request at the transport layer BEFORE it reaches Gin middleware.
        # Go returns "400 Bad Request" as text/plain, not JSON.
        # The OpenAPI spec allows text/plain as an alternative for 400 responses,
        # but CATS may not recognize this due to charset suffix or only checking
        # the first content type. This is expected HTTP behavior, not a security issue.
        if 'content type not matching' in result_reason or 'content type not matching' in result_details:
            # Check if the response is text/plain (with or without charset)
            response_content_type = data.get('response', {}).get('responseContentType', '')
            if response_content_type.startswith('text/plain') and response_code == 400:
                return (True, self.FP_RULE_CONTENT_TYPE_GO_HTTP)

        # 9. Injection False Positives for JSON API
        # TMI is a JSON API, not an HTML-rendering web application
        # "Payload reflected in response" means data is stored and returned as JSON
        # This is NOT XSS - XSS requires HTML context. JSON clients must escape output.
        # Similarly, NoSQL/Command injection "potential" findings are storage, not execution
        injection_fuzzers = [
            'XssInjectionInStringFields',
            'NoSqlInjectionInStringFields',
            'CommandInjectionInStringFields',
            'SqlInjectionInStringFields'
        ]
        if fuzzer in injection_fuzzers:
            # Check if the "vulnerability" is just data reflection/storage
            if 'reflected' in result_reason.lower() or 'potential' in result_reason.lower():
                # For a JSON API, storing and returning user data is correct behavior
                # The API doesn't execute the payloads - it stores them as string data
                return (True, self.FP_RULE_INJECTION_JSON_API)
            # NoSQL injection is not applicable - TMI uses PostgreSQL, not MongoDB
            # The $where operator and similar NoSQL syntax has no effect on SQL databases
            if fuzzer == 'NoSqlInjectionInStringFields':
                # Any "detected" NoSQL injection is a false positive for a SQL-backed API
                if 'detected' in result_reason.lower() or 'vulnerability' in result_reason.lower():
                    return (True, self.FP_RULE_INJECTION_JSON_API)

        # 9b. XSS on Query Parameters is ALWAYS a False Positive for JSON APIs
        # XSS requires HTML context to execute. TMI returns application/json responses.
        # GET requests ONLY have query parameters (no request body), so XSS on GET is not exploitable.
        # For POST/PUT/PATCH, query parameter values are also not exploitable.
        # See: docs/developer/testing/cats-findings-plan.md for detailed explanation
        if fuzzer == 'XssInjectionInStringFields':
            request_method = data.get('request', {}).get('httpMethod', '')
            # All GET requests are query-param only - no XSS risk
            if request_method == 'GET':
                return (True, self.FP_RULE_XSS_QUERY_PARAMS)
            # For any method, check if this is a warning (not reflected in stored data)
            # Warnings on XSS mean the payload was "accepted" in a query parameter
            # This is not exploitable because TMI returns JSON, not HTML
            if result == 'warn':
                return (True, self.FP_RULE_XSS_QUERY_PARAMS)

        # 10. Header Validation False Positives
        # These fuzzers send malformed/unusual headers and expect success.
        # Returning 400 Bad Request for invalid headers is CORRECT behavior.
        # This is proper input validation, not a security issue.
        header_validation_fuzzers = [
            'AcceptLanguageHeaders',      # Malformed Accept-Language values
            'UnsupportedContentTypesHeaders',  # Invalid Content-Type values
            'DummyContentLengthHeaders',  # Invalid Content-Length values
            'LargeNumberOfRandomAlphanumericHeaders',  # Header flooding
            'DuplicateHeaders',           # Duplicate header injection
            'ExtraHeaders',               # Unknown headers added
        ]
        if fuzzer in header_validation_fuzzers:
            # 400 Bad Request is correct for invalid headers
            if response_code == 400:
                return (True, self.FP_RULE_HEADER_VALIDATION)

        # 13. PrefixNumbersWithZeroFields False Positives
        # This fuzzer sends numeric values as strings with leading zeros (e.g., "0095" instead of 95)
        # CATS expects 2XX responses, but the API correctly returns 400 Bad Request because:
        # - JSON numbers with leading zeros are invalid per JSON spec
        # - Sending "0095" as a string when integer is expected is a type mismatch
        # - The API's strict type validation rejects these malformed inputs
        # This is CORRECT behavior - rejecting invalid JSON/type input
        if fuzzer == 'PrefixNumbersWithZeroFields':
            if response_code == 400:
                return (True, self.FP_RULE_LEADING_ZEROS)

        # 15. InvalidReferencesFields with connection errors (response code 999)
        # Response code 999 indicates a connection error or malformed request that
        # didn't receive a real HTTP response. This is a CATS/network issue, not an API bug.
        # Often occurs with URL encoding issues in path parameters.
        if response_code == 999:
            return (True, self.FP_RULE_CONNECTION_ERROR)

        # 16. StringFieldsLeftBoundary on optional fields
        # CATS sends empty strings for optional fields and expects 4XX
        # Empty strings on optional fields like 'description' are valid - field is optional
        # API correctly returns 201 Created because empty description is allowed
        if fuzzer == 'StringFieldsLeftBoundary':
            # Check if this is about an optional field returning success
            if response_code in [200, 201, 204]:
                # Optional fields accepting empty values is correct behavior
                if 'is required [FALSE]' in scenario.lower() or 'is required [false]' in scenario:
                    return (True, self.FP_RULE_STRING_BOUNDARY_OPTIONAL)

        # 11. Security Header False Positives
        # Skip CheckSecurityHeaders if needed (currently headers are implemented)
        # Uncomment if you want to mark these as FP:
        # if fuzzer == 'CheckSecurityHeaders':
        #     return (True, "SECURITY_HEADERS")

        # 12. Transfer Encoding False Positives (501 Not Implemented)
        # The DummyTransferEncodingHeaders fuzzer sends unsupported Transfer-Encoding
        # headers like 'chunked', 'gzip', 'deflate', etc. Go's net/http correctly
        # returns 501 Not Implemented for unsupported transfer encodings per RFC 7230.
        # This is correct HTTP behavior, not a security or implementation issue.
        if response_code == 501:
            if fuzzer == 'DummyTransferEncodingHeaders':
                return (True, self.FP_RULE_TRANSFER_ENCODING)
            # Also catch any 501 with "unsupported transfer encoding" message
            response_body = (data.get('response', {}).get('responseBody') or '').lower()
            if 'unsupported transfer encoding' in response_body:
                return (True, self.FP_RULE_TRANSFER_ENCODING)
            if 'transfer encoding' in result_reason.lower():
                return (True, self.FP_RULE_TRANSFER_ENCODING)

        # 17. CheckDeletedResourcesNotAvailable on list endpoints
        # This fuzzer deletes resources then checks if they're still accessible.
        # For LIST endpoints (GET /me/client_credentials, GET /admin/quotas/users/{id}),
        # returning 200 with an empty array [] is CORRECT REST behavior.
        # The fuzzer expects 404/410, but list endpoints should return 200 with empty results.
        if fuzzer == 'CheckDeletedResourcesNotAvailable':
            path = data.get('path', '')
            # List endpoints that return empty arrays after deletion
            list_patterns = [
                '/me/client_credentials',  # List user's client credentials
                '/admin/quotas/users/',    # Get user quota (returns default if not found)
                '/admin/quotas/addons/',   # Get addon quota
                '/admin/quotas/webhooks/', # Get webhook quota
            ]
            if any(path.startswith(pattern) or path == pattern.rstrip('/') for pattern in list_patterns):
                return (True, self.FP_RULE_DELETED_RESOURCE_LIST)

        # 19. JSON validation tests on form-urlencoded endpoints (CATS test design issue)
        # CATS fuzzers like MalformedJson, DuplicateKeysFields test JSON-specific issues
        # but some endpoints (like /oauth2/revoke per RFC 7009) accept form-urlencoded data.
        # When CATS sends form-urlencoded data but applies JSON validation expectations,
        # the server correctly handles the form data and returns 200 (per RFC 7009).
        # This is correct behavior - the fuzzers are testing the wrong content type.
        json_validation_fuzzers = ['MalformedJson', 'DuplicateKeysFields', 'RandomDummyInvalidJsonBody']
        if fuzzer in json_validation_fuzzers:
            # Check request Content-Type header
            request_headers = data.get('request', {}).get('headers') or []
            content_type = ''
            for h in request_headers:
                if h.get('key', '').lower() == 'content-type':
                    content_type = h.get('value', '').lower()
                    break
            # If form-urlencoded, JSON tests are false positives
            if 'application/x-www-form-urlencoded' in content_type:
                return (True, self.FP_RULE_FORM_URLENCODED_JSON_TEST)

        # 20. DELETE /me two-step challenge flow
        # DELETE /me implements a two-step deletion process:
        # 1. First call (no challenge) returns 200 with challenge string
        # 2. Second call (with challenge) confirms deletion and returns 204
        # CATS fuzzers (HappyPath, CheckSecurityHeaders, NewFields) send DELETE /me
        # without the challenge parameter, so 400 "Invalid or expired challenge" is correct.
        # The API is correctly enforcing the two-step flow for user safety.
        if response_code == 400:
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')
            if path == '/me' and request_method == 'DELETE':
                # Any 400 on DELETE /me without challenge is expected behavior
                # The two-step flow requires the challenge parameter
                return (True, self.FP_RULE_DELETE_ME_CHALLENGE)

        # 21. Admin settings reserved keys
        # Certain setting keys like "migrate" are reserved for API endpoints.
        # POST /admin/settings/migrate is an endpoint, not a setting key.
        # When fuzzers try to use "migrate" as a setting key (via path parameter),
        # the API correctly returns 400 "reserved_key".
        if response_code == 400:
            path = data.get('path', '')
            response_body = (data.get('response', {}).get('responseBody') or '').lower()
            # Check for reserved key errors on admin settings endpoints
            if path.startswith('/admin/settings/') and 'reserved' in response_body:
                return (True, self.FP_RULE_ADMIN_SETTINGS_RESERVED)
            # Also check result details for reserved key message
            if path.startswith('/admin/settings/') and 'reserved' in result_details:
                return (True, self.FP_RULE_ADMIN_SETTINGS_RESERVED)

        # 22. Path parameter validation errors (regex mismatch)
        # CATS uses parameter names like "cats-test-key" which contain hyphens,
        # but OpenAPI spec patterns like ^[a-z][a-z0-9_.]*$ don't allow hyphens.
        # The API correctly returns 400 for invalid path parameter format.
        if response_code == 400:
            # Check response body for OpenAPI validation errors
            json_body = data.get('response', {}).get('jsonBody') or {}
            error_desc = (json_body.get('error_description') or '').lower()
            # OpenAPI filter error for path parameter regex mismatch
            if 'openapi3filter.requesterror: parameter' in error_desc and "doesn't match the regular expression" in error_desc:
                return (True, self.FP_RULE_PATH_PARAM_VALIDATION)
            # Also check response body text
            response_body = (data.get('response', {}).get('responseBody') or '').lower()
            if 'parameter' in response_body and "doesn't match the regular expression" in response_body:
                return (True, self.FP_RULE_PATH_PARAM_VALIDATION)

        # 23. EmptyJsonBody with missing required fields
        # EmptyJsonBody fuzzer sends {} when required fields are documented.
        # The API correctly returns 400 for missing required properties.
        if fuzzer == 'EmptyJsonBody' and response_code == 400:
            json_body = data.get('response', {}).get('jsonBody') or {}
            error_desc = (json_body.get('error_description') or '').lower()
            if 'property' in error_desc and 'is missing' in error_desc:
                return (True, self.FP_RULE_EMPTY_BODY_REQUIRED)
            # Also catch "is required" messages (e.g., group member endpoints)
            if 'is required' in error_desc:
                return (True, self.FP_RULE_EMPTY_BODY_REQUIRED)
            # Also check response body text
            response_body = (data.get('response', {}).get('responseBody') or '').lower()
            if 'property' in response_body and 'is missing' in response_body:
                return (True, self.FP_RULE_EMPTY_BODY_REQUIRED)
            if 'is required' in response_body:
                return (True, self.FP_RULE_EMPTY_BODY_REQUIRED)

        # 24. Empty path parameter causing 405
        # RandomResources fuzzer may send empty path parameters causing different routes to match.
        # When the URL ends with / (empty final segment), 405 is expected behavior.
        if fuzzer == 'RandomResources' and response_code == 405:
            url = data.get('request', {}).get('url', '')
            # Empty path segment at end (double slash or trailing slash after base path)
            if url.endswith('/') or '//' in url:
                return (True, self.FP_RULE_EMPTY_PATH_PARAM)

        # 25. EmptyJsonBody with 404 Not Found
        # EmptyJsonBody fuzzer uses random UUIDs that don't exist in the database.
        # When the resource doesn't exist, 404 Not Found is correct behavior.
        # This is not related to the empty body - it's about non-existent resources.
        if fuzzer == 'EmptyJsonBody' and response_code == 404:
            json_body = data.get('response', {}).get('jsonBody') or {}
            error_desc = (json_body.get('error_description') or '').lower()
            # Common "not found" messages for various resource types
            not_found_phrases = ['not found', 'does not exist', 'no such']
            if any(phrase in error_desc for phrase in not_found_phrases):
                return (True, self.FP_RULE_EMPTY_JSON_BODY_NOT_FOUND)
            # Also check result reason
            if 'not found' in result_reason:
                return (True, self.FP_RULE_EMPTY_JSON_BODY_NOT_FOUND)

        # 26. StringFieldsLeftBoundary with empty path parameter causing route mismatch
        # When StringFieldsLeftBoundary sends empty string for path parameters,
        # the URL changes (e.g., /admin/settings/{key} becomes /admin/settings/)
        # which routes to a different endpoint (list vs item).
        # 200 for GET (hits list) or 405 for other methods (list doesn't support them)
        if fuzzer == 'StringFieldsLeftBoundary':
            url = data.get('request', {}).get('url', '')
            # Check if URL ends with trailing slash (empty path param)
            if url.endswith('/') or '//' in url:
                # 200 is expected for GET on list endpoint
                # 405 is expected for methods not supported on list endpoint
                if response_code in [200, 405]:
                    return (True, self.FP_RULE_STRING_BOUNDARY_EMPTY_PATH)

        # 27. No-body endpoints correctly rejecting request bodies
        # Some endpoints (like /admin/settings/migrate) don't accept request bodies.
        # When fuzzers send bodies to these endpoints, 400 is correct behavior.
        if response_code == 400:
            json_body = data.get('response', {}).get('jsonBody') or {}
            error_desc = (json_body.get('error_description') or '').lower()
            # Check for "does not accept a request body" message
            if 'does not accept a request body' in error_desc:
                return (True, self.FP_RULE_NO_BODY_ENDPOINT)
            # Also check response body text
            response_body_text = (data.get('response', {}).get('responseBody') or '').lower()
            if 'does not accept a request body' in response_body_text:
                return (True, self.FP_RULE_NO_BODY_ENDPOINT)

        # 28. Survey endpoint validation false positives (400)
        # Survey POST/PUT correctly rejects malformed survey_json, status, etc.
        if response_code == 400:
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')
            if '/admin/surveys' in path and request_method in ['POST', 'PUT']:
                if 'unexpected response code' in result_reason:
                    return (True, self.FP_RULE_SURVEY_VALIDATION_400)

        # 29. Survey DELETE 409 Conflict (referential integrity)
        # Deleting a survey with existing responses correctly returns 409
        if response_code == 409:
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')
            if '/admin/surveys/' in path and request_method == 'DELETE':
                return (True, self.FP_RULE_SURVEY_DELETE_CONFLICT_409)

        # 30. JSON Patch validation false positives (400)
        # PATCH endpoints correctly reject malformed/empty JSON Patch operations
        if response_code == 400:
            request_method = data.get('request', {}).get('httpMethod', '')
            if request_method == 'PATCH':
                if 'unexpected response code' in result_reason:
                    return (True, self.FP_RULE_JSONPATCH_INVALID_400)

        # 31. Survey response endpoint validation false positives (400)
        # Survey response POST/PUT correctly rejects fuzzed input
        if response_code == 400:
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')
            if '/survey_responses' in path and request_method in ['POST', 'PUT']:
                if 'unexpected response code' in result_reason:
                    return (True, self.FP_RULE_SURVEY_RESPONSE_VALIDATION_400)

        # 32. Bulk metadata validation false positives (400)
        # Bulk metadata endpoints correctly reject malformed payloads
        if response_code == 400:
            path = data.get('path', '')
            if '/metadata/bulk' in path:
                if 'unexpected response code' in result_reason:
                    return (True, self.FP_RULE_METADATA_BULK_VALIDATION_400)

        # 33. Metadata list returning 200 for random resources
        # GET on metadata list endpoints returns 200 with empty array when parent doesn't exist
        if response_code == 200 and fuzzer == 'RandomResources':
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')
            if '/metadata' in path and request_method == 'GET':
                return (True, self.FP_RULE_METADATA_LIST_RANDOM_200)

        # 34. Schema mismatch warnings with valid Error responses
        # CATS warns "Not matching response schema" but our Error responses are valid.
        # The Error schema has: error (required), error_description (required), details (nullable)
        # This is a CATS false positive when the response is actually a valid error.
        if result == 'warn' and 'not matching response schema' in result_reason:
            # Check if response has valid Error schema structure
            json_body = data.get('response', {}).get('jsonBody') or {}
            if isinstance(json_body, dict):
                # Full Error response has 'error' and 'error_description' fields
                if 'error' in json_body and 'error_description' in json_body:
                    return (True, self.FP_RULE_SCHEMA_MISMATCH_VALID_ERROR)
                # OAuth endpoints may return just 'error' per RFC 6749 section 5.2
                # (e.g., SAML/OIDC error responses with just error field)
                if 'error' in json_body and response_code == 400:
                    return (True, self.FP_RULE_SCHEMA_MISMATCH_VALID_ERROR)
                # Go HTTP layer returns text/plain "400 Bad Request" for malformed requests
                # CATS parses this as {"notAJson": "400 Bad Request"}
                if json_body.get('notAJson') == '400 Bad Request':
                    return (True, self.FP_RULE_SCHEMA_MISMATCH_VALID_ERROR)

        # 35. SSRF payload reflected in validation error messages
        # CATS sends SSRF URLs (e.g., http://intranet) in non-URL fields like boolean
        # or integer fields. The OpenAPI validation middleware correctly rejects with 400
        # and includes the invalid value in the error message. CATS flags this as
        # "SSRF payload reflected in response" but the server never attempted to connect
        # to the URL. The reflection is only in the validation error description.
        if fuzzer in ('SSRFInUrlFields', 'SsrfInjectionInStringFields') and response_code == 400:
            return (True, self.FP_RULE_SSRF_VALIDATION_400)

        # 36. ExamplesFields 409 on survey create (seed data collision)
        # CATS ExamplesFields fuzzer sends the OpenAPI example survey which collides
        # with seed data already in the database. 409 Conflict is correct behavior.
        if response_code == 409 and fuzzer == 'ExamplesFields':
            path = data.get('path', '')
            if '/admin/surveys' in path:
                return (True, self.FP_RULE_SURVEY_EXAMPLES_CONFLICT_409)

        # 37. Survey metadata POST 409 (seed data collision)
        # CATS seeding creates metadata entries. When fuzzers send valid POST requests
        # to /admin/surveys/{survey_id}/metadata, the metadata key already exists from
        # seeding, so the API correctly returns 409 Conflict.
        if response_code == 409:
            path = data.get('path', '')
            request_method = data.get('request', {}).get('httpMethod', '')
            if '/admin/surveys/' in path and '/metadata' in path and request_method == 'POST':
                return (True, self.FP_RULE_SURVEY_METADATA_CONFLICT_409)

        # 38. Empty JSON array body returning 200 (correct no-op)
        # EmptyJsonArrayBody fuzzer sends [] to bulk endpoints.
        # An empty array means "no operations to perform" so 200 is correct behavior.
        if fuzzer == 'EmptyJsonArrayBody' and response_code == 200:
            return (True, self.FP_RULE_EMPTY_ARRAY_BODY_200)

        # 39. SAML ACS endpoint returning 400 (no real IdP configured)
        # The /saml/acs endpoint processes SAML assertions from identity providers.
        # In dev/test environments, no real SAML IdP is configured, so all requests
        # correctly return 400 Bad Request. This is expected behavior.
        if response_code == 400:
            path = data.get('path', '')
            if path == '/saml/acs':
                return (True, self.FP_RULE_SAML_ACS_NO_IDP)

        # 40. Survey response POST schema mismatch (allOf+nullable edge case)
        # CATS ExamplesFields warns "response body does NOT match the corresponding schema"
        # on POST /intake/survey_responses. The SurveyResponse schema uses allOf composition
        # with nullable object fields (created_by, owner). The actual response is correct
        # but CATS/OpenAPI validators can have issues with allOf+nullable combinations.
        if result == 'warn' and 'not matching response schema' in result_reason:
            path = data.get('path', '')
            if '/survey_responses' in path:
                json_body = data.get('response', {}).get('jsonBody') or {}
                # Valid survey response has 'id' and 'survey_id' fields
                if isinstance(json_body, dict) and 'id' in json_body and 'survey_id' in json_body:
                    return (True, self.FP_RULE_SURVEY_RESPONSE_SCHEMA_ALLOF)

        return (False, None)

    def is_false_positive(self, data: Dict) -> bool:
        """
        Check if a test result is a false positive.

        Returns True if false positive, False otherwise.
        For the rule ID, use detect_false_positive() instead.
        """
        is_fp, _ = self.detect_false_positive(data)
        return is_fp

    # Keep the old method name as an alias for backward compatibility
    def is_oauth_auth_false_positive(self, data: Dict) -> bool:
        """Alias for is_false_positive for backward compatibility."""
        return self.is_false_positive(data)

    def insert_test_record(self, data: Dict, source_file: str):
        """Insert complete test record (test + request + response + headers)"""
        # Get or create lookup values
        result_type_id = self.get_or_create_result_type(data['result'])
        fuzzer_id = self.get_or_create_fuzzer(data['fuzzer'])
        server_id = self.get_or_create_server(data['server'])
        path_id = self.get_or_create_path(
            data['path'],
            data.get('contractPath', '')
        )

        # Extract test number
        test_number = self.extract_test_number(data['testId'])

        # Detect false positives and get rule ID
        is_fp, fp_rule = self.detect_false_positive(data)

        # Insert test
        cursor = self.conn.execute('''
            INSERT INTO tests (
                test_id, test_number, trace_id, scenario, expected_result,
                result_type_id, fuzzer_id, server_id, path_id,
                result_reason, result_details, source_file, is_false_positive, fp_rule
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ''', (
            data['testId'],
            test_number,
            data['traceId'],
            data['scenario'],
            data['expectedResult'],
            result_type_id,
            fuzzer_id,
            server_id,
            path_id,
            data.get('resultReason'),
            data.get('resultDetails'),
            source_file,
            1 if is_fp else 0,
            fp_rule
        ))
        test_id = cursor.lastrowid

        # Insert request
        request_data = data['request']
        method_id = self.get_or_create_method(request_data['httpMethod'])

        cursor = self.conn.execute('''
            INSERT INTO requests (test_id, http_method_id, url, timestamp)
            VALUES (?, ?, ?, ?)
        ''', (
            test_id,
            method_id,
            request_data['url'],
            request_data.get('timestamp', '')
        ))
        request_id = cursor.lastrowid

        # Insert response
        response_data = data['response']
        resp_method_id = self.get_or_create_method(response_data['httpMethod'])

        cursor = self.conn.execute('''
            INSERT INTO responses (
                test_id, http_method_id, response_code, response_time_ms,
                num_words, num_lines, content_length_bytes, response_content_type
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ''', (
            test_id,
            resp_method_id,
            response_data['responseCode'],
            int(response_data.get('responseTimeInMs', 0) or 0),
            int(response_data.get('numberOfWordsInResponse', 0) or 0),
            int(response_data.get('numberOfLinesInResponse', 0) or 0),
            int(response_data.get('contentLengthInBytes', 0) or 0),
            response_data.get('responseContentType')
        ))
        response_id = cursor.lastrowid

        # Batch insert request headers (handle null headers)
        request_headers_list = request_data.get('headers') or []
        req_headers = [
            (request_id, h['key'], h['value'], idx)
            for idx, h in enumerate(request_headers_list)
        ]
        if req_headers:
            self.conn.executemany('''
                INSERT INTO request_headers (request_id, header_key, header_value, header_order)
                VALUES (?, ?, ?, ?)
            ''', req_headers)

        # Batch insert response headers (handle null headers)
        response_headers_list = response_data.get('headers') or []
        resp_headers = [
            (response_id, h['key'], h['value'], idx)
            for idx, h in enumerate(response_headers_list)
        ]
        if resp_headers:
            self.conn.executemany('''
                INSERT INTO response_headers (response_id, header_key, header_value, header_order)
                VALUES (?, ?, ?, ?)
            ''', resp_headers)

    def process_directory(self, input_dir: Path, batch_size: int = 100):
        """Process all JSON files with batched commits"""
        # Find all Test*.json files
        json_files = sorted(
            input_dir.glob('Test*.json'),
            key=lambda p: self.extract_test_number(p.stem)
        )

        if not json_files:
            logger.error(f"No Test*.json files found in {input_dir}")
            return

        logger.info(f"Found {len(json_files)} JSON files to process")

        # Load existing caches
        self._load_caches()

        # Process files in batches
        batch = []
        for i, filepath in enumerate(json_files, 1):
            data = self.parse_json_file(filepath)

            if data:
                batch.append((data, filepath.name))
            else:
                self.stats['skipped'] += 1

            # Process batch when full or at end
            if len(batch) >= batch_size or i == len(json_files):
                try:
                    with self.transaction():
                        for test_data, source_file in batch:
                            try:
                                self.insert_test_record(test_data, source_file)
                                self.stats['processed'] += 1
                            except Exception as e:
                                logger.error(f"Failed to insert {source_file}: {e}")
                                self.stats['errors'] += 1
                except Exception as e:
                    logger.error(f"Batch insert failed: {e}")
                    self.stats['errors'] += len(batch)

                # Progress reporting
                if i % 1000 == 0 or i == len(json_files):
                    logger.info(f"Progress: {i}/{len(json_files)} files "
                               f"({self.stats['processed']} processed, "
                               f"{self.stats['errors']} errors, "
                               f"{self.stats['skipped']} skipped)")

                batch = []

        logger.info(f"Processing complete: {self.stats['processed']} records imported, "
                   f"{self.stats['errors']} errors, {self.stats['skipped']} skipped")

    def print_statistics(self):
        """Print summary statistics"""
        logger.info("=== Database Statistics ===")

        cursor = self.conn.cursor()

        # Table counts
        cursor.execute("SELECT COUNT(*) FROM tests")
        logger.info(f"Tests: {cursor.fetchone()[0]}")

        cursor.execute("SELECT COUNT(*) FROM result_types")
        logger.info(f"Result types: {cursor.fetchone()[0]}")

        cursor.execute("SELECT COUNT(*) FROM fuzzers")
        logger.info(f"Fuzzers: {cursor.fetchone()[0]}")

        cursor.execute("SELECT COUNT(*) FROM paths")
        logger.info(f"API paths: {cursor.fetchone()[0]}")

        cursor.execute("SELECT COUNT(*) FROM http_methods")
        logger.info(f"HTTP methods: {cursor.fetchone()[0]}")

        cursor.execute("SELECT COUNT(*) FROM request_headers")
        logger.info(f"Request headers: {cursor.fetchone()[0]}")

        cursor.execute("SELECT COUNT(*) FROM response_headers")
        logger.info(f"Response headers: {cursor.fetchone()[0]}")

        # Result distribution
        cursor.execute('''
            SELECT rt.name, COUNT(*) as count
            FROM tests t
            JOIN result_types rt ON t.result_type_id = rt.id
            GROUP BY rt.name
        ''')
        logger.info("Result distribution:")
        for name, count in cursor.fetchall():
            logger.info(f"  {name}: {count}")

        # Total false positive count
        cursor.execute('SELECT COUNT(*) FROM tests WHERE is_false_positive = 1')
        fp_count = cursor.fetchone()[0]
        cursor.execute('SELECT COUNT(*) FROM tests')
        total_count = cursor.fetchone()[0]
        fp_pct = (100.0 * fp_count / total_count) if total_count > 0 else 0
        logger.info(f"Total false positives: {fp_count} ({fp_pct:.2f}% of total)")

        # False positive breakdown by rule
        cursor.execute('''
            SELECT fp_rule, COUNT(*) as count
            FROM tests
            WHERE is_false_positive = 1
            GROUP BY fp_rule
            ORDER BY count DESC
        ''')
        logger.info("False positives by rule:")
        for rule, count in cursor.fetchall():
            rule_pct = (100.0 * count / fp_count) if fp_count > 0 else 0
            logger.info(f"  {rule}: {count} ({rule_pct:.1f}% of FPs)")

        # Result distribution excluding false positives
        cursor.execute('''
            SELECT rt.name, COUNT(*) as count
            FROM tests t
            JOIN result_types rt ON t.result_type_id = rt.id
            WHERE t.is_false_positive = 0
            GROUP BY rt.name
        ''')
        logger.info("Result distribution (excluding false positives):")
        for name, count in cursor.fetchall():
            logger.info(f"  {name}: {count}")

        # Database file size
        import os
        db_size_mb = os.path.getsize(self.db_path) / (1024 * 1024)
        logger.info(f"Database size: {db_size_mb:.2f} MB")

        logger.info("=========================")

    def analyze(self):
        """Run ANALYZE to update query optimizer statistics"""
        logger.info("Running ANALYZE for query optimization...")
        self.conn.execute("ANALYZE")
        logger.info("Analysis complete")


def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(
        description='Parse CATS JSON results into normalized SQLite database',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog='''
Examples:
  # Create database from CATS reports
  %(prog)s --input test/outputs/cats/report/ --output test/outputs/cats/cats-results.db

  # Custom batch size for memory management
  %(prog)s -i test/outputs/cats/report/ -o test/outputs/cats/results.db --batch-size 50
        '''
    )
    parser.add_argument(
        '-i', '--input',
        required=True,
        help='Directory containing CATS JSON test files'
    )
    parser.add_argument(
        '-o', '--output',
        default='test/outputs/cats/cats-results.db',
        help='SQLite database output path (default: test/outputs/cats/cats-results.db)'
    )
    parser.add_argument(
        '--batch-size',
        type=int,
        default=100,
        help='Batch size for transaction commits (default: 100)'
    )
    parser.add_argument(
        '--create-schema',
        action='store_true',
        help='Create database schema (use for new database)'
    )

    args = parser.parse_args()

    # Validate input directory
    input_path = Path(args.input)
    if not input_path.exists():
        logger.error(f"Input directory does not exist: {input_path}")
        sys.exit(1)

    if not input_path.is_dir():
        logger.error(f"Input path is not a directory: {input_path}")
        sys.exit(1)

    # Initialize parser
    db = CATSResultsParser(args.output)
    db.connect()

    try:
        # Create schema if requested or database is new
        if args.create_schema or not Path(args.output).exists():
            db.create_schema()

        # Process directory
        db.process_directory(input_path, batch_size=args.batch_size)

        # Run ANALYZE for query optimization
        db.analyze()

        # Print statistics
        db.print_statistics()

    except Exception as e:
        logger.error(f"Fatal error: {e}", exc_info=True)
        sys.exit(1)
    finally:
        db.close()

    logger.info("Import complete")


if __name__ == '__main__':
    main()
