import { encode } from 'base64-arraybuffer';
import PuzzleWorker from './puzzle.worker.js';

const METADATA_VERSION = 1;

export class WorkersPool {
    constructor(callbacks = {}, debug = false) {
        this._solutions = [];
        this._solutionsCount = 0;
        this._puzzleID = null;
        this._workers = [];
        this._debug = debug;
        this._timeStarted = null;
        this._timeFinished = null;
        this._anyWasm = false;

        this._callbacks = Object.assign({
            workersReady: () => 0,
            workerError: () => 0,
            workStarted: () => 0,
            workCompleted: () => 0,
            progress: () => 0,
        }, callbacks);
    }

    init(puzzle, autoStart) {
        if (!puzzle) { return; }
        if (puzzle.isZero()) {
            if (this._debug) { console.debug('[privatecaptcha][pool] skipping initializing workers'); }
            setTimeout(() => this._callbacks.workersReady(autoStart), 0);
            return;
        }

        const workersCount = 4;
        let readyWorkers = 0;
        const workers = [];
        const pool = this;
        const puzzleID = puzzle.ID;
        const puzzleData = puzzle.puzzleBuffer;

        for (let i = 0; i < workersCount; i++) {
            const worker = new PuzzleWorker();

            worker.onerror = (e) => this._callbacks.workerError(e);
            worker.onmessage = function(event) {
                if (!event.data) { return; }
                const command = event.data.command;
                switch (command) {
                    case "init":
                        readyWorkers++;
                        if (readyWorkers === workersCount) {
                            pool._callbacks.workersReady(autoStart);
                        }
                        break;
                    case "solve":
                        const { id, solution, wasm } = event.data.argument;
                        pool.onSolutionFound(id, solution, wasm);
                        break;
                    case "error":
                        if (event.data.error) {
                            pool._callbacks.workerError(event.data.error);
                        }
                        break;
                    default:
                        break;
                };
            };
            workers.push(worker);
        }

        this._workers = workers;

        if (this._debug) { console.debug(`[privatecaptcha][pool] initializing workers. count=${this._workers.length}`); }
        for (let i = 0; i < this._workers.length; i++) {
            this._workers[i].postMessage({
                command: "init",
                argument: {
                    id: puzzleID,
                    buffer: puzzleData,
                },
            });
        };
    }

    solve(puzzle) {
        if (!puzzle) { return; }

        if (this._debug) { console.debug('[privatecaptcha][pool] starting solving'); }
        this._solutions = [];
        this._solutionsCount = puzzle.solutionsCount;
        this._puzzleID = puzzle.ID;
        this._timeStarted = Date.now();
        this._timeFinished = null;

        const skipSolving = puzzle.isZero() || (puzzle.solutionsCount === 0);
        let stubSolution = null;

        for (let i = 0; i < puzzle.solutionsCount; i++) {
            if (!skipSolving) {
                this._workers[i % this._workers.length].postMessage({
                    command: "solve",
                    argument: {
                        difficulty: puzzle.difficulty,
                        puzzleIndex: i,
                        debug: this._debug,
                    },
                });
            } else {
                if (!stubSolution) { stubSolution = new Uint8Array(8); }
                this._solutions.push(stubSolution);
            }
        }

        this._callbacks.workStarted();

        if (skipSolving) {
            setTimeout(() => this.onWorkCompleted(), 0);
        }
    }

    stop() {
        const count = this._workers.length;
        for (let i = 0; i < count; i++) {
            this._workers[i].terminate();
        }
        this._workers = [];
        if (this._debug) { console.debug('[privatecaptcha][pool] terminated the workers. count=' + count); }
    }

    onSolutionFound(id, solution, wasm) {
        if (this._debug) { console.debug('[privatecaptcha][pool] solution found. length=' + solution.length); }
        if (id != this._puzzleID) {
            console.warn(`[privatecaptcha][pool] Discarding solution with invalid ID. actual=${id} expected=${this._puzzleID}`);
            return;
        }
        this._solutions.push(solution);

        if (wasm) { this._anyWasm = true; }

        const count = this._solutions.length;

        this._callbacks.progress(count * 100.0 / this._solutionsCount);

        if (count == this._solutionsCount) {
            this.onWorkCompleted();
        }
    }

    onWorkCompleted() {
        this._timeFinished = Date.now();
        this._callbacks.workCompleted();
    }

    serializeSolutions(errorCode) {
        if (this._debug) { console.debug('[privatecaptcha][pool] serializing solutions. count=' + this._solutions.length); }
        const solutionsLength = this._solutions.reduce((total, arr) => total + arr.length, 0);

        const metadataArray = this.writeMetadata(errorCode);
        const metadataSize = metadataArray.length;

        const resultArray = new Uint8Array(metadataSize + solutionsLength);
        let offset = 0;

        resultArray.set(metadataArray, offset);
        offset += metadataArray.length;

        for (let i = 0; i < this._solutions.length; i++) {
            resultArray.set(this._solutions[i], offset);
            offset += this._solutions[i].length;
        }

        return encode(resultArray);
    }

    writeMetadata(errorCode) {
        const metadataSize = 1 + 1 + 1 + 4;
        const binaryData = new Uint8Array(metadataSize);
        let currentIndex = 0;

        binaryData[currentIndex++] = METADATA_VERSION & 0xFF;
        binaryData[currentIndex++] = errorCode & 0xFF;

        const wasmFlag = this._anyWasm ? 1 : 0;
        binaryData[currentIndex++] = wasmFlag & 0xFF;

        const elapsedMillis = this.elapsedMillis();
        // Little-Endian
        binaryData[currentIndex++] = elapsedMillis & 0xFF;
        binaryData[currentIndex++] = (elapsedMillis >> 8) & 0xFF;
        binaryData[currentIndex++] = (elapsedMillis >> 16) & 0xFF;
        binaryData[currentIndex++] = (elapsedMillis >> 24) & 0xFF;

        return binaryData;
    }

    elapsedMillis() {
        if (this._timeStarted && this._timeFinished) {
            return this._timeFinished - this._timeStarted;
        }

        return 0;
    }
}
