'use strict';

import { decode } from 'base64-arraybuffer';

const PUZZLE_BUFFER_LENGTH = 128;
// RequestTimeout, Conflict, TooManyRequests
const ACCEPTABLE_CLIENT_ERRORS = [408, 409, 429];
const DEFAULT_TIMEOUT_MS = 5000;
const DEFAULT_GLOBAL_TIMEOUT_MS = 30000;

export async function getPuzzle(endpoint, sitekey, options = {}) {
    try {
        const response = await fetchWithBackoff(`${endpoint}?sitekey=${sitekey}`, {
            fetchOptions: { headers: [["x-pc-captcha-version", "1"]], mode: "cors" },
            maxAttempts: options.attempts ?? 5,
            initialDelay: 800,
            maxDelay: 6000,
            timeoutMs: options.timeout ?? DEFAULT_TIMEOUT_MS,
            globalTimeoutMs: options.globalTimeout ?? DEFAULT_GLOBAL_TIMEOUT_MS
        });

        if (response.ok) {
            const data = await response.text();
            return data;
        } else {
            let json = await response.json();
            if (json && json.error) {
                throw Error(json.error);
            }
        }
    } catch (err) {
        console.error('[privatecaptcha]', err);
        throw err;
    }

    throw Error('Internal error');
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

function readUInt32LE(binaryData, offset) {
    return (
        binaryData[offset] |
        (binaryData[offset + 1] << 8) |
        (binaryData[offset + 2] << 16) |
        (binaryData[offset + 3] << 24)
    ) >>> 0;
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
            throw Error(`Invalid amount of parts: ${parts.length}`);
        }

        const buffer = parts[0];
        this.signature = parts[1];

        const data = new Uint8Array(decode(buffer));
        let offset = 0;

        offset += 1; // version
        offset += 16; // propertyID

        this.ID = readUInt64LE(data, offset);
        offset += 8;

        this.difficulty = data[offset];
        offset += 1;

        this.solutionsCount = data[offset];
        offset += 1;

        this.expirationTimestamp = readUInt32LE(data, offset);
        offset += 4;

        offset += 4; // AccountID

        const userDataSize = 16;
        this.userData = data.slice(offset, offset + userDataSize);
        offset += userDataSize;

        let sourceBuffer = data;
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
