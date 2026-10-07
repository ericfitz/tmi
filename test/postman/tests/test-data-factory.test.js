// Run: node --test test/postman/tests/test-data-factory.test.js
// Checks that diagram data produced by the Postman factory conforms to the OpenAPI spec.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const Factory = require('../test-data-factory.js');
const spec = JSON.parse(
    fs.readFileSync(path.join(__dirname, '..', '..', '..', 'api-schema', 'tmi-openapi.json'), 'utf8'),
);
const schemas = spec.components.schemas;
const nodeShapes = schemas.Node.properties.shape.enum;
const edgeShapes = schemas.Edge.properties.shape.enum;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

test('spec enums are non-empty (guards against a vacuous test)', () => {
    assert.ok(nodeShapes.length > 0);
    assert.ok(edgeShapes.length > 0);
});

test('generateBasicDiagramCells returns schema-valid cells', () => {
    const cells = new Factory().generateBasicDiagramCells();
    assert.ok(cells.length >= 3, 'expected at least two nodes and one edge');
    const allShapes = new Set([...nodeShapes, ...edgeShapes]);
    const ids = new Set();
    for (const cell of cells) {
        assert.ok(allShapes.has(cell.shape), `shape "${cell.shape}" is not in the spec's Node/Edge shape enums`);
        assert.match(cell.id, UUID);
        ids.add(cell.id);
        const required = edgeShapes.includes(cell.shape) ? schemas.Edge.required : schemas.Node.required;
        for (const key of required) {
            assert.ok(key in cell, `${cell.shape} cell is missing required key "${key}"`);
        }
    }
    assert.equal(ids.size, cells.length, 'cell ids must be unique');
    assert.ok(cells.some((c) => nodeShapes.includes(c.shape)), 'expected a node');
    const edges = cells.filter((c) => edgeShapes.includes(c.shape));
    assert.ok(edges.length >= 1, 'expected an edge');
    for (const e of edges) {
        for (const end of [e.source, e.target]) {
            assert.match(end.cell, UUID);
            assert.ok(ids.has(end.cell), 'edge endpoint must reference an existing cell');
        }
    }
});

test('validDiagram carries the valid cells', () => {
    const d = new Factory().validDiagram();
    assert.ok(d.cells.length >= 3);
    for (const c of d.cells) assert.ok([...nodeShapes, ...edgeShapes].includes(c.shape));
});

// Evaluate a request's pre-request script in a sandbox and return the variables it sets.
function runPrerequest(request) {
    const vars = {};
    const setter = { set: (k, v) => { vars[k] = v; } };
    const pm = { collectionVariables: setter, environment: setter, variables: setter };
    const ev = request.event.find((e) => e.listen === 'prerequest');
    vm.runInNewContext(ev.script.exec.join('\n'), { pm, Date, JSON });
    return vars;
}

function findRequest(collection, method, urlSuffix) {
    const all = collection.item.flatMap((g) => g.item);
    const hit = all.find((r) => r.request.method === method && r.request.url.raw.endsWith(urlSuffix));
    assert.ok(hit, `no ${method} request ending in ${urlSuffix}`);
    return hit;
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

const collection = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'collaboration-tests-collection.json'), 'utf8'));

test('collaboration collection diagram-create body matches CreateDiagramRequest', () => {
    const req = findRequest(collection, 'POST', '/diagrams');
    const body = JSON.parse(runPrerequest(req)[bodyVar(req)]);
    const schema = schemas.CreateDiagramRequest;
    assertMatchesSchema(body, schema);
    assert.ok(schema.properties.type.enum.includes(body.type));
});

test('collaboration collection threat-model-create body matches the POST /threat_models schema', () => {
    const ref = spec.paths['/threat_models'].post.requestBody.content['application/json'].schema.$ref;
    const schema = schemas[ref.split('/').pop()];
    const req = findRequest(collection, 'POST', '/threat_models');
    assertMatchesSchema(JSON.parse(runPrerequest(req)[bodyVar(req)]), schema);
});
