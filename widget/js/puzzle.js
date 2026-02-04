'use strict';

import { decode } from 'base64-arraybuffer';

const PUZZLE_BUFFER_LENGTH = 128;
// RequestTimeout, Conflict, TooManyRequests
const ACCEPTABLE_CLIENT_ERRORS = [408, 409, 429];
const DEFAULT_TIMEOUT_MS = 5000;

export async function getPuzzle(endpoint, sitekey, options = {}) {
    const timeoutMs = options.timeout ?? DEFAULT_TIMEOUT_MS;
    try {
        const response = await fetchWithBackoff(`${endpoint}?sitekey=${sitekey}`,
            { headers: [["x-pc-captcha-version", "1"]], mode: "cors" },
            5 /*max attempts*/,
            800 /*initialDelay*/,
            6000 /*maxDelay*/,
            timeoutMs
        );

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

async function fetchWithBackoff(url, options, maxAttempts, initialDelay = 800, maxDelay = 6000, timeoutMs = DEFAULT_TIMEOUT_MS) {
    const controller = new AbortController();
    const { signal } = controller;
    let timeoutId = null;

    for (let attempt = 0; attempt < maxAttempts; attempt++) {
        if (attempt > 0) {
            const delay = Math.min(initialDelay * Math.pow(2, attempt), maxDelay);
            try {
                await wait(delay, signal);
            } catch (err) {
                if (signal.aborted) {
                    throw new Error('Fetch timed out');
                }
                throw err;
            }
        }

        if (timeoutId) {
            clearTimeout(timeoutId);
        }
        timeoutId = setTimeout(() => controller.abort(new Error('Fetch timed out')), timeoutMs);

        try {
            const response = await fetch(url, { ...options, signal });
            clearTimeout(timeoutId);
            if (response.ok) {
                return response;
            } else {
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
            clearTimeout(timeoutId);
            if (signal.aborted) {
                throw new Error('Fetch timed out');
            }
            console.error('[privatecaptcha]', err);
        }
    }

    throw new Error('Captcha puzzle load failed after maximum retry attempts');
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
