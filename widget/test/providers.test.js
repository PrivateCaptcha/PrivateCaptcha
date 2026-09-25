import test from 'node:test';
import assert from 'node:assert';
import { loadArgon2IDProvider } from '../js/hash-providers.js';
import { readUInt32LE } from '../js/puzzle.utils.js';

const argon2Fixtures = { project: {
    version: 19, passes: 1, parallelism: 1, tagLength: 32,
    nonce: '0001020304050607',
    puzzleBody: '0201000102030405060708090a0b0c0d0e0f0807060504030201981880857467101112131415161718191a1b1c1d1e1f',
    vectors: [{ tag: 'ed16534ab875459775338b0027e9cce89153734c64fdbff6121d73511862b66f', valueLittleEndian: 1246959341 }],
    transcripts: [{
        nonce: 'f0e0d0c0b0a09080',
        puzzleBody: '0201f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff1122334455667788880100f15365ffeeddccbbaa99887766554433221100',
        tag: '6ab6bde7102506d3b7156239571761f4e881d418d111ff11c7bca35e3322c66f', valueLittleEndian: 3887969898,
    }],
} };

function bytesFromHex(value) {
    return Uint8Array.from(value.match(/.{2}/g), (byte) => Number.parseInt(byte, 16));
}

function observedWasmDependencies(mode, observed) {
    return {
        async instantiateWasm(bytes) {
            if (mode === 'noble') {
                throw new Error('forced WASM setup failure');
            }
            const result = await WebAssembly.instantiate(bytes);
            observed.memory = result.instance.exports.memory;
            return result;
        },
    };
}

test('Argon providers match shared Go vectors', async (t) => {
    const fixture = argon2Fixtures.project;
    assert.strictEqual(fixture.version, 0x13);
    assert.strictEqual(fixture.passes, 1);
    assert.strictEqual(fixture.parallelism, 1);
    assert.strictEqual(fixture.tagLength, 32);

    const vectors = [...fixture.vectors, ...fixture.transcripts];
    const paths = [
        { mode: 'scalar', wasm: true },
        { mode: 'noble', wasm: false },
    ];
    for (const path of paths) {
        await t.test(path.mode, async () => {
            const observed = { memory: null };
            const provider = await loadArgon2IDProvider(observedWasmDependencies(path.mode, observed));
            assert.strictEqual(provider.wasm, path.wasm);
            assert.strictEqual(provider.implementation, `argon2id-${path.mode}`);
            if (path.wasm) assert.strictEqual(observed.memory.buffer.byteLength, 16 * 1024 * 1024 + 128 * 1024);

            for (const vector of vectors) {
                const output = new Uint8Array(4);
                const nonce = bytesFromHex(vector.nonce ?? fixture.nonce);
                const body = bytesFromHex(vector.puzzleBody ?? fixture.puzzleBody);
                assert.strictEqual(provider.hash(nonce, body, output), output);
                assert.deepStrictEqual(output, bytesFromHex(vector.tag).subarray(0, 4));
                assert.strictEqual(readUInt32LE(output, 0), vector.valueLittleEndian);
            }
        });
    }
});

test('Argon provider validates inputs before hashing', async () => {
    const provider = await loadArgon2IDProvider();
    const nonce = new Uint8Array(8);
    const body = new Uint8Array(48);
    const output = new Uint8Array(4);
    assert.throws(() => provider.hash(new Uint8Array(7), body, output), /8 bytes/);
    assert.throws(() => provider.hash(new Uint8Array(9), body, output), /8 bytes/);
    assert.throws(() => provider.hash(nonce, new Uint8Array(47), output), /48 bytes/);
    assert.throws(() => provider.hash(nonce, new Uint8Array(49), output), /48 bytes/);
    assert.throws(() => provider.hash(nonce, body, new Uint8Array(3)), /4 bytes/);
    assert.throws(() => provider.hash(nonce, body, new Uint8Array(5)), /4 bytes/);
});

