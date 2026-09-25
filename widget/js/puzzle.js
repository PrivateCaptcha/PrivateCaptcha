'use strict';

import { decode } from 'base64-arraybuffer';
import { readUInt32LE } from './puzzle.utils.js';

const PUZZLE_BUFFER_LENGTH = 128;
const PUZZLE_V1_LENGTH = 47;
const PUZZLE_V2_LENGTH = 48;
const PUZZLE_VERSION_1 = 1;
const PUZZLE_VERSION_2 = 2;
export const CHALLENGE_BLAKE2B = 0;
export const CHALLENGE_ARGON2ID = 1;
// RequestTimeout, Conflict, TooManyRequests
const ACCEPTABLE_CLIENT_ERRORS = [408, 409, 429];
const DEFAULT_TIMEOUT_MS = 5000;
const DEFAULT_GLOBAL_TIMEOUT_MS = 30000;

export async function getPuzzle(endpoint, sitekey, options = {}) {
    try {
        const separator = endpoint.includes('?') ? '&' : '?';
        const response = await fetchWithBackoff(`${endpoint}${separator}sitekey=${encodeURIComponent(sitekey)}`, {
            fetchOptions: { headers: [["x-pc-captcha-version", globalThis.__privateCaptchaExtended === true ? "2" : "1"]], mode: "cors" },
            maxAttempts: options.attempts ?? 5,
            initialDelay: 800,
            maxDelay: 6000,
            timeoutMs: options.timeout ?? DEFAULT_TIMEOUT_MS,
            globalTimeoutMs: options.globalTimeout ?? DEFAULT_GLOBAL_TIMEOUT_MS
        });

        if (response.ok) {
            const data = await response.text();
            const notice = response.headers.get('X-PC-Widget-Notice');
            return { data, notice };
        } else {
            let json = await response.json();
            if (json && json.error) {
                throw new Error(json.error);
            }
        }
    } catch (err) {
        console.error('[privatecaptcha]', err);
        throw err;
    }

    throw new Error('Internal error');
};

function wait(delay, signal) {
    return new Promise((resolve, reject) => {
        const timeoutId = setTimeout(resolve, delay);
        if (signal) {
            signal.addEventListener('abort', () => {
                clearTimeout(timeoutId);
                reject(signal.reason || new Error('Aborted'));
            }, { once: true });
        }
    });
}

async function fetchWithBackoff(url, options = {}) {
    const {
        fetchOptions = {},
        maxAttempts = 5,
        initialDelay = 800,
        maxDelay = 6000,
        timeoutMs = DEFAULT_TIMEOUT_MS,
        globalTimeoutMs = DEFAULT_GLOBAL_TIMEOUT_MS
    } = options;

    // Global AbortController is used to abort wait() between retries when global timeout occurs
    const globalController = new AbortController();
    const { signal: globalSignal } = globalController;
    let globalTimeoutId = setTimeout(() => globalController.abort(new Error('Fetch timed out')), globalTimeoutMs);
    let lastError = null;

    for (let attempt = 0; attempt < maxAttempts; attempt++) {
        if (attempt > 0) {
            const delay = Math.min(initialDelay * Math.pow(2, attempt), maxDelay);
            try {
                await wait(delay, globalSignal);
            } catch (err) {
                clearTimeout(globalTimeoutId);
                if (globalSignal.aborted) {
                    lastError = 'Global time out';
                    const error = new Error('Fetch timed out');
                    error.internalError = lastError;
                    throw error;
                }
                throw err;
            }
        }

        // Per-call AbortController is used for individual fetch timeout
        const fetchController = new AbortController();
        const { signal: fetchSignal } = fetchController;
        const fetchTimeoutId = setTimeout(() => fetchController.abort(new Error('Fetch timed out')), timeoutMs);

        // If global timeout fires, abort the current fetch as well
        const globalAbortHandler = () => fetchController.abort(new Error('Fetch timed out'));
        globalSignal.addEventListener('abort', globalAbortHandler, { once: true });

        try {
            const response = await fetch(url, { ...fetchOptions, signal: fetchSignal });
            clearTimeout(fetchTimeoutId);
            globalSignal.removeEventListener('abort', globalAbortHandler);
            if (response.ok) {
                clearTimeout(globalTimeoutId);
                return response;
            } else {
                lastError = `HTTP ${response.status}`;
                console.warn('[privatecaptcha]', `HTTP request failed. url=${url} status=${response.status}`);
            }

            if ((response.status >= 400) && (response.status < 500) &&
                !ACCEPTABLE_CLIENT_ERRORS.includes(response.status)) {
                // we don't retry on most client errors
                break;
            } else {
                continue;
            }
        } catch (err) {
            clearTimeout(fetchTimeoutId);
            globalSignal.removeEventListener('abort', globalAbortHandler);
            if (globalSignal.aborted) {
                clearTimeout(globalTimeoutId);
                lastError = 'Global time out';
                const error = new Error('Fetch timed out');
                error.internalError = lastError;
                throw error;
            }
            if (fetchSignal.aborted) {
                // Per-call timeout - continue to next attempt
                lastError = 'Fetch timed out';
                console.warn('[privatecaptcha]', `Fetch attempt ${attempt + 1} timed out`);
                continue;
            }
            lastError = err.message || String(err);
            console.error('[privatecaptcha]', err);
        }
    }

    clearTimeout(globalTimeoutId);
    const error = new Error('Captcha puzzle load failed after maximum retry attempts');
    error.internalError = lastError;
    throw error;
}

