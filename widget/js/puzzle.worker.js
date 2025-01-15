'use strict';

import blake2bModule from './blake2-wrapper.js';
let blake2b = blake2bModule.impl;
let blake2bInitialized = false;
let puzzleBuffer = null;
let puzzleID = null;
let useWasm = false;

if (blake2bModule.ready) {
    blake2bModule.ready(() => {
        useWasm = blake2bModule.WASM_LOADED;
        console.debug('[privatecaptcha][worker] Hasher loaded. wasm=' + useWasm);
        blake2b = blake2bModule.impl;
        blake2bInitialized = true;
        if (puzzleBuffer) {
            self.postMessage({ command: "init" });
        }
    });
} else {
    console.warn('[privatecaptcha][worker] Blake2b ready() is not defined');
}

function readUInt32LE(buffer, offset) {
    return (
        buffer[offset] |
        (buffer[offset + 1] << 8) |
        (buffer[offset + 2] << 16) |
        (buffer[offset + 3] << 24)
    ) >>> 0;
}

function thresholdFromDifficulty(d) {
    return (Math.pow(2, Math.floor((255.999 - d) / 8.0))) >>> 0;
}

function findSolution(threshold, puzzleIndex, debug) {
    const length = puzzleBuffer.length;
    if (debug) {
        console.debug(`[privatecaptcha][worker] looking for a solution. threshold=${threshold} puzzleID=${puzzleIndex} length=${length}`);
    }
    puzzleBuffer[length - 8] = puzzleIndex;

    let hash = new Uint8Array(32);

    for (let i = 0; i < 256; i++) {
        puzzleBuffer[length - 1 - 3] = i;

        for (let j = 0; j < 256; j++) {
            puzzleBuffer[length - 1 - 2] = j;

            for (let k = 0; k < 256; k++) {
                puzzleBuffer[length - 1 - 1] = j;

                for (let l = 0; l < 256; l++) {
                    puzzleBuffer[length - 1 - 0] = l;

                    hash.fill(0);
                    blake2b(hash.length).update(puzzleBuffer).digest(hash);
                    const prefix = readUInt32LE(hash, 0);

                    if (prefix <= threshold) {
                        if (debug) {
                            console.debug(`[privatecaptcha][worker] found solution. prefix=${prefix} threshold=${threshold}`);
                        }
                        return puzzleBuffer.subarray(length - 8);
                    }
                }
            }
        }
    }

    return new Uint8Array(0);
}

self.onmessage = (event) => {
    const { command, argument } = event.data;

    switch (command) {
        case "init":
            const { id, buffer } = argument;
            puzzleID = id;
            puzzleBuffer = buffer;

            //importScripts('./blakejs/blake2b.js')
            // ack
            if (blake2bInitialized) {
                self.postMessage({ command: "init" });
            }
            break;
        case "solve":
            const { difficulty, puzzleIndex, debug } = argument;
            const threshold = thresholdFromDifficulty(difficulty);
            const solution = findSolution(threshold, puzzleIndex, debug);
            self.postMessage({ command: command, argument: { id: puzzleID, solution: solution, wasm: useWasm } });
            break;
        default:
            break;
    }
};