test('Argon provider propagates hash-time failures without changing implementations', async () => {
    const expected = new Error('WASM hash failed');
    let nobleCalls = 0;
    const provider = await loadArgon2IDProvider({
        instantiateWasm: async () => ({ instance: { exports: {
            memory: { buffer: new ArrayBuffer(256) },
            puzzle_ptr: () => 0,
            digest_ptr: () => 128,
            hash_puzzle: () => { throw expected; },
            prepare_puzzle: () => {},
            solve_batch: () => 0,
        } } }),
        nobleHash: () => { nobleCalls++; return new Uint8Array(32); },
    });
    assert.throws(() => provider.hash(new Uint8Array(8), new Uint8Array(48), new Uint8Array(4)), (error) => error === expected);
    assert.strictEqual(nobleCalls, 0);
});

test('Argon provider returns big-endian WASM solution bytes across counter carries', async () => {
    const observed = [];
    const provider = await loadArgon2IDProvider({
        instantiateWasm: async () => ({ instance: { exports: {
            memory: { buffer: new ArrayBuffer(256) },
            puzzle_ptr: () => 0,
            digest_ptr: () => 128,
            hash_puzzle: () => 0,
            prepare_puzzle: () => {},
            solve_batch: (...args) => { observed.push(args); return 1; },
            get_solution_nonce: () => 0x01020304,
        } } }),
    });
    const body = new Uint8Array(48);
    assert.deepStrictEqual(await provider.solve(body, 12, 7), Uint8Array.of(7, 0, 0, 0, 1, 2, 3, 4));
    assert.deepStrictEqual(observed, [[7, 12, 0, 65536]]);
    await assert.rejects(provider.solve(new Uint8Array(47), 12, 7), /48 bytes/);
    await assert.rejects(provider.solve(body, -1, 7), /threshold/);
    await assert.rejects(provider.solve(body, 12, 256), /solution index/);
});

test('Argon provider selects noble if scalar WASM fails', async () => {
    const expected = Uint8Array.from({ length: 32 }, (_, index) => index);
    let observed;
    const provider = await loadArgon2IDProvider({
        instantiateWasm: async () => { throw new RangeError('WASM unavailable'); },
        nobleHash: (password, salt, parameters) => {
            observed = { password, salt, parameters };
            return expected;
        },
    });
    const nonce = bytesFromHex(argon2Fixtures.project.nonce);
    const body = bytesFromHex(argon2Fixtures.project.puzzleBody);
    const output = new Uint8Array(4);
    assert.strictEqual(provider.implementation, 'argon2id-noble');
    provider.hash(nonce, body, output);
    assert.deepStrictEqual(output, expected.subarray(0, 4));
    assert.strictEqual(observed.password.length, 128);
    assert.deepStrictEqual(observed.password.subarray(0, 48), body);
    assert.deepStrictEqual(observed.password.subarray(120), nonce);
    assert.deepStrictEqual(observed.salt, body.subarray(0, 16));
    assert.strictEqual(observed.parameters.m, 16384);
    assert.strictEqual(observed.parameters.t, 1);
    assert.strictEqual(observed.parameters.p, 1);
    assert.strictEqual(observed.parameters.dkLen, 32);
});

test('Argon provider rejects malformed noble output', async () => {
    for (const length of [3, 4, 33]) {
        const provider = await loadArgon2IDProvider({
            instantiateWasm: async () => { throw new Error('WASM unavailable'); },
            nobleHash: () => new Uint8Array(length),
        });
        assert.throws(() => provider.hash(new Uint8Array(8), new Uint8Array(48), new Uint8Array(4)), /32-byte tag/);
    }
});

test('Argon provider propagates Noble failures', async () => {
    for (const expected of [new Error('hash failed'), new RangeError('Noble out of memory')]) {
        const provider = await loadArgon2IDProvider({
            instantiateWasm: async () => { throw new Error('WASM unavailable'); },
            nobleHash: () => { throw expected; },
        });
        assert.throws(() => provider.hash(new Uint8Array(8), new Uint8Array(48), new Uint8Array(4)), (error) => error === expected);
    }
});
