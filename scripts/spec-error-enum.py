#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.11"
# ///
"""Apply the #1048 error-code vocabulary to api-schema/tmi-openapi.json.

Sets the Error.error and OAuthError.error enums from internal/errcode, points
protocol-route error responses at OAuthError, and rewrites response examples
whose `error` value is not a member of the enum for the schema they sit under.
Rerunnable: a second run reports zero rewrites.
"""

import argparse
import collections
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ERRCODE = ROOT / "internal" / "errcode" / "errcode.go"
# Mirrors errcode.protocolRoutePattern in internal/errcode/errcode.go.
PROTOCOL_PATH = re.compile(
    r"^/(oauth2/(authorize|token|refresh|revoke|introspect|userinfo|callback|step_up)|saml(/|$)|\.well-known/)"
)
STATUS_CODE = {
    "400": "invalid_input", "401": "unauthorized", "403": "forbidden",
    "404": "not_found", "405": "method_not_allowed", "406": "not_acceptable",
    "409": "conflict", "410": "gone", "412": "version_mismatch",
    "413": "payload_too_large", "415": "unsupported_media_type",
    "422": "unprocessable_entity", "428": "if_match_required",
    "429": "rate_limit_exceeded", "500": "server_error",
    "501": "not_implemented", "503": "service_unavailable",
}
COMPONENT_STATUS = {
    "BadRequest": "400", "Unauthorized": "401", "StepUpRequired": "401",
    "Forbidden": "403", "NotFound": "404", "MethodNotAllowed": "405",
    "NotAcceptable": "406", "Conflict": "409", "PayloadTooLarge": "413",
    "UnsupportedMediaType": "415", "PreconditionRequired": "428",
    "TooManyRequests": "429", "InternalServerError": "500",
    "ServiceUnavailable": "503",
}
# Old code -> new code where the meaning is identical (REST routes).
RENAMES = {
    "invalid_request": "invalid_input", "validation_failed": "invalid_input",
    "validation_error": "invalid_input", "invalid_uuid": "invalid_id",
    "invalid_format": "invalid_patch", "patch_failed": "invalid_patch",
    "internal_error": "server_error", "internal_server_error": "server_error",
    "too_many_requests": "rate_limit_exceeded", "rate_limited": "rate_limit_exceeded",
}

REST_DESC = (
    "Machine-readable REST error code. One of: "
    "invalid_input (400, a body, field, header or query value fails validation); "
    "invalid_id (400, a path or query identifier is malformed); "
    "invalid_patch (400, a JSON Patch document is malformed or cannot be applied); "
    "unauthorized (401, missing, expired or invalid credentials); "
    "insufficient_user_authentication (401, step-up authentication required, RFC 9470); "
    "forbidden (403, authenticated but not permitted); "
    "not_found (404, resource does not exist or is hidden by authorization); "
    "method_not_allowed (405); not_acceptable (406); "
    "conflict (409, duplicate, in use, or wrong lifecycle state); "
    "gone (410, permanently removed); "
    "version_mismatch (409, If-Match does not match the current version); "
    "payload_too_large (413); unsupported_media_type (415); "
    "unprocessable_entity (422, well-formed request that cannot be processed in the current state); "
    "if_match_required (428, If-Match header missing); "
    "rate_limit_exceeded (429, transient limit, honor Retry-After); "
    "quota_exceeded (403 or 429, hard cap reached, retrying does not help); "
    "server_error (500); not_implemented (501); "
    "service_unavailable (503, a dependency is unavailable, retry later); "
    "feature_not_available (404) and content_token_provider_not_configured (422) are "
    "legacy domain codes kept at top level because clients branch on them; they are "
    "also present in details.code and will move to details.code only in a future "
    "breaking change. "
    "A domain-specific reason a client can act on is in details.code."
)
PROTOCOL_DESC = (
    "Error code for OAuth, SAML and discovery routes: the RFC 6749, 7009 and 9470 codes, "
    "the documented TMI extensions (identity_mismatch, account_conflict, email_not_verified, "
    "provider_unreachable, provider_response_invalid, invalid_provider), and the transport "
    "codes that route-agnostic middleware can emit (unauthorized, not_found, "
    "method_not_allowed, not_acceptable, payload_too_large, unsupported_media_type, "
    "rate_limit_exceeded, server_error). SAML routes also use the TMI extension codes "
    "saml_error, saml_not_enabled, saml_unavailable, saml_provider_not_found, "
    "saml_metadata_error, saml_init_error, saml_invalid_logout_request and saml_logout_error."
)


