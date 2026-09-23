import { once } from 'node:events';
import { createInterface } from 'node:readline';
import { createSolver } from './js/blake2-solver.js';
import { Puzzle } from './js/puzzle.js';
import { thresholdFromDifficulty } from './js/puzzle.utils.js';

async function main() {
    let lineNumber = 0;
    for await (const rawData of createInterface({ input: process.stdin, crlfDelay: Infinity })) {
        lineNumber++;
        try {
            const puzzle = new Puzzle(rawData);
            const solver = await createSolver(puzzle.puzzleBuffer);
            if (!solver.wasm) {
                throw new Error('WASM solver unavailable');
            }

            const solutions = new Uint8Array(7 + puzzle.solutionsCount * 8);
            solutions.set([1, 0, 1], 0); // metadata: version, error, WASM flag
            const threshold = thresholdFromDifficulty(puzzle.difficulty);
            for (let index = 0; index < puzzle.solutionsCount; index++) {
                const solution = await solver.solve(threshold, index);
                if (solution.length !== 8) {
                    throw new Error(`no solution for index ${index}`);
                }
                solutions.set(solution, 7 + index * 8);
            }

            const payload = `${Buffer.from(solutions).toString('base64')}.${rawData}\n`;
            if (!process.stdout.write(payload)) {
                await once(process.stdout, 'drain');
            }
        } catch (error) {
            throw new Error(`puzzle ${lineNumber}: ${error.message}`, { cause: error });
        }
    }
}

main().catch(error => {
    console.error(error);
    process.exitCode = 1;
});
