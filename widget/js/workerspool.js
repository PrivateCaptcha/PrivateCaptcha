import { encode } from 'base64-arraybuffer';
import PuzzleWorker from './puzzle.worker.js';

export class WorkersPool {
    constructor(callbacks = {}, debug = false) {
        this._solutions = [];
        this._solutionsCount = 0;
        this._puzzleID = null;
        this._workers = [];
        this._debug = debug;
        this._timeStarted = null;
        this._timeFinished = null;

        this._callbacks = Object.assign({
            workersReady: () => 0,
            workerError: () => 0,
            workCompleted: () => 0,
            progress: () => 0,
        }, callbacks);
    }

    init(puzzleID, puzzleData, autoStart) {
        const workersCount = 4;
        let readyWorkers = 0;
        const workers = [];
        const pool = this;

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
                        const { id, solution } = event.data.argument;
                        pool.onSolutionFound(id, solution);
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
        this._puzzleID = puzzleID;

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

        for (let i = 0; i < puzzle.solutionsCount; i++) {
            this._workers[i % this._workers.length].postMessage({
                command: "solve",
                argument: {
                    difficulty: puzzle.difficulty,
                    puzzleIndex: i,
                    debug: this._debug,
                },
            });
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

    onSolutionFound(id, solution) {
        if (this._debug) { console.debug('[privatecaptcha][pool] solution found. length=' + solution.length); }
        if (id != this._puzzleID) {
            console.warn(`[privatecaptcha][pool] Discarding solution with invalid ID. actual=${id} expected=${this._puzzleID}`);
            return;
        }
        this._solutions.push(solution);

        const count = this._solutions.length;

        this._callbacks.progress(count * 100.0 / this._solutionsCount);

        if (count == this._solutionsCount) {
            this._timeFinished = Date.now();
            this._callbacks.workCompleted();
        }
    }

    serializeSolutions() {
        if (this._debug) { console.debug('[privatecaptcha][pool] solutions found. count=' + this._solutions.length); }
        const totalLength = this._solutions.reduce((total, arr) => total + arr.length, 0);
        const resultArray = new Uint8Array(totalLength);
        let offset = 0;
        for (let i = 0; i < this._solutions.length; i++) {
            resultArray.set(this._solutions[i], offset);
            offset += this._solutions[i].length;
        }

        return encode(resultArray);
    }

    elapsedMillis() {
        if (this._timeStarted && this._timeFinished) {
            return this._timeFinished - this._timeStarted;
        }

        return 0;
    }
}
