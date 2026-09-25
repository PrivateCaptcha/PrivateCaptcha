import { readUInt32LE } from './puzzle.utils.js';

const MAX_UINT32 = 0xffffffff;

function incrementCounter(nonce) {
    for (let index = nonce.length - 1; index >= 4; index--) {
        if (nonce[index] !== 0xff) {
            nonce[index]++;
            return true;
        }
        nonce[index] = 0;
    }
    return false;
}

export async function findArgon2IDSolution(canonicalBody, threshold, solutionIndex, provider) {
    if (!(canonicalBody instanceof Uint8Array) || canonicalBody.length !== 48) {
        throw new Error('Argon2id puzzle body must be exactly 48 bytes');
    }
    if (!Number.isSafeInteger(threshold) || threshold < 0 || threshold > MAX_UINT32) {
        throw new Error('Argon2id threshold must be a uint32');
    }
    if (!Number.isSafeInteger(solutionIndex) || solutionIndex < 0 || solutionIndex > 0xff) {
        throw new Error('Argon2id solution index must be a byte');
    }
    if (!provider || (typeof provider.solve !== 'function' && typeof provider.hash !== 'function')) {
        throw new Error('Argon2id provider must expose solve() or hash()');
    }

    if (typeof provider.solve === 'function') {
        return provider.solve(canonicalBody, threshold, solutionIndex);
    }

    const nonce = new Uint8Array(8);
    const output = new Uint8Array(4);
    nonce[0] = solutionIndex;

    while (true) {
        provider.hash(nonce, canonicalBody, output);
        if (readUInt32LE(output, 0) <= threshold) {
            return nonce.slice();
        }
        if (!incrementCounter(nonce)) {
            throw new Error('Argon2id nonce space exhausted');
        }
    }
}
