"""Unit tests for scripts/lib/openapi_examples.py.

These pin the walker that backs `make check-response-examples`. CATS validates a
2xx response against the operation's `example`, not its `schema`
(Endava/cats#206), so the walker deciding "this example omits a declared
property" is what stands between a schema edit and a pile of
"Not matching response schema" findings.

The `$ref` cases are not incidental: the first draft seeded the cycle guard with
a schema's own `$ref` *before* dereferencing it, so every `$ref`'d schema
resolved to `{}` and the check silently passed on 90 broken examples while
reporting only 3. A walker that under-reports is worse than no walker, because
the green build is taken as proof.
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from openapi_examples import SpecWalker, iter_response_examples  # noqa: E402


# SEM@746aa587013953beb72c3f38c9643442ca4cca0d: build a minimal specification document for testing (pure)
def spec_with(schemas, paths=None):
    return {"components": {"schemas": schemas}, "paths": paths or {}}


# SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify detection of properties omitted from response examples
class TestMissingPaths(unittest.TestCase):
    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify an omitted top-level property is reported
    def test_flags_missing_top_level_property(self):
        walker = SpecWalker(spec_with({}))
        schema = {"type": "object", "properties": {"a": {}, "b": {}}}
        self.assertEqual(walker.missing_paths(schema, {"a": 1}), ["b"])

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify a complete example reports no omissions
    def test_complete_example_reports_nothing(self):
        walker = SpecWalker(spec_with({}))
        schema = {"type": "object", "properties": {"a": {}, "b": {}}}
        self.assertEqual(walker.missing_paths(schema, {"a": 1, "b": 2}), [])

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify a null value satisfies the property-name comparison
    def test_null_value_still_counts_as_present(self):
        """CATS compares property *names*, so `null` covers the name."""
        walker = SpecWalker(spec_with({}))
        schema = {"type": "object", "properties": {"a": {"nullable": True}}}
        self.assertEqual(walker.missing_paths(schema, {"a": None}), [])

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify a schema reference resolves rather than reporting a cycle
    def test_resolves_ref_instead_of_treating_it_as_a_cycle(self):
        """The regression that made the check report 3 gaps instead of 651."""
        walker = SpecWalker(
            spec_with({"Team": {"type": "object", "properties": {"id": {}, "uri": {}}}})
        )
        schema = {"$ref": "#/components/schemas/Team"}
        self.assertEqual(walker.missing_paths(schema, {"id": "x"}), ["uri"])

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify composed schema branches merge their declared properties
    def test_merges_all_of_branches(self):
        walker = SpecWalker(
            spec_with(
                {
                    "Base": {"type": "object", "properties": {"id": {}}},
                    "Team": {
                        "allOf": [
                            {"$ref": "#/components/schemas/Base"},
                            {"type": "object", "properties": {"email_address": {}}},
                        ]
                    },
                }
            )
        )
        schema = {"$ref": "#/components/schemas/Team"}
        self.assertEqual(
            sorted(walker.missing_paths(schema, {})), ["email_address", "id"]
        )

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify alternative schema branches union their declared properties
    def test_unions_one_of_branches(self):
        """A response may be any branch, so the example must cover all of them."""
        walker = SpecWalker(spec_with({}))
        schema = {
            "oneOf": [
                {"type": "object", "properties": {"a": {}}},
                {"type": "object", "properties": {"b": {}}},
            ]
        }
        self.assertEqual(sorted(walker.missing_paths(schema, {})), ["a", "b"])

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify omissions inside array items are reported
    def test_descends_into_array_items(self):
        walker = SpecWalker(spec_with({}))
        schema = {
            "type": "object",
            "properties": {
                "users": {
                    "type": "array",
                    "items": {"type": "object", "properties": {"id": {}, "email": {}}},
                }
            },
        }
        self.assertEqual(
            walker.missing_paths(schema, {"users": [{"id": 1}]}), ["users.email"]
        )

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify an empty array example reports no omissions
    def test_empty_array_contributes_nothing(self):
        """A server returning `[]` emits no item property names either."""
        walker = SpecWalker(spec_with({}))
        schema = {
            "type": "object",
            "properties": {
                "users": {
                    "type": "array",
                    "items": {"type": "object", "properties": {"id": {}}},
                }
            },
        }
        self.assertEqual(walker.missing_paths(schema, {"users": []}), [])

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify unbounded property maps are excluded from reporting
    def test_additional_properties_map_is_not_reported(self):
        """Unbounded keys cannot be enumerated; reporting them is unactionable."""
        walker = SpecWalker(spec_with({}))
        schema = {
            "type": "object",
            "properties": {"metadata": {"type": "object",
                                        "additionalProperties": {"type": "string"}}},
        }
        self.assertEqual(walker.missing_paths(schema, {"metadata": {}}), [])

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify a self-referential schema terminates instead of recursing
    def test_self_referential_schema_terminates(self):
        walker = SpecWalker(
            spec_with(
                {
                    "Node": {
                        "type": "object",
                        "properties": {
                            "id": {},
                            "child": {"$ref": "#/components/schemas/Node"},
                        },
                    }
                }
            )
        )
        schema = {"$ref": "#/components/schemas/Node"}
        self.assertEqual(walker.missing_paths(schema, {"id": 1, "child": {"id": 2}}), [])


# SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify selection of response examples from a specification
class TestIterResponseExamples(unittest.TestCase):
    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify an operation example takes precedence over a schema example
    def test_prefers_operation_example_over_schema_example(self):
        spec = spec_with(
            {"Team": {"type": "object", "example": {"from": "schema"}}},
            {
                "/teams": {
                    "get": {
                        "responses": {
                            "200": {
                                "content": {
                                    "application/json": {
                                        "schema": {"$ref": "#/components/schemas/Team"},
                                        "example": {"from": "operation"},
                                    }
                                }
                            }
                        }
                    }
                }
            },
        )
        found = list(iter_response_examples(spec))
        self.assertEqual(len(found), 1)
        *_, example, source = found[0]
        self.assertEqual(example, {"from": "operation"})
        self.assertEqual(source, "operation")

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify a schema example is used when the operation declares none
    def test_falls_back_to_schema_example(self):
        spec = spec_with(
            {"Team": {"type": "object", "example": {"from": "schema"}}},
            {
                "/teams": {
                    "get": {
                        "responses": {
                            "200": {
                                "content": {
                                    "application/json": {
                                        "schema": {"$ref": "#/components/schemas/Team"}
                                    }
                                }
                            }
                        }
                    }
                }
            },
        )
        *_, example, source = list(iter_response_examples(spec))[0]
        self.assertEqual(example, {"from": "schema"})
        self.assertEqual(source, "schema")

    # SEM@746aa587013953beb72c3f38c9643442ca4cca0d: verify failure and example-free responses are skipped
    def test_ignores_non_2xx_and_exampleless_responses(self):
        spec = spec_with(
            {},
            {
                "/teams": {
                    "get": {
                        "responses": {
                            "400": {
                                "content": {
                                    "application/json": {"example": {"error": "x"}}
                                }
                            },
                            "200": {
                                "content": {"application/json": {"schema": {"type": "object"}}}
                            },
                        }
                    }
                }
            },
        )
        self.assertEqual(list(iter_response_examples(spec)), [])


if __name__ == "__main__":
    unittest.main()