def fail(msg: str) -> None:
    print(f"spec-error-enum: {msg}", file=sys.stderr)
    sys.exit(1)


def parse_sets() -> tuple[list[str], list[str], set[str]]:
    """Read the REST, protocol and detail code values from errcode.go."""
    text = ERRCODE.read_text()
    consts = dict(re.findall(r"^\s*(\w+)\s+Code = \"([a-z0-9_]+)\"", text, re.M))

    def names(var: str) -> list[str]:
        m = re.search(rf"var {var} = \[\]Code\{{([^\n}}]*)\}}", text) or re.search(
            rf"var {var} = \[\]Code\{{\n(.*?)\n\}}", text, re.S
        )
        if not m:
            fail(f"cannot find {var} in errcode.go")
        return [consts[n] for n in re.findall(r"\w+", m.group(1))]

    rest = names("restCodes")
    proto = names("rfcCodes")
    for c in names("transportCodes"):
        if c not in proto:
            proto.append(c)
    details = {v for k, v in consts.items() if k.startswith("Detail")}
    return rest, proto, details


counts: collections.Counter = collections.Counter()


def fix_error_value(
    holder: dict, status: str | None, enum: set[str], details: set[str], where: str
) -> None:
    if "PROTOCOL_ENUM" in globals() and enum is PROTOCOL_ENUM and "retry_after" in holder:
        holder.pop("retry_after")  # OAuthError has no retry_after; the header carries it
        holder.setdefault("error_description", "Rate limit exceeded. Please try again later.")
        counts["retry_after removed from protocol example"] += 1
    old = holder.get("error")
    if not isinstance(old, str) or old in enum:
        return
    new = None
    if old in RENAMES and RENAMES[old] in enum:
        new = RENAMES[old]
    elif old in details:
        new = STATUS_CODE.get(status or "")
        holder.setdefault("details", {})["code"] = old
    elif old == "unprocessable_entity":
        new = "invalid_input"
    elif old == "collaboration_session_exists":
        new = "conflict"
        holder.setdefault("details", {})["code"] = old
    else:  # sentence or unknown value
        new = STATUS_CODE.get(status or "")
        if new is None:
            fail(f"{where}: cannot map error {old!r} (status {status!r})")
        if not holder.get("error_description"):
            holder["error_description"] = old
    if new == "invalid_input" and new not in enum and "invalid_request" in enum:
        new = "invalid_request"  # protocol routes use the RFC 6749 code
    if new not in enum:
        fail(f"{where}: {old!r} -> {new!r} not in enum")
    holder["error"] = new
    counts[f"{old} -> {new}"] += 1


def fix_examples(node: dict, status: str | None, enum: set[str], details: set[str], where: str) -> None:
    ex = node.get("example")
    if isinstance(ex, dict):
        fix_error_value(ex, status, enum, details, where)
    for name, e in (node.get("examples") or {}).items():
        if isinstance(e, dict) and isinstance(e.get("value"), dict):
            fix_error_value(e["value"], status, enum, details, f"{where}#{name}")


