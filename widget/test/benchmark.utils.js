const DEFAULTS = Object.freeze({
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

function integer(value, name, minimum, maximum) {
    const normalized = typeof value === 'string' ? value.trim() : value;
    if (normalized === '') {
        throw new Error(`${name} must contain integers`);
    }
    const parsed = Number(normalized);
    if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
        throw new Error(`${name} must be an integer between ${minimum} and ${maximum}`);
    }
    return parsed;
}

function integerList(value, name, minimum, maximum) {
    if (!value) {
        throw new Error(`${name} must not be empty`);
    }
    return value.split(',').map((item) => integer(item, name, minimum, maximum));
}

export function parseBenchmarkArgs(args) {
    const result = {
        ...DEFAULTS,
        solutions: [...DEFAULTS.solutions],
        blakeDifficulties: [...DEFAULTS.blakeDifficulties],
        argonDifficulties: [...DEFAULTS.argonDifficulties],
    };

    for (const argument of args) {
        if (argument === '--json') {
            result.json = true;
        } else if (argument === '--help') {
            result.help = true;
        } else if (argument.startsWith('--algorithm=')) {
            result.algorithm = argument.slice('--algorithm='.length);
            if (!['all', 'blake2b', 'argon2id'].includes(result.algorithm)) {
                throw new Error('algorithm must be all, blake2b, or argon2id');
            }
        } else if (argument.startsWith('--runs=')) {
            result.runs = integer(argument.slice('--runs='.length), 'runs', 1, 100);
        } else if (argument.startsWith('--warmup-ms=')) {
            result.warmupMillis = integer(argument.slice('--warmup-ms='.length), 'warmup-ms', 0, 60_000);
        } else if (argument.startsWith('--window-ms=')) {
            result.windowMillis = integer(argument.slice('--window-ms='.length), 'window-ms', 1, 60_000);
        } else if (argument.startsWith('--solutions=')) {
            result.solutions = integerList(argument.slice('--solutions='.length), 'solutions', 1, 255);
        } else if (argument.startsWith('--blake-difficulties=')) {
            result.blakeDifficulties = integerList(argument.slice('--blake-difficulties='.length), 'blake-difficulties', 0, 255);
        } else if (argument.startsWith('--argon-difficulties=')) {
            result.argonDifficulties = integerList(argument.slice('--argon-difficulties='.length), 'argon-difficulties', 0, 255);
        } else {
            throw new Error(`unknown option: ${argument}`);
        }
    }

    return result;
}

export function summarizeSamples(samples) {
    if (samples.length === 0) {
        throw new Error('at least one benchmark sample is required');
    }
    const sorted = [...samples].sort((left, right) => left - right);
    const middle = Math.floor(sorted.length / 2);
    const median = sorted.length % 2 === 0
        ? (sorted[middle - 1] + sorted[middle]) / 2
        : sorted[middle];
    return {
        median,
        minimum: sorted[0],
        maximum: sorted.at(-1),
    };
}

export function summarizeBenchmarkSamples(samples) {
    return {
        millis: summarizeSamples(samples.map((sample) => sample.millis)),
        units: summarizeSamples(samples.map((sample) => sample.units)),
        unitsPerSecond: summarizeSamples(samples.map((sample) => sample.units * 1000 / sample.millis)),
    };
}

export function blakeSolutionAttempts(solution) {
    if (!(solution instanceof Uint8Array) || solution.length !== 8) {
        throw new Error('Blake2b solution must be an 8-byte nonce');
    }
    return (((solution[4] * 256 + solution[5]) * 256 + solution[6]) * 256 + solution[7]) + 1;
}
