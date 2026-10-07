'use strict';
// Guards that every Postman collection can run on its own.
//
// A request that uses {{someId}} which nothing in the same collection provides gets
// the literal text "{{someId}}" on the wire (a 400 invalid_id, or a 401 with no auth).
// That is how threat-crud, bulk-operations, metadata and permission-matrix ended up
// failing in a standalone run: only comprehensive-test-collection created the threat
// model they assumed. Run with: node --test test/postman/tests/

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const POSTMAN_DIR = path.join(__dirname, '..');
const spec = JSON.parse(fs.readFileSync(path.join(POSTMAN_DIR, '..', '..', 'api-schema', 'tmi-openapi.json'), 'utf8'));
const schemas = spec.components.schemas;

// Variables the runners pass with `newman --env-var NAME=...`, read from the runner scripts
// so this list cannot drift from them.
function runnerEnvVars() {
    const names = new Set();
    for (const script of ['run-postman-collection.sh', 'run-tests.sh']) {
        const src = fs.readFileSync(path.join(POSTMAN_DIR, script), 'utf8');
        for (const m of src.matchAll(/--env-var\s+"(\w+)=/g)) names.add(m[1]);
    }
    return names;
}

// Variables a collection uses but cannot provide itself. Each entry is
// {collection file: {variable: where it comes from}}. Nothing here can be filled by the suite
// order (no NEW_COLLECTIONS entry depends on comprehensive-test-collection for a variable),
// so keep it to inputs that really are external, and never list a standalone-runnable collection.
const SUITE_ORDER_ALLOWLIST = {
    // Not run by run-tests.sh or the make targets; its own script says the id must be
    // supplied by hand (admin users API or the database).
    'ownership-transfer-tests-collection.json': { bobInternalUuid: 'manual input, see the request pre-request note' },
};

function loadCollections() {
    return fs
        .readdirSync(POSTMAN_DIR)
        .filter((f) => f.endsWith('.json'))
        .sort()
        .map((file) => ({ file, coll: JSON.parse(fs.readFileSync(path.join(POSTMAN_DIR, file), 'utf8')) }))
        // Only Postman collections (some older ones have no `info` block, just a top-level name).
        .filter(({ coll }) => Array.isArray(coll.item));
}

function* walkRequests(items, trail = []) {
    for (const it of items || []) {
        if (it.item) yield* walkRequests(it.item, [...trail, it.name]);
        else yield { name: [...trail, it.name].join(' / '), item: it };
    }
}

function* walkNodes(coll) {
    yield coll;
    const rec = function* (items) {
        for (const it of items || []) {
            yield it;
            yield* rec(it.item);
        }
    };
    yield* rec(coll.item);
}

function varsIn(text) {
    const out = [];
    for (const m of String(text || '').matchAll(/\{\{\s*([^{}\s]+)\s*\}\}/g)) {
        if (!m[1].startsWith('$')) out.push(m[1]); // {{$guid}} etc. are Postman dynamic variables
    }
    return out;
}

// Variables a request reads from its URL, headers, body and auth.
function usedVars(req) {
    const r = req.request;
    const texts = [];
    texts.push(typeof r.url === 'string' ? r.url : r.url && r.url.raw);
    for (const h of r.header || []) texts.push(h.key, h.value);
    if (r.body) {
        texts.push(r.body.raw);
        for (const p of [...(r.body.urlencoded || []), ...(r.body.formdata || [])]) texts.push(p.key, p.value);
    }
    texts.push(JSON.stringify(r.auth || req.auth || {}));
    return new Set(texts.flatMap(varsIn));
}

// Variables the collection itself can provide.
function providedVars(coll) {
    const provided = new Set();
    for (const v of coll.variable || []) {
        if (v.value !== undefined && v.value !== null && String(v.value) !== '') provided.add(v.key);
    }
    const setCall = /pm\.(?:collectionVariables|environment|variables|globals)\.set\(\s*['"]([^'"]+)['"]/g;
    for (const node of walkNodes(coll)) {
        for (const ev of node.event || []) {
            const src = ((ev.script && ev.script.exec) || []).join('\n');
            for (const m of src.matchAll(setCall)) provided.add(m[1]);
        }
    }
    return provided;
}

test('every {{variable}} a request uses is provided by the runner or by the collection itself', () => {
    const env = runnerEnvVars();
    for (const required of ['baseUrl', 'token_alice', 'token_bob', 'token_charlie', 'token_diana']) {
        assert.ok(env.has(required), `runner env list lost ${required}; the parse of --env-var is broken`);
    }
    const collections = loadCollections();
    assert.ok(collections.length >= 5, 'expected the postman collections (guards against a vacuous test)');

    const unset = new Map(); // "file {{var}}" -> request names
    let checked = 0;
    for (const { file, coll } of collections) {
        const provided = providedVars(coll);
        const allowed = SUITE_ORDER_ALLOWLIST[file] || {};
        for (const req of walkRequests(coll.item)) {
            for (const v of usedVars(req.item)) {
                checked++;
                if (env.has(v) || provided.has(v) || v in allowed) continue;
                const key = `${file}: {{${v}}}`;
                unset.set(key, [...(unset.get(key) || []), req.name]);
            }
        }
    }
    const problems = [...unset].map(
        ([key, names]) => `${key} is used by ${names.length} request(s), first "${names[0]}", but nothing in the collection sets it`,
    );
    assert.ok(checked > 100, 'expected to check many variable uses (guards against a vacuous test)');
    assert.deepEqual(problems, [], `\n${problems.join('\n')}`);
});

test('the suite-order allow-list only names collections and variables that still exist', () => {
    const byFile = new Map(loadCollections().map((c) => [c.file, c.coll]));
    for (const [file, vars] of Object.entries(SUITE_ORDER_ALLOWLIST)) {
        assert.ok(byFile.has(file), `allow-list names a missing collection: ${file}`);
        const used = new Set([...walkRequests(byFile.get(file).item)].flatMap((r) => [...usedVars(r.item)]));
        for (const v of Object.keys(vars)) assert.ok(used.has(v), `${file}: allow-listed {{${v}}} is no longer used`);
    }
});

test('the four standalone collections and collaboration are not allow-listed', () => {
    for (const name of ['threat-crud', 'bulk-operations', 'metadata', 'permission-matrix', 'collaboration']) {
        assert.ok(!(`${name}-tests-collection.json` in SUITE_ORDER_ALLOWLIST), `${name} must run standalone`);
    }
});

// ---------------------------------------------------------------------------
// The setup requests build real request bodies; check them against the spec.
// ---------------------------------------------------------------------------
const factorySource = fs.readFileSync(path.join(POSTMAN_DIR, 'test-data-factory.js'), 'utf8');

// Run a request's pre-request script the way Newman does (a bare sandbox with only `pm`; the
// runner provides the factory source as a global) and return the variables it sets.
function runPrerequest(request) {
    const vars = {};
    const setter = { set: (k, v) => { vars[k] = v; }, get: (k) => vars[k] };
    const pm = {
        collectionVariables: setter,
        environment: setter,
        variables: setter,
        globals: { get: (k) => (k === 'TMITestDataFactory' ? factorySource : undefined) },
    };
    const ev = request.event.find((e) => e.listen === 'prerequest');
    vm.runInNewContext(ev.script.exec.join('\n'), { pm, Date, JSON });
    return vars;
}

function findRequest(file, method, urlSuffix) {
    const coll = loadCollections().find((c) => c.file === file).coll;
    const hit = [...walkRequests(coll.item)].find(
        (r) => r.item.request.method === method && r.item.request.url.raw.endsWith(urlSuffix),
    );
    assert.ok(hit, `${file}: no ${method} request ending in ${urlSuffix}`);
    return hit.item;
}

function bodyVar(request) {
    const m = /^\{\{(\w+)\}\}$/.exec(request.request.body.raw);
    assert.ok(m, 'request body must be a single collection variable');
    return m[1];
}

function assertMatchesSchema(body, schema) {
    for (const key of Object.keys(body)) assert.ok(key in schema.properties, `unexpected key "${key}"`);
    for (const key of schema.required || []) assert.ok(key in body, `missing required key "${key}"`);
}

const threatModelInput = schemas[spec.paths['/threat_models'].post.requestBody.content['application/json'].schema.$ref.split('/').pop()];

test('every collection setup request that creates a threat model sends a ThreatModelInput', () => {
    const files = [
        'threat-crud', 'bulk-operations', 'metadata', 'permission-matrix', 'collaboration',
    ].map((n) => `${n}-tests-collection.json`);
    for (const file of files) {
        const coll = loadCollections().find((c) => c.file === file).coll;
        const creates = [...walkRequests(coll.item)].filter(
            (r) => r.item.request.method === 'POST' && r.item.request.url.raw.endsWith('/threat_models')
                && /Alice|Threat Model for Collaboration/.test(r.name),
        );
        assert.ok(creates.length > 0, `${file}: no setup request creates Alice's threat model`);
        for (const c of creates) {
            const body = JSON.parse(runPrerequest(c.item)[bodyVar(c.item)]);
            assertMatchesSchema(body, threatModelInput);
            for (const a of body.authorization || []) {
                assertMatchesSchema(a, { properties: { ...schemas.Principal.properties, role: {} }, required: [...schemas.Principal.required, 'role'] });
            }
        }
    }
});

test('the permission-matrix setup requests send documented request bodies', () => {
    const file = 'permission-matrix-tests-collection.json';
    const cases = [
        ['/documents', schemas.DocumentBase],
        ['/repositories', schemas.RepositoryBase],
        ['/assets', schemas.AssetBase],
        ['/notes', schemas.NoteBase],
        ['/diagrams', schemas.CreateDiagramRequest],
    ];
    const coll = loadCollections().find((c) => c.file === file).coll;
    for (const [suffix, schema] of cases) {
        const req = [...walkRequests(coll.item)].find(
            (r) => r.name.includes('Setup - Create Test') && r.item.request.url.raw.endsWith(suffix),
        );
        assert.ok(req, `${file}: no setup request for ${suffix}`);
        assertMatchesSchema(JSON.parse(runPrerequest(req.item)[bodyVar(req.item)]), schema);
    }
});

test('collaboration diagram-create body matches CreateDiagramRequest', () => {
    const req = findRequest('collaboration-tests-collection.json', 'POST', '/diagrams');
    const body = JSON.parse(runPrerequest(req)[bodyVar(req)]);
    const schema = schemas.CreateDiagramRequest;
    assertMatchesSchema(body, schema);
    assert.ok(schema.properties.type.enum.includes(body.type));
});
