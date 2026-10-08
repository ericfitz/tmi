#!/bin/bash
# Shared helper for the Postman runners: hand the real test data factory to Newman.
# Source this file; it defines functions only and does not change shell options.
#
# Newman runs every collection/folder/request script in its own scope, so a class
# defined in one script is invisible to the next. The runners therefore pass the
# factory's source as the TMITestDataFactory global (newman --globals <file>), and
# each script that uses the class evaluates it with
#
#   const TMITestDataFactory = eval(pm.globals.get('TMITestDataFactory') + '\n;TMITestDataFactory');
#
# (the trailing expression is needed because a `class` declaration inside eval is
# scoped to the eval). tests/test-data-factory.test.js enforces that every collection
# script using the factory carries exactly that line.

# write_factory_globals <factory.js> <out.json>: write a Postman globals file whose
# TMITestDataFactory value is the factory source, byte for byte. The file is created
# mode 600 and only appears at <out.json> once complete.
write_factory_globals() {
    local factory="$1" out="$2" tmp
    if [ ! -f "$factory" ]; then
        echo "factory-globals: test data factory not found: $factory" >&2
        return 1
    fi
    if ! command -v jq >/dev/null 2>&1; then
        echo "factory-globals: jq is required to build the Newman globals file" >&2
        return 1
    fi
    tmp="$(umask 077; mktemp "${out}.XXXXXX")" || return 1
    if ! jq -n --rawfile src "$factory" \
        '{values: [{key: "TMITestDataFactory", value: $src, type: "any", enabled: true}]}' >"$tmp"; then
        rm -f "$tmp"
        echo "factory-globals: failed to build globals from $factory" >&2
        return 1
    fi
    mv -f "$tmp" "$out"
}

# make_factory_globals_tmp <factory.js>: write the globals file to a new temp file and
# print its path. The caller owns cleanup, e.g.
#   FACTORY_GLOBALS_FILE="$(make_factory_globals_tmp "$FACTORY_JS")" || exit 1
#   trap 'rm -f "$FACTORY_GLOBALS_FILE"' EXIT
make_factory_globals_tmp() {
    local factory="$1" out
    out="$(umask 077; mktemp "${TMPDIR:-/tmp}/tmi-postman-globals.XXXXXX")" || return 1
    if ! write_factory_globals "$factory" "$out"; then
        rm -f "$out"
        return 1
    fi
    printf '%s' "$out"
}
