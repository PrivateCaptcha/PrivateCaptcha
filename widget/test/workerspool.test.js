import test from 'node:test';
import assert from 'node:assert';
import { WorkersPool } from '../js/workerspool.js';
import { CHALLENGE_BLAKE2B, CHALLENGE_ARGON2ID } from '../js/puzzle.js';
import { createWorkerSolver } from '../js/worker-solver.js';

function puzzle(solutionsCount = 2) {
    return {
        ID: 42,
        challenge: CHALLENGE_BLAKE2B,
        puzzleBuffer: new Uint8Array(128),
        difficulty: 136,
        solutionsCount,
        isZero() { return false; },
    };
}

test('WorkersPool dispatches eight Argon2id lanes sequentially through one worker', () => {
    const { TestWorker, workers } = workerHarness();
    const work = puzzle(8);
    work.challenge = CHALLENGE_ARGON2ID;
    work.puzzleBytes = Uint8Array.from({ length: 48 }, (_, index) => index);
    const errors = [];
    let completed = 0;
    const pool = new WorkersPool({
        workerError(error) { errors.push(error); },
        workCompleted() { completed++; },
    }, false, TestWorker);

    pool.init(work, false);
    assert.strictEqual(workers.length, 1);
    assert.strictEqual(workers[0].messages[0].argument.challenge, CHALLENGE_ARGON2ID);
    assert.deepStrictEqual(workers[0].messages[0].argument.buffer, work.puzzleBytes);
    workers[0].onmessage({ data: { command: 'init' } });
    pool.solve(work);

    for (let index = 0; index < work.solutionsCount; index++) {
        assert.strictEqual(workers[0].messages.length, index + 2);
        assert.strictEqual(workers[0].messages[index + 1].argument.puzzleIndex, index);
        assert.strictEqual(workers[0].messages[index + 1].argument.difficulty, work.difficulty);
        const solution = Uint8Array.of(index, 0, 0, 0, 0, 0, 0, index);
        workers[0].onmessage({ data: { command: 'solve', argument: { id: work.ID, solution, wasm: true } } });
    }
    assert.strictEqual(completed, 1);
    assert.deepStrictEqual(errors, []);
    assert.strictEqual(workers[0].messages.length, work.solutionsCount + 1);
    const serialized = Uint8Array.from(atob(pool.serializeSolutions(0)), (char) => char.charCodeAt(0));
    assert.strictEqual(serialized.length, 7 + work.solutionsCount * 8);
    assert.strictEqual(serialized[2], 1);
    for (let index = 0; index < work.solutionsCount; index++) {
        assert.strictEqual(serialized[7 + index * 8], index);
    }
});

test('WorkersPool keeps four Blake workers and the normalized working buffer', () => {
    const { TestWorker, workers } = workerHarness();
    const work = puzzle();
    const pool = new WorkersPool({}, false, TestWorker);
    pool.init(work, false);
    assert.strictEqual(workers.length, 4);
    for (const worker of workers) {
        assert.strictEqual(worker.messages[0].argument.challenge, CHALLENGE_BLAKE2B);
        assert.strictEqual(worker.messages[0].argument.buffer, work.puzzleBuffer);
    }
});

test('WorkersPool rejects unknown challenge before creating workers', () => {
    const { TestWorker, workers } = workerHarness();
    const work = puzzle();
    work.challenge = 99;
    const pool = new WorkersPool({}, false, TestWorker);
    assert.throws(() => pool.init(work, false), /Unknown puzzle challenge/);
    assert.strictEqual(workers.length, 0);
});

test('WorkersPool uses the Argon2id solution count returned by the server', () => {
    const { TestWorker, workers } = workerHarness();
    const work = puzzle(3);
    work.challenge = CHALLENGE_ARGON2ID;
    work.puzzleBytes = new Uint8Array(48);
    let completed = 0;
    const pool = new WorkersPool({ workCompleted() { completed++; } }, false, TestWorker);
    pool.init(work, false);
    pool.solve(work);
    for (let index = 0; index < work.solutionsCount; index++) {
        workers[0].onmessage({ data: {
            command: 'solve', argument: { id: work.ID, solution: Uint8Array.of(index, 0, 0, 0, 0, 0, 0, 1), wasm: true },
        } });
    }
    assert.strictEqual(completed, 1);
    assert.strictEqual(workers[0].messages.length, work.solutionsCount + 1);
});

