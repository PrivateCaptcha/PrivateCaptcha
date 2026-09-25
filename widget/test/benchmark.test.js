import test from 'node:test';
import assert from 'node:assert';
import {
    blakeSolutionAttempts,
    parseBenchmarkArgs,
    summarizeBenchmarkSamples,
    summarizeSamples,
} from './benchmark.utils.js';

test('benchmark arguments have useful independent defaults', () => {
    assert.deepStrictEqual(parseBenchmarkArgs([]), {
        algorithm: 'all',
        runs: 3,
        warmupMillis: 250,
        windowMillis: 250,
        solutions: [1],
        blakeDifficulties: [136, 152, 168],
        argonDifficulties: [0, 8, 16],
        json: false,
        help: false,
    });
});

test('benchmark arguments accept difficulty and solution matrices', () => {
    assert.deepStrictEqual(parseBenchmarkArgs([
        '--algorithm=argon2id',
        '--runs=5',
        '--warmup-ms=50',
        '--window-ms=100',
        '--solutions=1,2,4',
        '--blake-difficulties=120,136',
        '--argon-difficulties=0,12',
        '--json',
    ]), {
        algorithm: 'argon2id',
        runs: 5,
        warmupMillis: 50,
        windowMillis: 100,
        solutions: [1, 2, 4],
        blakeDifficulties: [120, 136],
        argonDifficulties: [0, 12],
        json: true,
        help: false,
    });
});

test('benchmark arguments reject unknown or unsafe values', () => {
    assert.throws(() => parseBenchmarkArgs(['--algorithm=sha256']), /algorithm/);
    assert.throws(() => parseBenchmarkArgs(['--runs=0']), /runs/);
    assert.throws(() => parseBenchmarkArgs(['--warmup-ms=-1']), /warmup-ms/);
    assert.throws(() => parseBenchmarkArgs(['--solutions=1,0']), /solutions/);
    assert.throws(() => parseBenchmarkArgs(['--memory-mib=8']), /unknown option/);
    assert.throws(() => parseBenchmarkArgs(['--argon-t=2']), /unknown option/);
    assert.throws(() => parseBenchmarkArgs(['--argon-p=2']), /unknown option/);
    assert.throws(() => parseBenchmarkArgs(['--argon-difficulties=,8']), /argon-difficulties/);
    assert.throws(() => parseBenchmarkArgs(['--argon-difficulties=8,']), /argon-difficulties/);
    assert.throws(() => parseBenchmarkArgs(['--argon-difficulties=8,,16']), /argon-difficulties/);
    assert.throws(() => parseBenchmarkArgs(['--unknown=1']), /unknown option/);
});

test('benchmark sample summaries use median and preserve the range', () => {
    assert.deepStrictEqual(summarizeSamples([30, 10, 20]), {
        median: 20,
        minimum: 10,
        maximum: 30,
    });
    assert.deepStrictEqual(summarizeSamples([40, 10, 30, 20]), {
        median: 25,
        minimum: 10,
        maximum: 40,
    });
});

test('benchmark throughput preserves paired timing samples', () => {
    assert.deepStrictEqual(summarizeBenchmarkSamples([
        { millis: 1, units: 100 },
        { millis: 10, units: 1 },
        { millis: 100, units: 10 },
    ]), {
        millis: { median: 10, minimum: 1, maximum: 100 },
        units: { median: 10, minimum: 1, maximum: 100 },
        unitsPerSecond: { median: 100, minimum: 100, maximum: 100_000 },
    });
});

test('Blake solution attempts decode the four-byte search counter', () => {
    assert.strictEqual(blakeSolutionAttempts(Uint8Array.of(0, 0, 0, 0, 0, 0, 0, 0)), 1);
    assert.strictEqual(blakeSolutionAttempts(Uint8Array.of(3, 0, 0, 0, 0, 0, 1, 0)), 257);
    assert.strictEqual(blakeSolutionAttempts(Uint8Array.of(3, 0, 0, 0, 255, 255, 255, 255)), 2 ** 32);
    assert.throws(() => blakeSolutionAttempts(new Uint8Array(7)), /8-byte/);
});
