import { createSolver } from './blake2-solver.js';
import { loadArgon2IDProvider } from './hash-providers.js';
import { findArgon2IDSolution } from './argon2-search.js';

const CHALLENGE_BLAKE2B = 0;
const CHALLENGE_ARGON2ID = 1;

export async function createWorkerSolver(challenge, body, providers = {}) {
    if (challenge === CHALLENGE_BLAKE2B) {
        return (providers.blake ?? createSolver)(body);
    }
    if (challenge !== CHALLENGE_ARGON2ID) {
        throw new Error(`Unknown puzzle challenge: ${challenge}`);
    }

    const provider = await (providers.argon ?? loadArgon2IDProvider)();
    return {
        wasm: provider.wasm,
        solve(threshold, index) {
            return (providers.search ?? findArgon2IDSolution)(body, threshold, index, provider);
        },
    };
}
