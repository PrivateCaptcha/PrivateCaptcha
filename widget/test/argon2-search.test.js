import test from 'node:test';
import assert from 'node:assert';
import { findArgon2IDSolution } from '../js/argon2-search.js';

test('Argon search uses the canonical transcript and big-endian counter', async () => {
    const body = Uint8Array.from({ length: 48 }, (_, index) => index);
    const calls = [];
    let retainedPassword;
    const provider = {
        hash(password, salt, output) {
            assert.strictEqual(salt, body);
            assert.strictEqual(password.length, 8);
            assert.strictEqual(output.length, 4);
            calls.push(password.slice());
            retainedPassword = password;
            output.set(calls.length === 1 ? [2, 0, 0, 0] : [1, 0, 0, 0]);
        },
    };

    const solution = await findArgon2IDSolution(body, 1, 7, provider);

    assert.deepStrictEqual(calls, [
        Uint8Array.of(7, 0, 0, 0, 0, 0, 0, 0),
        Uint8Array.of(7, 0, 0, 0, 0, 0, 0, 1),
    ]);
    assert.deepStrictEqual(solution, calls[1]);
    retainedPassword.fill(255);
    assert.deepStrictEqual(solution, calls[1], 'returned solution must not alias the search nonce');
});

test('Argon search reads provider output as little-endian uint32', async () => {
    let calls = 0;
    const solution = await findArgon2IDSolution(new Uint8Array(48), 1, 0, {
        hash(_password, _salt, output) {
            calls++;
            if (calls > 1) { throw new Error('unexpected extra hash'); }
            output.set([1, 0, 0, 0]);
        },
    });

    assert.deepStrictEqual(solution, new Uint8Array(8));
});

test('Argon search keeps its lane fixed across big-endian counter carry', async () => {
    const candidates = [];
    const solution = await findArgon2IDSolution(new Uint8Array(48), 0, 9, {
        hash(password, _salt, output) {
            candidates.push(password.slice());
            output.fill(candidates.length === 257 ? 0 : 1);
        },
    });

    assert.deepStrictEqual(candidates[255], Uint8Array.of(9, 0, 0, 0, 0, 0, 0, 255));
    assert.deepStrictEqual(candidates[256], Uint8Array.of(9, 0, 0, 0, 0, 0, 1, 0));
    assert.deepStrictEqual(solution, candidates[256]);
});

test('Argon search propagates provider failure and reports nonce exhaustion', async () => {
    const expected = new Error('hash failed');
    await assert.rejects(findArgon2IDSolution(new Uint8Array(48), 0, 0, {
        hash() { throw expected; },
    }), (error) => error === expected);

    await assert.rejects(findArgon2IDSolution(new Uint8Array(48), 0, 3, {
        hash(password, _salt, output) {
            password.fill(255, 4);
            output.fill(255);
        },
    }), /nonce space exhausted/);
});

test('Argon search validates inputs before hashing', async () => {
    const provider = { hash() { throw new Error('must not hash'); } };
    const body = new Uint8Array(48);

    await assert.rejects(findArgon2IDSolution(new Uint8Array(47), 0, 0, provider), /48 bytes/);
    await assert.rejects(findArgon2IDSolution(new Uint8Array(49), 0, 0, provider), /48 bytes/);
    await assert.rejects(findArgon2IDSolution(body, -1, 0, provider), /threshold/);
    await assert.rejects(findArgon2IDSolution(body, 0x100000000, 0, provider), /threshold/);
    await assert.rejects(findArgon2IDSolution(body, 0, -1, provider), /solution index/);
    await assert.rejects(findArgon2IDSolution(body, 0, 256, provider), /solution index/);
    await assert.rejects(findArgon2IDSolution(body, 0, 0, {}), /provider/);
});

test('Argon search delegates batches to a WASM solver', async () => {
    const body = new Uint8Array(48);
    const expected = Uint8Array.of(3, 0, 0, 0, 0, 0, 0, 4);
    const solution = await findArgon2IDSolution(body, 7, 3, {
        solve(input, threshold, index) {
            assert.strictEqual(input, body);
            assert.strictEqual(threshold, 7);
            assert.strictEqual(index, 3);
            return expected;
        },
    });
    assert.strictEqual(solution, expected);
});
