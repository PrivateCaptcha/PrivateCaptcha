import { encode } from 'base64-arraybuffer';
import PuzzleWorker from './puzzle.worker.js';
import { CHALLENGE_BLAKE2B, CHALLENGE_ARGON2ID } from './puzzle.js';

const METADATA_VERSION = 1;

export class WorkersPool {
    constructor(callbacks = {}, debug = false, WorkerClass = PuzzleWorker) {
        this._solutions = [];
        this._solutionsCount = 0;
        this._puzzleID = null;
        this._workers = [];
        this._debug = debug;
        this._timeStarted = null;
        this._timeFinished = null;
        this._anyWasm = false;
        this._workFailed = false;
        this._puzzle = null;
        this._WorkerClass = WorkerClass;

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
        if (puzzle.challenge !== CHALLENGE_BLAKE2B && puzzle.challenge !== CHALLENGE_ARGON2ID) {
            throw new Error(`Unknown puzzle challenge: ${puzzle.challenge}`);
        }
        if (puzzle.isZero() && puzzle.challenge === CHALLENGE_BLAKE2B) {
            if (this._debug) { console.debug('[privatecaptcha][pool] skipping initializing workers'); }
            setTimeout(() => this._callbacks.workersReady(autoStart), 0);
            return;
        }

        const workers = [];
        try {
            this.initWorkers(puzzle, autoStart, workers);
        } catch (error) {
            for (const worker of workers) {
                worker.terminate();
            }
            this._workers = [];
            throw error;
        }
    }

    initWorkers(puzzle, autoStart, workers) {
        const workersCount = puzzle.challenge === CHALLENGE_ARGON2ID ? 1 : 4;
        let readyWorkers = 0;
        const puzzleData = puzzle.challenge === CHALLENGE_ARGON2ID ? puzzle.puzzleBytes : puzzle.puzzleBuffer;

        for (let i = 0; i < workersCount; i++) {
            const worker = new this._WorkerClass();
            worker.onerror = (e) => {
                if (this._workers.includes(worker)) { this.onWorkerError(e); }
            };
            worker.onmessage = (event) => {
                if (!this._workers.includes(worker) || !event.data) { return; }
                switch (event.data.command) {
                    case 'init':
                        readyWorkers++;
                        if (readyWorkers === workersCount) { this._callbacks.workersReady(autoStart); }
                        break;
                    case 'solve':
                        const { id, solution, wasm } = event.data.argument || {};
                        this.onSolutionFound(id, solution, wasm);
                        break;
                    case 'error':
                        if (event.data.error) { this.onWorkerError(event.data.error); }
                        break;
                    default:
                        break;
                }
            };
            workers.push(worker);
        }

        this._workers = workers;
        if (this._debug) { console.debug(`[privatecaptcha][pool] initializing workers. count=${workers.length}`); }
        for (const worker of workers) {
            worker.postMessage({
                command: 'init',
                argument: { id: puzzle.ID, challenge: puzzle.challenge, buffer: puzzleData },
            });
        }
    }

    solve(puzzle) {
        if (!puzzle) { return; }

        if (this._debug) { console.debug('[privatecaptcha][pool] starting solving'); }
        this._solutions = [];
        this._solutionsCount = puzzle.solutionsCount;
        this._puzzleID = puzzle.ID;
        this._puzzle = puzzle;
        this._timeStarted = Date.now();
        this._timeFinished = null;
        this._workFailed = false;

        const skipSolving = (puzzle.isZero() && puzzle.challenge === CHALLENGE_BLAKE2B) || (puzzle.solutionsCount === 0);
        let stubSolution = null;

        for (let i = 0; i < puzzle.solutionsCount; i++) {
            if (!skipSolving) {
                if (puzzle.challenge !== CHALLENGE_ARGON2ID || i === 0) {
                    this._workers[i % this._workers.length].postMessage({
                        command: "solve",
                        argument: {
                            difficulty: puzzle.difficulty,
                            puzzleIndex: i,
                            debug: this._debug,
                        },
                    });
                }
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

    reset() {
        this._solutions = [];
        this._solutionsCount = 0;
        this._puzzleID = null;
        this._timeStarted = null;
        this._timeFinished = null;
        this._anyWasm = false;
        this._workFailed = false;
        this._puzzle = null;
    }

    onSolutionFound(id, solution, wasm) {
        if (this._workFailed || this._timeFinished !== null || this._puzzle === null) { return; }
        if (id !== this._puzzleID) {
            console.warn(`[privatecaptcha][pool] Discarding solution with invalid ID. actual=${id} expected=${this._puzzleID}`);
            return;
        }
        if (!(solution instanceof Uint8Array) || solution.length !== 8 || typeof wasm !== 'boolean' ||
            solution[0] >= this._solutionsCount || this._solutions.some((found) => found[0] === solution[0]) ||
            (this._puzzle.challenge === CHALLENGE_ARGON2ID && solution[0] !== this._solutions.length)) {
            this.onWorkerError(new Error('Invalid puzzle worker solve response'));
            return;
        }
        if (this._debug) { console.debug('[privatecaptcha][pool] solution found. length=' + solution.length); }
        this._solutions.push(solution);

        if (wasm) { this._anyWasm = true; }

        const count = this._solutions.length;

        this._callbacks.progress(count * 100.0 / this._solutionsCount);

        if (count == this._solutionsCount) {
            this.onWorkCompleted();
        } else if (this._puzzle.challenge === CHALLENGE_ARGON2ID) {
            try {
                this._workers[0].postMessage({
                    command: 'solve',
                    argument: { difficulty: this._puzzle.difficulty, puzzleIndex: count, debug: this._debug },
                });
            } catch (error) {
                this.onWorkerError(error);
            }
        }
    }

    onWorkCompleted() {
        if (this._workFailed || this._timeFinished !== null) { return; }
        this._timeFinished = Date.now();
        this._callbacks.workCompleted();
    }

    onWorkerError(error) {
        if (this._workFailed || this._timeFinished !== null) { return; }
        this._workFailed = true;
        this.stop();
        this._callbacks.workerError(error);
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
