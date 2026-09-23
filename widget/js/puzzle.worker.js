'use strict';

import { createSolver } from './blake2-solver.js';
import { thresholdFromDifficulty } from './puzzle.utils.js';

let solver = null;
let puzzleID = null;

self.onmessage = async (event) => {
    const { command, argument } = event.data;

    switch (command) {
        case 'init': {
            const { id, buffer } = argument;
            puzzleID = id;
            solver = await createSolver(buffer);
            self.postMessage({ command: 'init' });
            break;
        }
        case 'solve': {
            const { difficulty, puzzleIndex, debug } = argument;
            const threshold = thresholdFromDifficulty(difficulty);
            const solution = await solver.solve(threshold, puzzleIndex, debug);
            self.postMessage({ command: 'solve', argument: { id: puzzleID, solution, wasm: solver.wasm } });
            break;
        }
        default:
            break;
    }
};