test('WorkersPool terminates the Argon2id worker on provider failure', () => {
    const { TestWorker, workers } = workerHarness();
    const errors = [];
    const pool = new WorkersPool({ workerError(error) { errors.push(error); } }, false, TestWorker);
    const work = puzzle(8);
    work.challenge = CHALLENGE_ARGON2ID;
    work.puzzleBytes = new Uint8Array(48);
    pool.init(work, false);
    pool.solve(work);
    workers[0].onmessage({ data: { command: 'error', error: 'Argon2id WASM search failed' } });
    assert.deepStrictEqual(errors, ['Argon2id WASM search failed']);
    assert.strictEqual(workers[0].terminated, true);
});

test('WorkersPool reports a failed Argon2id dispatch after the first solution', () => {
    const { TestWorker, workers } = workerHarness();
    class ThrowingWorker extends TestWorker {
        postMessage(message) {
            if (message.command === 'solve' && message.argument.puzzleIndex === 1) {
                throw new Error('dispatch failed');
            }
            super.postMessage(message);
        }
    }
    const work = puzzle(3);
    work.challenge = CHALLENGE_ARGON2ID;
    work.puzzleBytes = new Uint8Array(48);
    const errors = [];
    let completed = 0;
    const pool = new WorkersPool({
        workerError(error) { errors.push(error); },
        workCompleted() { completed++; },
    }, false, ThrowingWorker);
    pool.init(work, true);
    pool.solve(work);
    workers[0].onmessage({ data: {
        command: 'solve', argument: { id: work.ID, solution: Uint8Array.of(0, 0, 0, 0, 0, 0, 0, 1), wasm: true },
    } });
    assert.match(errors[0]?.message, /dispatch failed/);
    assert.strictEqual(errors.length, 1);
    assert.strictEqual(completed, 0);
    assert.strictEqual(workers[0].terminated, true);
});

for (const challenge of [CHALLENGE_BLAKE2B, CHALLENGE_ARGON2ID]) {
    test(`WorkersPool fails ${challenge === CHALLENGE_BLAKE2B ? 'Blake2b' : 'Argon2id'} without completing partial work`, () => {
        const { TestWorker, workers } = workerHarness();
        const errors = [];
        let completed = 0;
        const pool = new WorkersPool({
            workerError(error) { errors.push(error); },
            workCompleted() { completed++; },
        }, false, TestWorker);
        const work = puzzle(8);
        work.challenge = challenge;
        if (challenge === CHALLENGE_ARGON2ID) { work.puzzleBytes = new Uint8Array(48); }
        pool.init(work, true);
        pool.solve(work);

        workers[0].onmessage({ data: {
            command: 'solve', argument: { id: work.ID, solution: Uint8Array.of(0, 0, 0, 0, 0, 0, 0, 1), wasm: true },
        } });
        workers[0].onmessage({ data: { command: 'error', error: 'search failed' } });
        assert.deepStrictEqual(errors, ['search failed']);
        assert.strictEqual(completed, 0);
        assert.ok(workers.every((worker) => worker.terminated));
        assert.strictEqual(pool._solutions.length, 1);

        workers[0].onmessage({ data: {
            command: 'solve', argument: { id: work.ID, solution: Uint8Array.of(1, 0, 0, 0, 0, 0, 0, 2), wasm: true },
        } });
        workers[0].onmessage({ data: { command: 'error', error: 'late error' } });
        assert.deepStrictEqual(errors, ['search failed']);
        assert.strictEqual(completed, 0);
        assert.strictEqual(pool._solutions.length, 1);

        pool.reset();
        pool.init(work, true);
        pool.solve(work);
        workers[0].onmessage({ data: { command: 'error', error: 'stale worker error' } });
        assert.deepStrictEqual(errors, ['search failed']);
        assert.ok(pool._workers.every((worker) => !worker.terminated));
        pool.stop();
    });
}

test('WorkersPool terminates a worker when Argon2id provider initialization fails', () => {
    const { TestWorker, workers } = workerHarness();
    const errors = [];
    const pool = new WorkersPool({ workerError(error) { errors.push(error); } }, false, TestWorker);
    const work = puzzle(8);
    work.challenge = CHALLENGE_ARGON2ID;
    work.puzzleBytes = new Uint8Array(48);
    pool.init(work, true);
    workers[0].onmessage({ data: { command: 'error', error: 'Argon2id provider unavailable' } });
    assert.deepStrictEqual(errors, ['Argon2id provider unavailable']);
    assert.strictEqual(workers[0].terminated, true);
});

