import { createSolver } from './blake2-solver.js';

export async function createWorkerSolver(challenge, body) {
    if (challenge !== 0) {
        throw new Error(`Unsupported puzzle challenge: ${challenge}`);
    }
    return createSolver(body);
}
