import { findArgon2IDSolution } from '../js/argon2-search.js';
import { createSolver } from '../js/blake2-solver.js';
import { loadArgon2IDProvider } from '../js/hash-providers.js';
import { thresholdFromDifficulty } from '../js/puzzle.utils.js';
import { blake2b } from 'blakejs/blake2b.js';
import {
    blakeSolutionAttempts,
    parseBenchmarkArgs,
    summarizeBenchmarkSamples,
} from './benchmark.utils.js';

const MIB = 1024 * 1024;
const HELP = `Usage: make bench-widget BENCH_ARGS='[options]'

Options:
  --algorithm=all|blake2b|argon2id
  --runs=N
  --warmup-ms=N
  --window-ms=N
  --solutions=N[,N...]
  --blake-difficulties=N[,N...]
  --argon-difficulties=N[,N...]
  --json
  --help`;

function memoryUsage() {
    globalThis.gc?.();
    const usage = process.memoryUsage();
    return { rss: usage.rss, external: usage.external };
}

function memoryDelta(before, after) {
    return {
        rssMiB: (after.rss - before.rss) / MIB,
        externalMiB: (after.external - before.external) / MIB,
    };
}

function runWindow(operation, minimumMillis) {
    const started = performance.now();
    let operations = 0;
    let units = 0;
    let elapsed;
    do {
        units += operation();
        operations++;
        elapsed = performance.now() - started;
    } while (elapsed < minimumMillis);
    return {
        millis: elapsed / operations,
        units: units / operations,
    };
}

function measure(operation, options) {
    if (options.warmupMillis > 0) {
        runWindow(operation, options.warmupMillis);
    }
    const samples = Array.from({ length: options.runs }, () => runWindow(operation, options.windowMillis));
    return summarizeBenchmarkSamples(samples);
}

async function runAsyncWindow(operation, minimumMillis) {
    const started = performance.now();
    let operations = 0;
    let units = 0;
    let elapsed;
    do {
        units += await operation();
        operations++;
        elapsed = performance.now() - started;
    } while (elapsed < minimumMillis);
    return { millis: elapsed / operations, units: units / operations };
}

async function measureAsync(operation, options) {
    if (options.warmupMillis > 0) {
        await runAsyncWindow(operation, options.warmupMillis);
    }
    const samples = [];
    for (let run = 0; run < options.runs; run++) {
        samples.push(await runAsyncWindow(operation, options.windowMillis));
    }
    return summarizeBenchmarkSamples(samples);
}

function round(value, digits = 3) {
    const scale = 10 ** digits;
    return Math.round(value * scale) / scale;
}

function resultRow(profile, result, options, memory) {
    const medianMillis = result.millis.median;
    return {
        ...profile,
        runs: options.runs,
        medianMillis: round(medianMillis, 6),
        minimumMillis: round(result.millis.minimum, 6),
        maximumMillis: round(result.millis.maximum, 6),
        medianHashes: round(result.units.median, 1),
        hashesPerSecond: round(result.unitsPerSecond.median, 1),
        rssDeltaMiB: round(memory.rssMiB),
        externalDeltaMiB: round(memory.externalMiB),
    };
}

function blakeSolveOperation(difficulty, solutions) {
    const base = Uint8Array.from({ length: 128 }, (_, index) => index);
    const threshold = thresholdFromDifficulty(difficulty);
    let puzzleID = 0;
    return async () => {
        const body = base.slice();
        new DataView(body.buffer).setUint32(0, puzzleID++);
        const solver = await createSolver(body);
        let hashes = 0;
        for (let index = 0; index < solutions; index++) {
            const solution = await solver.solve(threshold, index, false);
            if (solution.length !== 8) {
                throw new Error('Blake2b nonce space exhausted');
            }
            hashes += blakeSolutionAttempts(solution);
        }
        return hashes;
    };
}

function argonSolveOperation(difficulty, solutions, provider) {
    const threshold = thresholdFromDifficulty(difficulty);
    const body = new Uint8Array(48);
    body[0] = 2;
    body[1] = 1;
    body[26] = difficulty;
    body[27] = solutions;
    let puzzleID = 0;
    return async () => {
        new DataView(body.buffer).setUint32(2, puzzleID++);
        let hashes = 0;
        for (let index = 0; index < solutions; index++) {
            const solution = await findArgon2IDSolution(body, threshold, index, provider);
            hashes += blakeSolutionAttempts(solution);
        }
        return hashes;
    };
}

async function benchmarkBlake(options) {
    const before = memoryUsage();
    const input = new Uint8Array(128);
    const hashMemory = memoryDelta(before, memoryUsage());
    const rows = [resultRow({
        algorithm: 'blake2b', operation: 'hash', provider: 'blake2b-js',
        wasm: false, mMiB: null, t: null, p: null, difficulty: null, solutions: null,
    }, measure(() => { blake2b(input, null, 32); return 1; }, options), options, hashMemory)];

    const solveBefore = memoryUsage();
    const solver = await createSolver(input);
    const solveMemory = memoryDelta(solveBefore, memoryUsage());

    for (const difficulty of options.blakeDifficulties) {
        for (const solutions of options.solutions) {
            rows.push(resultRow({
                algorithm: 'blake2b', operation: 'solve', provider: `blake2b-${solver.kind}`,
                wasm: solver.wasm, mMiB: null, t: null, p: null, difficulty, solutions,
            }, await measureAsync(blakeSolveOperation(difficulty, solutions), options), options, solveMemory));
        }
    }
    return rows;
}

async function benchmarkArgon(options) {
    const rows = [];
    const before = memoryUsage();
    const provider = await loadArgon2IDProvider();
    const memory = memoryDelta(before, memoryUsage());
    const password = new Uint8Array(8);
    const salt = new Uint8Array(48);
    const output = new Uint8Array(4);
    rows.push(resultRow({
        algorithm: 'argon2id', operation: 'hash', provider: provider.implementation,
        wasm: provider.wasm, mMiB: 16, t: 1, p: 1,
        difficulty: null, solutions: null,
    }, measure(() => { provider.hash(password, salt, output); return 1; }, options), options, memory));

    for (const difficulty of options.argonDifficulties) {
        for (const solutions of options.solutions) {
            rows.push(resultRow({
                algorithm: 'argon2id', operation: 'solve', provider: provider.implementation,
                wasm: provider.wasm, mMiB: 16, t: 1, p: 1,
                difficulty, solutions,
            }, await measureAsync(argonSolveOperation(difficulty, solutions, provider), options), options, memory));
        }
    }
    return rows;
}

async function main() {
    const options = parseBenchmarkArgs(process.argv.slice(2));
    if (options.help) {
        console.log(HELP);
        return;
    }

    const rows = [];
    if (options.algorithm === 'all' || options.algorithm === 'blake2b') {
        rows.push(...await benchmarkBlake(options));
    }
    if (options.algorithm === 'all' || options.algorithm === 'argon2id') {
        rows.push(...await benchmarkArgon(options));
    }

    if (options.json) {
        console.log(JSON.stringify({ runtime: process.version, rows }, null, 2));
    } else {
        console.log(`Node ${process.version}; memory deltas are approximate process measurements.`);
        console.table(rows);
    }
}

main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
});
