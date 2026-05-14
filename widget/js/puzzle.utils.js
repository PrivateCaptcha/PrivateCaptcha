export function readUInt32LE(binaryData, offset) {
    return (
        binaryData[offset] |
        (binaryData[offset + 1] << 8) |
        (binaryData[offset + 2] << 16) |
        (binaryData[offset + 3] << 24)
    ) >>> 0;
}

export function thresholdFromDifficulty(d) {
    return (Math.pow(2, (255.999 - d) / 8.0)) >>> 0;
}

export function findSolution(buffer, threshold, puzzleIndex, debug, hasher) {
    const length = buffer.length;
    if (debug) {
        console.debug(`[privatecaptcha][worker] looking for a solution. threshold=${threshold} puzzleID=${puzzleIndex} length=${length}`);
    }
    buffer[length - 8] = puzzleIndex;

    let hash = new Uint8Array(32);

    for (let i = 0; i < 256; i++) {
        buffer[length - 1 - 3] = i;

        for (let j = 0; j < 256; j++) {
            buffer[length - 1 - 2] = j;

            for (let k = 0; k < 256; k++) {
                buffer[length - 1 - 1] = k;

                for (let l = 0; l < 256; l++) {
                    buffer[length - 1 - 0] = l;

                    hash.fill(0);
                    hasher(hash.length).update(buffer).digest(hash);
                    const prefix = readUInt32LE(hash, 0);

                    if (prefix <= threshold) {
                        if (debug) {
                            console.debug(`[privatecaptcha][worker] found solution. prefix=${prefix} threshold=${threshold}`);
                        }
                        return buffer.subarray(length - 8);
                    }
                }
            }
        }
    }

    return new Uint8Array(0);
}
