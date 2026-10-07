'use strict';
// Guards that the five standalone collections (threat-crud, bulk-operations, metadata,
// permission-matrix, collaboration) only send requests the OpenAPI spec documents, and do not
// assert error codes or users the spec and the dev stack contradict.
// Run: node --test test/postman/tests/*.test.js   (the directory form fails on Node 22)

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const POSTMAN_DIR = path.join(__dirname, '..');
const spec = JSON.parse(fs.readFileSync(path.join(POSTMAN_DIR, '..', '..', 'api-schema', 'tmi-openapi.json'), 'utf8'));
const FILES = ['threat-crud', 'bulk-operations', 'metadata', 'permission-matrix', 'collaboration'].map(
    (n) => `${n}-tests-collection.json`,
);
const METHODS = ['get', 'put', 'post', 'delete', 'patch', 'options', 'head'];

function load(file) {
    return JSON.parse(fs.readFileSync(path.join(POSTMAN_DIR, file), 'utf8'));
}

function* walkRequests(items, trail = []) {
    for (const it of items || []) {
        if (it.item) yield* walkRequests(it.item, [...trail, it.name]);
        else yield { name: [...trail, it.name].join(' / '), item: it };
    }
}

// The spec path template a request URL belongs to: the template with the most literal
// segments among those that match (so /threats/bulk beats /threats/{threat_id}).
function specPathFor(raw) {
    const urlPath = raw.replace(/^\{\{baseUrl\}\}/, '').split('?')[0];
    const segs = urlPath.split('/').filter(Boolean);
    let best = null;
    for (const tmpl of Object.keys(spec.paths)) {
        const t = tmpl.split('/').filter(Boolean);
        if (t.length !== segs.length) continue;
        let literals = 0;
        const ok = t.every((s, i) => {
            if (s.startsWith('{')) return segs[i].length > 0;
            literals += 1;
            return s === segs[i];
        });
        if (ok && (!best || literals > best.literals)) best = { tmpl, literals };
    }
    return best && best.tmpl;
}

test('every request in the five collections is a documented operation', () => {
    const problems = [];
    let checked = 0;
    for (const file of FILES) {
        for (const { name, item } of walkRequests(load(file).item)) {
            const req = item.request;
            const raw = typeof req.url === 'string' ? req.url : req.url.raw;
            const tmpl = specPathFor(raw);
            const method = req.method.toLowerCase();
            checked += 1;
            if (!tmpl) problems.push(`${file}: "${name}" ${req.method} ${raw} is not a path in the spec`);
            else if (!METHODS.includes(method) || !spec.paths[tmpl][method]) {
                problems.push(`${file}: "${name}" ${req.method} ${tmpl} is not an operation in the spec`);
            }
        }
    }
    assert.ok(checked > 50, 'expected to check many requests (guards against a vacuous test)');
    assert.deepEqual(problems, [], `\n${problems.join('\n')}`);
});

test('request bodies use a documented content type and only where the spec has a request body', () => {
    const problems = [];
    for (const file of FILES) {
        for (const { name, item } of walkRequests(load(file).item)) {
            const req = item.request;
            const raw = typeof req.url === 'string' ? req.url : req.url.raw;
            const tmpl = specPathFor(raw);
            const op = tmpl && spec.paths[tmpl][req.method.toLowerCase()];
            if (!op) continue; // reported by the previous test
            const documented = op.requestBody ? Object.keys(op.requestBody.content) : [];
            const sent = (req.header || []).find((h) => h.key.toLowerCase() === 'content-type');
            const hasBody = Boolean(req.body && req.body.raw);
            if (!hasBody && sent) problems.push(`${file}: "${name}" sends Content-Type ${sent.value} without a body`);
            if (hasBody && documented.length === 0) problems.push(`${file}: "${name}" sends a body but ${req.method} ${tmpl} has no request body`);
            if (hasBody && documented.length > 0 && !(sent && documented.includes(sent.value))) {
                problems.push(`${file}: "${name}" Content-Type ${sent ? sent.value : '(none)'} not in ${JSON.stringify(documented)}`);
            }
        }
    }
    assert.deepEqual(problems, [], `\n${problems.join('\n')}`);
});

test('assertions use the 403 error code the server returns, not the spec example access_denied', () => {
    const hits = [];
    for (const file of FILES) {
        for (const { name, item } of walkRequests(load(file).item)) {
            for (const ev of item.event || []) {
                if ((ev.script.exec || []).join('\n').includes('access_denied')) hits.push(`${file}: "${name}"`);
            }
        }
    }
    assert.deepEqual(hits, [], `\n${hits.join('\n')}`);
});

test('the permission-matrix reader is bob: charlie is an administrator and owns every threat model', () => {
    const coll = load('permission-matrix-tests-collection.json');
    const bad = [];
    for (const { name, item } of walkRequests(coll.item)) {
        const text = JSON.stringify(item);
        if (/token_charlie|setActiveUser\('charlie'\)|provider_id: 'charlie'/.test(text)) bad.push(name);
    }
    assert.deepEqual(bad, [], `requests that use charlie as a user: ${bad.join(', ')}`);
    const readers = [...walkRequests(coll.item)].filter((r) => /^Reader /.test(r.item.name));
    assert.ok(readers.length > 20, 'expected the reader requests (guards against a vacuous test)');
    for (const r of readers.filter((r) => !/Setup/.test(r.item.name))) {
        const auth = r.item.request.header.find((h) => h.key === 'Authorization');
        assert.equal(auth && auth.value, 'Bearer {{token_bob}}', `${r.name} must authenticate as bob`);
    }
});
