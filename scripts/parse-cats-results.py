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
from typing import Dict, Optional, Tuple
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
        is_oauth_false_positive BOOLEAN DEFAULT 0,
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
    CREATE INDEX IF NOT EXISTS idx_tests_oauth_false_positive ON tests(is_oauth_false_positive);

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
        t.is_oauth_false_positive
    FROM tests t
    JOIN result_types rt ON t.result_type_id = rt.id
    JOIN fuzzers f ON t.fuzzer_id = f.id
    JOIN paths p ON t.path_id = p.id
    JOIN servers s ON t.server_id = s.id
    JOIN requests req ON t.id = req.test_id
    JOIN http_methods m ON req.http_method_id = m.id
    JOIN responses r ON t.id = r.test_id;

    -- Filtered test results view (excludes OAuth false positives)
    CREATE VIEW IF NOT EXISTS test_results_filtered_view AS
    SELECT *
    FROM test_results_view
    WHERE is_oauth_false_positive = 0;

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

    def is_false_positive(self, data: Dict) -> bool:
        """
        Detect false positives from CATS fuzzing.

        False positives are legitimate API responses that CATS flags as errors
        but are actually correct behavior for the API being tested.

        Categories:
        1. OAuth/Auth FP: 401/403 responses (expected auth errors during fuzzing)
        2. Rate Limit FP: 429 responses (infrastructure, not API behavior)
        3. Validation FP: 400 responses from injection/boundary fuzzing
        4. Not Found FP: 404 responses from random resource testing
        5. Response Contract FP: Header mismatches (spec issues, not security)
        """
        response_code = data.get('response', {}).get('responseCode', 0)
        result_reason = (data.get('resultReason') or '').lower()
        result_details = (data.get('resultDetails') or '').lower()
        result = data.get('result', '').lower()
        fuzzer = data.get('fuzzer', '')
        scenario = (data.get('scenario') or '').lower()

        # Only check errors and warnings, not successes
        if result not in ['error', 'warn']:
            return False

        # 1. Rate Limit False Positives (429)
        # Rate limiting is infrastructure protection, not API behavior
        if response_code == 429:
            return True

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
                    return True

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
                return True
            # Also check for fuzzer names containing injection patterns
            fuzzer_lower = fuzzer.lower()
            if any(pattern in fuzzer_lower for pattern in ['injection', 'overflow', 'chars', 'unicode', 'malformed', 'hangul', 'zalgo', 'bidirectional', 'fullwidth', 'abugida']):
                return True

        # 4. Not Found False Positives (404)
        # Expected when fuzzing with random/invalid resource IDs
        if response_code == 404:
            not_found_fuzzers = [
                'RandomResourcesFuzzer', 'InsecureDirectObjectReferences',
                'RandomForeignKeyReference', 'NonExistentResource'
            ]
            if fuzzer in not_found_fuzzers:
                return True
            # Also catch general "not found" reasons
            if 'unexpected response code: 404' in result_reason:
                return True

        # 4b. IDOR False Positives for admin-only and list endpoints
        # InsecureDirectObjectReferences fuzzer replaces ID fields with alternative values
        # For admin-only endpoints and list endpoints, this is expected behavior:
        # - Admin endpoints: user is an admin, so they have access regardless of ID
        # - List endpoints: returning empty results (200) with non-matching filters is correct
        # - Optional ID fields: using non-existent IDs in optional fields is harmless
        if fuzzer == 'InsecureDirectObjectReferences':
            # Admin endpoints - admin user has full access
            path = data.get('path', '')
            if path.startswith('/admin/'):
                return True
            # List endpoints return 200 with empty results for non-matching filters
            if response_code == 200 and 'list' in path.lower():
                return True
            # POST/PUT with optional ID fields - non-existent IDs are ignored
            request_method = data.get('request', {}).get('httpMethod', '')
            if response_code in [200, 201, 204] and request_method in ['POST', 'PUT']:
                # Check if the scenario involves optional fields like threat_model_id
                if any(field in scenario for field in ['threat_model_id', 'webhook_id', 'addon_id']):
                    return True

        # 5. HTTP Methods False Positives
        # HttpMethods fuzzer tests unsupported HTTP methods on endpoints
        # Returning 400/405 for unsupported methods is correct behavior
        if fuzzer in ['HttpMethods', 'NonRestHttpMethods', 'CustomHttpMethods']:
            if response_code in [400, 405]:
                return True

        # 6. Response Contract False Positives
        # Header mismatches are spec issues, not security issues
        contract_fuzzers = [
            'ResponseHeadersMatchContractHeaders',
            'ResponseContentTypeMatchesContract'
        ]
        if fuzzer in contract_fuzzers:
            return True

        # 7. Injection False Positives for JSON API
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
                return True

        # 6. Security Header False Positives
        # Skip CheckSecurityHeaders if needed (currently headers are implemented)
        # Uncomment if you want to mark these as FP:
        # if fuzzer == 'CheckSecurityHeaders':
        #     return True

        return False

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

        # Detect OAuth/auth false positives
        is_oauth_fp = self.is_oauth_auth_false_positive(data)

        # Insert test
        cursor = self.conn.execute('''
            INSERT INTO tests (
                test_id, test_number, trace_id, scenario, expected_result,
                result_type_id, fuzzer_id, server_id, path_id,
                result_reason, result_details, source_file, is_oauth_false_positive
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
            1 if is_oauth_fp else 0
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

        # OAuth false positive count
        cursor.execute('SELECT COUNT(*) FROM tests WHERE is_oauth_false_positive = 1')
        oauth_fp_count = cursor.fetchone()[0]
        logger.info(f"OAuth/Auth false positives (expected 401/403): {oauth_fp_count}")

        # Result distribution excluding OAuth false positives
        cursor.execute('''
            SELECT rt.name, COUNT(*) as count
            FROM tests t
            JOIN result_types rt ON t.result_type_id = rt.id
            WHERE t.is_oauth_false_positive = 0
            GROUP BY rt.name
        ''')
        logger.info("Result distribution (excluding OAuth false positives):")
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
