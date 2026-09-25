import scalarBase64 from '../wasm/blake2b-solver-scalar.wasm';
import { blake2bInit, blake2bUpdate, blake2bFinal } from 'blakejs/blake2b.js';
import { findBlake2bSolution } from './puzzle.utils.js';

const BATCH_SIZE = 65536;
const NONCE_SPACE = 0x100000000;

function decodeWasm(base64) {
    const decoded = atob(base64);
    const bytes = new Uint8Array(decoded.length);
    for (let index = 0; index < decoded.length; index++) {
        bytes[index] = decoded.charCodeAt(index);
    }
    return bytes;
}

function jsHash(length) {
    const context = blake2bInit(length);
    return {
        update(input) {
            blake2bUpdate(context, input);
            return this;
        },
        digest(output) {
            output.set(blake2bFinal(context));
            return output;
        },
    };
}

export async function createSolver(puzzle, runtime = globalThis.WebAssembly) {
    if (runtime?.instantiate) {
        try {
            const { instance } = await runtime.instantiate(decodeWasm(scalarBase64));
            const wasm = instance.exports;
            new Uint8Array(wasm.memory.buffer).set(puzzle, wasm.puzzle_ptr());
            wasm.prepare_puzzle();
            return {
                kind: 'scalar',
                wasm: true,
                async solve(threshold, puzzleIndex) {
                    for (let start = 0; start < NONCE_SPACE; start += BATCH_SIZE) {
                        if (wasm.solve_batch(puzzleIndex, threshold, start, BATCH_SIZE)) {
                            const nonce = wasm.get_solution_nonce() >>> 0;
                            const solution = puzzle.slice(120, 128);
                            solution[0] = puzzleIndex;
                            solution[4] = nonce >>> 24;
                            solution[5] = nonce >>> 16;
                            solution[6] = nonce >>> 8;
                            solution[7] = nonce;
                            return solution;
                        }
                        await new Promise(resolve => setTimeout(resolve, 0));
                    }
                    return new Uint8Array(0);
                },
            };
        } catch (error) {
            console.warn('[privatecaptcha][worker] scalar WASM unavailable', error);
        }
    }

    return {
        kind: 'js',
        wasm: false,
        async solve(threshold, puzzleIndex, debug) {
            return findBlake2bSolution(puzzle, threshold, puzzleIndex, debug, jsHash).slice();
        },
    };
}