def lowercase_detail_examples(o) -> None:
    if isinstance(o, dict):
        d = o.get("details")
        if isinstance(d, dict) and isinstance(d.get("code"), str) and d["code"] != d["code"].lower():
            counts["details.code lowercased"] += 1
            d["code"] = d["code"].lower()
        for v in o.values():
            lowercase_detail_examples(v)
    elif isinstance(o, list):
        for v in o:
            lowercase_detail_examples(v)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--spec", required=True)
    args = ap.parse_args()
    path = Path(args.spec)
    spec = json.loads(path.read_text())
    rest, proto, detail_set = parse_sets()
    schemas = spec["components"]["schemas"]
    error = schemas["Error"]
    error["properties"]["error"]["enum"] = rest
    error["properties"]["error"]["description"] = REST_DESC
    error["properties"]["details"]["properties"]["code"]["example"] = "session_not_found"
    error["example"] = {
        "error": "invalid_input",
        "error_description": "The request is missing a required parameter",
    }
    oauth = json.loads(json.dumps(error))
    oauth["description"] = "Error response for OAuth, SAML and discovery routes"
    oauth["properties"]["error"]["enum"] = proto
    oauth["properties"]["error"]["description"] = PROTOCOL_DESC
    oauth["example"] = {
        "error": "invalid_request",
        "error_description": "The request is missing a required parameter",
    }
    schemas["OAuthError"] = oauth
    schemas["Error"] = error  # keep key order stable: OAuthError appended

    global PROTOCOL_ENUM
    rest_set, proto_set = set(rest), set(proto)
    PROTOCOL_ENUM = proto_set
    oauth_ref = {"$ref": "#/components/schemas/OAuthError"}

    for p, item in spec["paths"].items():
        is_proto = bool(PROTOCOL_PATH.match(p))
        enum = proto_set if is_proto else rest_set
        for method, op in item.items():
            if not isinstance(op, dict) or "responses" not in op:
                continue
            for st, resp in op["responses"].items():
                if not re.match(r"[45]\d\d$", st) or "content" not in resp:
                    continue
                c = resp["content"].get("application/json")
                if c is None:
                    continue
                sc = c.get("schema", {})
                if is_proto and (
                    sc.get("$ref") == "#/components/schemas/Error"
                    or (sc.get("type") == "object" and "error" in sc.get("properties", {}))
                ):
                    c["schema"] = dict(oauth_ref)
                    counts["protocol responses retargeted"] += 1
                fix_examples(c, st, enum, detail_set, f"{p} {method} {st}")
                fix_examples(sc, st, enum, detail_set, f"{p} {method} {st} schema")
    # Protocol operations that $ref a shared REST response get an OAuth variant of it.
    comps = spec["components"].setdefault("responses", {})
    for p, item in spec["paths"].items():
        if not PROTOCOL_PATH.match(p):
            continue
        for method, op in item.items():
            if not isinstance(op, dict) or "responses" not in op:
                continue
            for st, resp in op["responses"].items():
                ref = resp.get("$ref", "") if re.match(r"[45]\d\d$", st) else ""
                if not ref.startswith("#/components/responses/"):
                    continue
                base = ref.rsplit("/", 1)[1]
                if base.startswith("OAuth"):
                    continue
                variant = "OAuth" + ("ErrorResponse" if base == "Error" else base)
                if variant not in comps:
                    v = json.loads(json.dumps(comps[base]))
                    v["description"] = v.get("description", "Error response") + " (OAuth, SAML and discovery routes use the OAuthError schema.)"
                    for ct, c in v.get("content", {}).items():
                        if ct == "application/json":
                            c["schema"] = dict(oauth_ref)
                            c.pop("example", None)
                            c.pop("examples", None)
                    comps[variant] = v
                    counts["OAuth response components"] += 1
                resp["$ref"] = "#/components/responses/" + variant
                counts["protocol shared refs retargeted"] += 1
    for name, resp in spec["components"].get("responses", {}).items():
        for ct, c in resp.get("content", {}).items():
            if ct != "application/json":
                continue
            st = COMPONENT_STATUS.get(name)
            fix_examples(c, st, rest_set, detail_set, f"components.responses.{name}")
            # inline property example, e.g. InternalServerError.error
            prop = c.get("schema", {}).get("properties", {}).get("error")
            if isinstance(prop, dict) and isinstance(prop.get("example"), str):
                holder = {"error": prop["example"]}
                fix_error_value(holder, st, rest_set, detail_set, f"components.responses.{name} property")
                prop["example"] = holder["error"]
    lowercase_detail_examples(spec)
    for k, v in sorted(counts.items()):
        print(f"{v:4d}  {k}")
    path.write_text(json.dumps(spec, indent=2, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
