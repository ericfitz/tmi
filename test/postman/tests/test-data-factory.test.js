// Run: node --test test/postman/tests/test-data-factory.test.js
// Checks that diagram data produced by the Postman factory conforms to the OpenAPI spec.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

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
