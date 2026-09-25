import scalarBase64 from '../wasm/argon2id-solver-scalar.wasm';
import { argon2id as nobleArgon2ID } from '@noble/hashes/argon2.js';

// Fixed Argon2id protocol parameters: m=16 MiB, t=1, p=1.
const MEMORY_KIB = 16 * 1024;
const TAG_LENGTH = 32;
const PASSWORD_LENGTH = 128;
const BODY_LENGTH = 48;
const NONCE_LENGTH = 8;
const BATCH_SIZE = 65536;
const NONCE_SPACE = 0x100000000;

const defaultDependencies = {
    instantiateWasm: (bytes) => WebAssembly.instantiate(bytes),
    nobleHash: nobleArgon2ID,
};

function validateBytes(value, length, name) {
    if (!(value instanceof Uint8Array) || value.length !== length) {
        throw new Error(`Argon2id ${name} must be exactly ${length} bytes`);
    }
}

function normalizedPassword(nonce, canonicalBody) {
    const password = new Uint8Array(PASSWORD_LENGTH);
    password.set(canonicalBody);
    password.set(nonce, PASSWORD_LENGTH - NONCE_LENGTH);
    return password;
}

function copyPrefix(tag, output) {
    if (!(tag instanceof Uint8Array) || tag.length !== TAG_LENGTH) {
        throw new Error('Argon2id implementation must return a 32-byte tag');
    }
    output.set(tag.subarray(0, 4));
    return output;
}

function createWasmProvider(wasm) {
    const puzzle = new Uint8Array(wasm.memory.buffer, wasm.puzzle_ptr(), PASSWORD_LENGTH);
    const digest = new Uint8Array(wasm.memory.buffer, wasm.digest_ptr(), TAG_LENGTH);
    return {
        implementation: 'argon2id-scalar',
        wasm: true,
        hash(nonce, canonicalBody, output) {
            validateBytes(nonce, NONCE_LENGTH, 'password');
            validateBytes(canonicalBody, BODY_LENGTH, 'salt');
            validateBytes(output, 4, 'output');
            puzzle.fill(0);
            puzzle.set(canonicalBody);
            puzzle.set(nonce, PASSWORD_LENGTH - NONCE_LENGTH);
            if (wasm.hash_puzzle() !== 0) {
                throw new Error('Argon2id WASM hash failed');
            }
            return copyPrefix(digest, output);
        },
        async solve(canonicalBody, threshold, index) {
            validateBytes(canonicalBody, BODY_LENGTH, 'salt');
            if (!Number.isSafeInteger(threshold) || threshold < 0 || threshold > 0xffffffff) {
                throw new Error('Argon2id threshold must be a uint32');
            }
            if (!Number.isSafeInteger(index) || index < 0 || index > 0xff) {
                throw new Error('Argon2id solution index must be a byte');
            }
            puzzle.fill(0);
            puzzle.set(canonicalBody);
            wasm.prepare_puzzle();
            for (let start = 0; start < NONCE_SPACE; start += BATCH_SIZE) {
                const result = wasm.solve_batch(index, threshold, start, BATCH_SIZE);
                if (result === 1) {
                    const nonce = wasm.get_solution_nonce() >>> 0;
                    return Uint8Array.of(index, 0, 0, 0, nonce >>> 24, nonce >>> 16, nonce >>> 8, nonce);
                }
                if (result !== 0) throw new Error('Argon2id WASM search failed');
                await new Promise((resolve) => setTimeout(resolve, 0));
            }
            throw new Error('Argon2id nonce space exhausted');
        },
    };
}

function createNobleProvider(dependencies) {
    return {
        implementation: 'argon2id-noble',
        wasm: false,
        hash(nonce, canonicalBody, output) {
            validateBytes(nonce, NONCE_LENGTH, 'password');
            validateBytes(canonicalBody, BODY_LENGTH, 'salt');
            validateBytes(output, 4, 'output');
            const password = normalizedPassword(nonce, canonicalBody);
            const tag = dependencies.nobleHash(password, password.subarray(0, 16), {
                t: 1,
                m: MEMORY_KIB,
                p: 1,
                version: 0x13,
                dkLen: TAG_LENGTH,
                maxmem: MEMORY_KIB * 1024,
            });
            return copyPrefix(tag, output);
        },
    };
}

async function instantiate(base64, dependencies) {
    const bytes = Uint8Array.from(atob(base64), (character) => character.charCodeAt(0));
    const result = await dependencies.instantiateWasm(bytes);
    const wasm = (result?.instance ?? result)?.exports;
    if (!wasm?.memory || typeof wasm.hash_puzzle !== 'function' ||
        typeof wasm.solve_batch !== 'function' || typeof wasm.prepare_puzzle !== 'function') {
        throw new Error('Argon2id WASM instantiation returned no solver');
    }
    return wasm;
}

export async function loadArgon2IDProvider(overrides = {}) {
    const dependencies = { ...defaultDependencies, ...overrides };
    try {
        return createWasmProvider(await instantiate(scalarBase64, dependencies));
    } catch {
        return createNobleProvider(dependencies);
    }
}
