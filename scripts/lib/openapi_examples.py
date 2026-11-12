"""Shared walker for OpenAPI response `example` completeness.

Why this exists
---------------
CATS validates a 2xx response against the operation's declared **`example`**,
not its **`schema`** (Endava/cats#206, root-caused in #637). When an example is
present, the response body's property names -- recursively -- must be a *subset*
of the example's; the schema is never consulted. When no example is present the
check never runs at all.

So an example that omits any property the schema declares is a latent
"Not matching response schema" finding that fires the moment a fuzzer gets a 2xx
on that operation with the field populated. The 2026-07-31 campaign hit exactly
that: `PATCH /teams/{team_id}` produced 54 findings because its example lacked
`email_address` and `uri`.

The 2026-07-30 remediation built examples from *observed response bodies*, which
kept the values honest but silently missed any property that happened to be null
or absent in the fixture. This module is the schema-driven counterpart: it
compares an example against every property its schema *declares*, at every level
the example reaches, so absent-in-fixture stops meaning absent-in-example.

Deliberately NOT covered: `additionalProperties` maps. Their keys are unbounded,
so no example can enumerate them -- that is the known-unsatisfiable residual on
`MinimalDiagramModel.metadata` documented upstream. Such maps are skipped rather
than reported, because there is no spec-side fix to point a developer at.
"""

from __future__ import annotations

from typing import Any

HTTP_METHODS = {"get", "post", "put", "patch", "delete"}

# Guards runaway descent through self-referential schemas (a threat model
# contains diagrams which contain cells which carry metadata, and several
# schemas reference each other). Examples are never legitimately deeper.
MAX_DEPTH = 8


class SpecWalker:
    """Resolves `$ref`s against one loaded spec document."""

    def __init__(self, spec: dict[str, Any]) -> None:
        self.spec = spec

    def resolve(
        self, node: Any, seen: frozenset[str] = frozenset()
    ) -> tuple[Any, frozenset[str]]:
        """Follow `$ref` chains, returning the target and the refs consumed.

        The consumed set must be threaded back to callers: a `$ref` is only a
        cycle relative to the branch that is currently descending through it, so
        the guard has to accumulate along the path rather than be seeded up
        front (seeding it makes the very first dereference look like a cycle and
        silently empties every `$ref`'d schema).
        """
        while isinstance(node, dict) and "$ref" in node:
            ref = node["$ref"]
            if ref in seen:
                return {}, seen
            seen = seen | {ref}
            cur: Any = self.spec
            for part in ref.lstrip("#/").split("/"):
                if not isinstance(cur, dict) or part not in cur:
                    return {}, seen
                cur = cur[part]
            node = cur
        return (node if isinstance(node, dict) else {}), seen

    def deref(self, node: Any, seen: frozenset[str] = frozenset()) -> Any:
        """`resolve` when the caller does not need the updated guard."""
        return self.resolve(node, seen)[0]

    def object_properties(
        self, schema: Any, seen: frozenset[str] = frozenset(), depth: int = 0
    ) -> dict[str, Any]:
        """Declared properties of a (possibly composed) object schema.

        `allOf` branches are merged. `oneOf`/`anyOf` branches are unioned: a
        response may be any branch, so the example must be a superset of all of
        them for CATS's subset rule to hold.
        """
        if depth > MAX_DEPTH:
            return {}
        schema, seen = self.resolve(schema, seen)
        if not isinstance(schema, dict):
            return {}

        props: dict[str, Any] = {}
        for key in ("allOf", "oneOf", "anyOf"):
            for sub in schema.get(key, []) or []:
                props.update(self.object_properties(sub, seen, depth + 1))
        declared = schema.get("properties")
        if isinstance(declared, dict):
            props.update(declared)
        return props

    def item_schema(self, schema: Any, seen: frozenset[str] = frozenset()) -> Any:
        """The `items` schema of an array, looking through composition."""
        schema = self.deref(schema, seen)
        if not isinstance(schema, dict):
            return {}
        if "items" in schema:
            return schema["items"]
        for key in ("allOf", "oneOf", "anyOf"):
            for sub in schema.get(key, []) or []:
                found = self.item_schema(sub, seen)
                if found:
                    return found
        return {}

    def is_array(self, schema: Any, seen: frozenset[str] = frozenset()) -> bool:
        resolved = self.deref(schema, seen)
        if not isinstance(resolved, dict):
            return False
        if resolved.get("type") == "array" or "items" in resolved:
            return True
        for key in ("allOf", "oneOf", "anyOf"):
            for sub in resolved.get(key, []) or []:
                if self.is_array(sub, seen):
                    return True
        return False

    def missing_paths(
        self,
        schema: Any,
        example: Any,
        prefix: str = "",
        seen: frozenset[str] = frozenset(),
        depth: int = 0,
    ) -> list[str]:
        """Dotted property paths the schema declares but the example omits.

        Only descends where the example already reaches: an array whose example
        is empty contributes nothing, because the server returning `[]` there
        means no item property names appear in the response either.
        """
        if depth > MAX_DEPTH:
            return []
        schema, seen = self.resolve(schema, seen)

        if self.is_array(schema, seen):
            items = self.item_schema(schema, seen)
            if not isinstance(example, list):
                return []
            missing: list[str] = []
            for entry in example:
                for path in self.missing_paths(items, entry, prefix, seen, depth + 1):
                    if path not in missing:
                        missing.append(path)
            return missing

        props = self.object_properties(schema, seen, depth)
        if not props or not isinstance(example, dict):
            return []

        missing = []
        for name, sub_schema in props.items():
            path = f"{prefix}{name}"
            if name not in example:
                missing.append(path)
                continue
            missing.extend(
                self.missing_paths(
                    sub_schema, example[name], f"{path}.", seen, depth + 1
                )
            )
        return missing


def iter_response_examples(spec: dict[str, Any]):
    """Yield (path, method, code, media_object, schema, example, source).

    `source` is "operation" when the example sits on the media type and
    "schema" when it comes from the schema itself -- CATS honours both, and only
    the operation-level one can be edited without affecting other operations.
    """
    walker = SpecWalker(spec)
    for path, path_item in (spec.get("paths") or {}).items():
        if not isinstance(path_item, dict):
            continue
        for method, op in path_item.items():
            if method not in HTTP_METHODS or not isinstance(op, dict):
                continue
            for code, response in (op.get("responses") or {}).items():
                if not str(code).startswith("2"):
                    continue
                resolved = walker.deref(response)
                media = (resolved.get("content") or {}).get("application/json")
                if not isinstance(media, dict):
                    continue
                schema = media.get("schema", {})
                if "example" in media:
                    yield path, method, code, media, schema, media["example"], "operation"
                    continue
                schema_example = walker.deref(schema).get("example")
                if schema_example is not None:
                    yield path, method, code, media, schema, schema_example, "schema"
