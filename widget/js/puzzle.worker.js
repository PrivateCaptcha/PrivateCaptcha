'use strict';

import * as blake2bModule from './blake2-wrapper.js';
import { thresholdFromDifficulty, findSolution } from './puzzle.utils.js';
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
            const solution = findSolution(puzzleBuffer, threshold, puzzleIndex, debug, blake2b);
            self.postMessage({ command: command, argument: { id: puzzleID, solution: solution, wasm: useWasm } });
            break;
        default:
            break;
    }
};