test('worker solver dispatches the canonical v2 body through the shared Argon search', async () => {
    const body = Uint8Array.from({ length: 48 }, (_, index) => index);
    const provider = { wasm: true };
    const observed = [];
    const solver = await createWorkerSolver(CHALLENGE_ARGON2ID, body, {
        async argon(...args) {
            observed.push(args);
            return provider;
        },
        search(...args) {
            observed.push(args);
            return Uint8Array.of(3, 0, 0, 0, 0, 0, 0, 0);
        },
    });
    assert.strictEqual(solver.wasm, true);
    assert.deepStrictEqual(await solver.solve(123, 3), Uint8Array.of(3, 0, 0, 0, 0, 0, 0, 0));
    assert.deepStrictEqual(observed, [[], [body, 123, 3, provider]]);
});

test('worker solver keeps Blake dispatch and propagates Argon provider failures', async () => {
    const body = new Uint8Array(128);
    const blake = { wasm: false, solve: async () => new Uint8Array(8) };
    assert.strictEqual(await createWorkerSolver(CHALLENGE_BLAKE2B, body, { blake: async (bytes) => {
        assert.strictEqual(bytes, body);
        return blake;
    } }), blake);
    await assert.rejects(createWorkerSolver(99, body), /Unknown puzzle challenge/);
    const expected = new RangeError('Argon2id provider unavailable');
    await assert.rejects(createWorkerSolver(CHALLENGE_ARGON2ID, new Uint8Array(48), {
        argon: async () => { throw expected; },
    }), (error) => error === expected);
});

function workerHarness() {
    const workers = [];
    class TestWorker {
        constructor() {
            this.messages = [];
            this.terminated = false;
            workers.push(this);
        }

        postMessage(message) {
            this.messages.push(message);
        }

        terminate() {
            this.terminated = true;
        }
    }
    return { TestWorker, workers };
}

test('WorkersPool terminates partially constructed workers when initialization fails', () => {
    const expected = new Error('worker construction failed');
    const workers = [];
    class FailingWorker {
        constructor() {
            if (workers.length === 2) { throw expected; }
            this.terminated = false;
            workers.push(this);
        }

        terminate() {
            this.terminated = true;
        }
    }

    const pool = new WorkersPool({}, false, FailingWorker);
    assert.throws(() => pool.init(puzzle(), false), (error) => error === expected);
    assert.strictEqual(workers.length, 2);
    assert.ok(workers.every((worker) => worker.terminated));
});

for (const testCase of [
    { name: 'short solution', solution: new Uint8Array(7), wasm: true },
    { name: 'missing WASM status', solution: new Uint8Array(8), wasm: undefined },
]) {
    test(`WorkersPool rejects a ${testCase.name} response`, () => {
        const { TestWorker, workers } = workerHarness();
        const errors = [];
        let completed = 0;
        const pool = new WorkersPool({
            workerError(error) { errors.push(error); },
            workCompleted() { completed++; },
        }, false, TestWorker);
        const work = puzzle();
        pool.init(work, false);
        pool.solve(work);

        workers[0].onmessage({
            data: { command: 'solve', argument: { id: work.ID, solution: testCase.solution, wasm: testCase.wasm } },
        });

        assert.match(errors[0]?.message, /Invalid puzzle worker solve response/);
        assert.strictEqual(completed, 0);
        assert.ok(workers.every((worker) => worker.terminated));
    });
}

test('WorkersPool rejects duplicate solution lanes', () => {
    const { TestWorker, workers } = workerHarness();
    const errors = [];
    let completed = 0;
    const pool = new WorkersPool({
        workerError(error) { errors.push(error); },
        workCompleted() { completed++; },
    }, false, TestWorker);
    const work = puzzle();
    pool.init(work, false);
    pool.solve(work);
    const solution = new Uint8Array(8);

    workers[0].onmessage({
        data: { command: 'solve', argument: { id: work.ID, solution, wasm: true } },
    });
    workers[1].onmessage({
        data: { command: 'solve', argument: { id: work.ID, solution, wasm: true } },
    });

    assert.match(errors[0]?.message, /Invalid puzzle worker solve response/);
    assert.strictEqual(completed, 0);
    assert.ok(workers.every((worker) => worker.terminated));
});
