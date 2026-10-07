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

// ---------------------------------------------------------------------------
// Collections must load the real factory (not rely on an undefined global)
// ---------------------------------------------------------------------------
// Newman runs every collection/folder/request script in its own scope, so a class
// defined in one script is invisible to the next. The runners provide the factory
// source as the `TMITestDataFactory` global (newman --globals, built by
// lib/factory-globals.sh); each script that uses the class must evaluate it itself,
// with exactly this line:
const vm = require('node:vm');
const LOADER =
    "const TMITestDataFactory = eval(pm.globals.get('TMITestDataFactory') + '\\n;TMITestDataFactory');";
const factorySource = fs.readFileSync(path.join(__dirname, '..', 'test-data-factory.js'), 'utf8');

// Every script in every collection JSON next to the factory: {file, where, listen, src}.
function allCollectionScripts() {
    const dir = path.join(__dirname, '..');
    const out = [];
    const collect = (file, owner, where) => {
        for (const ev of owner.event || []) {
            const exec = (ev.script && ev.script.exec) || [];
            out.push({ file, where, listen: ev.listen, src: exec.join('\n') });
        }
    };
    const walk = (file, items, parent) => {
        for (const it of items || []) {
            const where = `${parent}/${it.name}`;
            collect(file, it, where);
            walk(file, it.item, where);
        }
    };
    for (const file of fs.readdirSync(dir).filter((f) => f.endsWith('.json')).sort()) {
        const coll = JSON.parse(fs.readFileSync(path.join(dir, file), 'utf8'));
        collect(file, coll, '(collection)');
        walk(file, coll.item, '');
    }
    return out;
}

test('every script that constructs TMITestDataFactory loads the real factory with the agreed loader', () => {
    const scripts = allCollectionScripts();
    const users = scripts.filter((s) => s.src.includes('new TMITestDataFactory'));
    assert.ok(users.length > 0, 'expected at least one factory-using script (guards against a vacuous test)');
    for (const s of users) {
        const id = `${s.file} ${s.where} [${s.listen}]`;
        assert.ok(s.src.includes(LOADER), `${id}: uses the factory without the loader line`);
        assert.ok(
            s.src.indexOf(LOADER) < s.src.indexOf('new TMITestDataFactory'),
            `${id}: loader must come before the first use`,
        );
    }
});

test('no collection script carries its own copy of the factory class or sets the global itself', () => {
    for (const s of allCollectionScripts()) {
        const id = `${s.file} ${s.where} [${s.listen}]`;
        assert.ok(!/class\s+TMITestDataFactory\b/.test(s.src), `${id}: inline copy of the factory class`);
        assert.ok(
            !/pm\.globals\.set\(\s*['"]TMITestDataFactory['"]/.test(s.src),
            `${id}: sets the TMITestDataFactory global (the runners own it)`,
        );
    }
});

// Run `body` the way a Newman script runs: a bare sandbox with only `pm`, where
// `module`, `global` and `window` are all undefined and the factory source is
// available only as the TMITestDataFactory global.
// Pass source=null to model a runner that did not provide the global.
function inSandbox(body, source = factorySource) {
    const sandbox = { pm: { globals: { get: (k) => (k === 'TMITestDataFactory' && source !== null ? source : undefined) } } };
    return vm.runInNewContext(`(function () { ${body} })()`, sandbox);
}

test('the loader yields a constructible class from the factory source in a bare Newman-like sandbox', () => {
    const Loaded = inSandbox(`${LOADER}\nreturn TMITestDataFactory;`);
    assert.equal(typeof Loaded, 'function');
    const f = new Loaded();
    assert.equal(typeof f.validThreatModel, 'function');
    assert.ok(f.validThreat().name);
    assert.ok(Array.isArray(f.validDiagram().cells));
});

test('the loader fails loudly when the runner did not provide the global', () => {
    // vm contexts have their own realm, so match on the error name rather than instanceof.
    assert.throws(() => inSandbox(`${LOADER}\nreturn TMITestDataFactory;`, null), { name: 'ReferenceError' });
});

test('every factory method the collections call exists on the loaded class', () => {
    const Loaded = inSandbox(`${LOADER}\nreturn TMITestDataFactory;`);
    const proto = Loaded.prototype;
    let checked = 0;
    for (const s of allCollectionScripts().filter((x) => x.src.includes('new TMITestDataFactory'))) {
        const vars = [...s.src.matchAll(/(?:const|let|var)\s+(\w+)\s*=\s*new TMITestDataFactory\b/g)].map((m) => m[1]);
        assert.ok(vars.length > 0, `${s.file} ${s.where}: could not find the factory variable`);
        for (const v of vars) {
            for (const m of s.src.matchAll(new RegExp(`\\b${v}\\.(\\w+)\\(`, 'g'))) {
                checked++;
                assert.equal(
                    typeof proto[m[1]],
                    'function',
                    `${s.file} ${s.where}: calls factory.${m[1]}() which the real factory does not define`,
                );
            }
        }
    }
    assert.ok(checked > 0, 'expected to check at least one method call (guards against a vacuous test)');
});
