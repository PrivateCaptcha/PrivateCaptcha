'use strict';

import { createWorkerSolver } from './worker-solver.js';
import { thresholdFromDifficulty } from './puzzle.utils.js';

let solver = null;
let puzzleID = null;

self.onmessage = async (event) => {
    const { command, argument } = event.data;

    try {
        switch (command) {
            case 'init': {
                const { id, buffer, challenge } = argument;
                solver = null;
                solver = await createWorkerSolver(challenge, buffer);
                puzzleID = id;
                self.postMessage({ command: 'init' });
                break;
            }
            case 'solve': {
                if (!solver) { throw new Error('Puzzle worker is not initialized'); }
                const { difficulty, puzzleIndex, debug } = argument;
                const threshold = thresholdFromDifficulty(difficulty);
                const solution = await solver.solve(threshold, puzzleIndex, debug);
                if (!(solution instanceof Uint8Array) || solution.length !== 8) {
                    throw new Error('Puzzle nonce space exhausted');
                }
                self.postMessage({ command: 'solve', argument: { id: puzzleID, solution, wasm: solver.wasm } });
                break;
            }
            default:
                break;
        }
    } catch (error) {
        self.postMessage({ command: 'error', error: error?.message ?? String(error) });
    }
};
