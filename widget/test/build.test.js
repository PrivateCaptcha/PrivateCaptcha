import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

test('Blake2b widget omits Argon2id WASM and noble fallback', async () => {
    const [blake2b, extended, wasm] = await Promise.all([
        readFile('./static/js/privatecaptcha.js', 'utf8'),
        readFile('./static/js/privatecaptcha-ext.js', 'utf8'),
        readFile('./wasm/argon2id-solver-scalar.wasm'),
    ]);

    const argonWasm = wasm.toString('base64');
    const nobleFallback = '"m" (memory) must be at least 8*p bytes';
    assert.ok(extended.includes(argonWasm));
    assert.ok(extended.includes(nobleFallback));
    assert.ok(!blake2b.includes(argonWasm));
    assert.ok(!blake2b.includes(nobleFallback));
    assert.ok(blake2b.length < extended.length);
    assert.ok(/x-pc-captcha-version",\s*(?:false\s*\?\s*"2"\s*:\s*"1"|"1")/.test(blake2b));
    assert.ok(/x-pc-captcha-version",\s*(?:true\s*\?\s*"2"\s*:\s*"1"|"2")/.test(extended));
});
