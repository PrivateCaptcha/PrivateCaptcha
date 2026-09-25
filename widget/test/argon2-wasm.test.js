import test from 'node:test';
import assert from 'node:assert';
import scalarBase64 from '../wasm/argon2id-solver-scalar.wasm';
import { loadArgon2IDProvider } from '../js/hash-providers.js';
import { findArgon2IDSolution } from '../js/argon2-search.js';
import { readUInt32LE } from '../js/puzzle.utils.js';
import { argon2id as nobleArgon2ID } from '@noble/hashes/argon2.js';

const fixtures = { project: {
    nonce: '0001020304050607',
    puzzleBody: '0201000102030405060708090a0b0c0d0e0f0807060504030201981880857467101112131415161718191a1b1c1d1e1f',
    vectors: [{ tag: 'ed16534ab875459775338b0027e9cce89153734c64fdbff6121d73511862b66f' }],
    transcripts: [{
        nonce: 'f0e0d0c0b0a09080',
        puzzleBody: '0201f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff1122334455667788880100f15365ffeeddccbbaa99887766554433221100',
        tag: '6ab6bde7102506d3b7156239571761f4e881d418d111ff11c7bca35e3322c66f',
    }],
} };

function bytesFromHex(value) {
    return Uint8Array.from(value.match(/.{2}/g), (byte) => Number.parseInt(byte, 16));
}

function hexFromBytes(value) {
    return Array.from(value, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

test('scalar Argon2id WASM matches the signed v2 transcript', async () => {
    const project = fixtures.project;
    const binary = Uint8Array.from(atob(scalarBase64), (character) => character.charCodeAt(0));
    assert.deepStrictEqual(WebAssembly.Module.imports(new WebAssembly.Module(binary)), []);
    const { instance } = await WebAssembly.instantiate(binary);
    const wasm = instance.exports;
    for (const vector of [...project.vectors, ...project.transcripts]) {
        const body = bytesFromHex(vector.puzzleBody ?? project.puzzleBody);
        const nonce = bytesFromHex(vector.nonce ?? project.nonce);
        const password = new Uint8Array(128);
        password.set(body);
        password.set(nonce, 120);
        new Uint8Array(wasm.memory.buffer).set(password, wasm.puzzle_ptr());
        assert.strictEqual(wasm.hash_puzzle(), 0);
        const tag = hexFromBytes(new Uint8Array(wasm.memory.buffer, wasm.digest_ptr(), 32));
        assert.strictEqual(tag, vector.tag);
    }
});

test('Argon2id batch solver returns a verifier-compatible 8-byte nonce', async () => {
    const body = bytesFromHex(fixtures.project.puzzleBody);
    const provider = await loadArgon2IDProvider();
    assert.strictEqual(provider.implementation, 'argon2id-scalar');
    const expected = Uint8Array.of(3, 0, 0, 0, 0, 0, 0, 0);
    const tagPrefix = new Uint8Array(4);
    provider.hash(expected, body, tagPrefix);
    const solution = await findArgon2IDSolution(body, readUInt32LE(tagPrefix, 0), 3, provider);
    assert.deepStrictEqual(solution, expected);
});

test('Noble fallback agrees with the full Go Argon2id tags', () => {
    const project = fixtures.project;
    for (const vector of [...project.vectors, ...project.transcripts]) {
        const password = new Uint8Array(128);
        password.set(bytesFromHex(vector.puzzleBody ?? project.puzzleBody));
        password.set(bytesFromHex(vector.nonce ?? project.nonce), 120);
        const tag = nobleArgon2ID(password, password.subarray(0, 16), {
            t: 1, m: 16384, p: 1, version: 0x13, dkLen: 32, maxmem: 16384 * 1024,
        });
        assert.strictEqual(hexFromBytes(tag), vector.tag);
    }
});