function readUInt64LE(binaryData, offset) {
    return (
        BigInt(readUInt32LE(binaryData, offset)) +
        (BigInt(readUInt32LE(binaryData, offset + 4)) << 32n)
    );
}

export class Puzzle {
    constructor(rawData) {
        this.puzzleBuffer = null;
        this.puzzleBytes = null;

        this.version = null;
        this.challenge = null;
        this.ID = null;
        this.difficulty = null;
        this.solutionsCount = null;
        this.expirationTimestamp = null;
        this.userData = null;

        this.signature = null;

        this.parse(rawData);
        this.rawData = rawData;
    }

    parse(rawData) {
        const parts = rawData.split('.');
        if (parts.length !== 2) {
            throw new Error(`Invalid amount of parts: ${parts.length}`);
        }

        const buffer = parts[0];
        this.signature = parts[1];

        const data = new Uint8Array(decode(buffer));
        if (data.length < 1) {
            throw new Error('Puzzle body is empty');
        }

        this.version = data[0];
        let expectedLength;
        let offset = 1;
        switch (this.version) {
            case PUZZLE_VERSION_1:
                expectedLength = PUZZLE_V1_LENGTH;
                this.challenge = CHALLENGE_BLAKE2B;
                break;
            case PUZZLE_VERSION_2:
                expectedLength = PUZZLE_V2_LENGTH;
                this.challenge = data[offset++];
                if (this.challenge !== CHALLENGE_BLAKE2B && this.challenge !== CHALLENGE_ARGON2ID) {
                    throw new Error(`Unknown puzzle challenge: ${this.challenge}`);
                }
                break;
            default:
                throw new Error(`Unknown puzzle version: ${this.version}`);
        }
        if (data.length !== expectedLength) {
            throw new Error(`Invalid puzzle body length: ${data.length}`);
        }

        this.puzzleBytes = data.slice();
        offset += 16; // propertyID

        this.ID = readUInt64LE(data, offset);
        offset += 8;

        this.difficulty = data[offset];
        offset += 1;

        this.solutionsCount = data[offset];
        offset += 1;

        this.expirationTimestamp = readUInt32LE(data, offset);
        offset += 4;

        const userDataSize = 16;
        const userDataStart = offset;
        offset += 4; // AccountID is the first four bytes of userData
        this.userData = data.slice(userDataStart, userDataStart + userDataSize);
        offset += userDataSize - 4;

        let sourceBuffer = this.puzzleBytes;
        if (sourceBuffer.length < PUZZLE_BUFFER_LENGTH) {
            const enlargedBuffer = new Uint8Array(PUZZLE_BUFFER_LENGTH);
            enlargedBuffer.set(sourceBuffer);
            this.puzzleBuffer = enlargedBuffer;
        } else {
            this.puzzleBuffer = sourceBuffer;
        }
    }

    isZero() {
        return (this.ID === 0n) && (this.difficulty === 0) && (this.expirationTimestamp === 0);
    }

    expirationMillis() {
        if (!this.expirationTimestamp) { return 0; }

        const expiration = new Date(this.expirationTimestamp * 1000);
        const currentDate = new Date();
        const diff = expiration - currentDate;
        return diff;
    }
};
