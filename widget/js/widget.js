'use strict';

import { getPuzzle, Puzzle } from './puzzle.js'
import { encode } from 'base64-arraybuffer';
import { WorkersPool } from './workerspool.js'
import { CaptchaElement, STATE_EMPTY, STATE_ERROR, STATE_READY, STATE_IN_PROGRESS, STATE_VERIFIED, STATE_LOADING, DISPLAY_POPUP, DISPLAY_WIDGET } from './html.js';

window.customElements.define('private-captcha', CaptchaElement);

const PUZZLE_ENDPOINT_URL = 'https://api.privatecaptcha.com/puzzle';
const ERROR_NO_ERROR = 0;
const ERROR_FETCH_PUZZLE = 1;
const ERROR_SOLVE_PUZZLE = 2;

function findParentFormElement(element) {
    while (element && element.tagName !== 'FORM') {
        element = element.parentElement;
    }
    return element;
}

export class CaptchaWidget {
    constructor(element, options = {}) {
        this._element = element;
        this._puzzle = null;
        this._expiryTimeout = null;
        this._state = STATE_EMPTY;
        this._lastProgress = null;
        this._solution = null;
        this._userStarted = false; // aka 'user started while we were initializing'
        this._options = {};
        this._errorCode = ERROR_NO_ERROR;

        this.setOptions(options);

        this._workersPool = new WorkersPool({
            workersReady: this.onWorkersReady.bind(this),
            workerError: this.onWorkerError.bind(this),
            workStarted: this.onWorkStarted.bind(this),
            workCompleted: this.onWorkCompleted.bind(this),
            progress: this.onWorkProgress.bind(this),
        }, this._options.debug);

        const form = findParentFormElement(this._element);
        if (form) {
            // NOTE: this does not work on Safari by (Apple) design if we click a button
            // "once" means listener will be removed after being called, "passive" - cannot use preventDefault()
            form.addEventListener('focusin', this.onFocusIn.bind(this), { once: true, passive: true });
            this._element.innerHTML = `<private-captcha display-mode="${this._options.displayMode}" lang="${this._options.lang}" theme="${this._options.theme}" extra-styles="${this._options.styles}"${this._options.debug ? ' debug="true"' : ''}></private-captcha>`;
            this._element.addEventListener('check', this.onChecked.bind(this));

            if (this._options.storeVariable) {
                this._element[this._options.storeVariable] = this;
            }

            if (DISPLAY_POPUP === this._options.displayMode) {
                const anchor = form.querySelector(".private-captcha-anchor");
                if (anchor) {
                    anchor.style.position = "relative";
                } else {
                    console.warn('[privatecaptcha] cannot find anchor for popup')
                }
            }
        } else {
            console.warn('[privatecaptcha] cannot find form element');
        }
    }

    setOptions(options) {
        this._options = Object.assign({
            startMode: this._element.dataset["startMode"] || "click",
            debug: this._element.dataset["debug"],
            fieldName: this._element.dataset["solutionField"] || "private-captcha-solution",
            puzzleEndpoint: this._element.dataset["puzzleEndpoint"] || PUZZLE_ENDPOINT_URL,
            sitekey: this._element.dataset["sitekey"] || "",
            displayMode: this._element.dataset["displayMode"] || "widget",
            lang: this._element.dataset["lang"] || "en",
            theme: this._element.dataset["theme"] || "light",
            styles: this._element.dataset["styles"] || "",
            storeVariable: this._element.dataset["storeVariable"] || null,
        }, options);
    }

    // fetches puzzle from the server and setup workers
    async init(autoStart) {
        this.trace(`init() was called. state=${this._state}`);

        const sitekey = this._options.sitekey || this._element.dataset["sitekey"];
        if (!sitekey) {
            console.error("[privatecaptcha] sitekey not set on captcha element");
            return;
        }

        if ((this._state != STATE_EMPTY) && (this._state != STATE_ERROR)) {
            console.warn(`[privatecaptcha] captcha has already been initialized. state=${this._state}`)
            return;
        }

        if (this._workersPool) {
            this._workersPool.stop();
        }

        const startWorkers = (this._options.startMode == "auto") || autoStart;

        try {
            this._puzzle = null;
            this._solution = null;
            this._errorCode = ERROR_NO_ERROR;
            this.setState(STATE_LOADING);
            this.trace('fetching puzzle');
            const puzzleData = await getPuzzle(this._options.puzzleEndpoint, sitekey);
            this._puzzle = new Puzzle(puzzleData);
            const expirationMillis = this._puzzle.expirationMillis();
            this.trace(`parsed puzzle buffer. isZero=${this._puzzle.isZero()} ttl=${expirationMillis / 1000}`);
            if (this._expiryTimeout) { clearTimeout(this._expiryTimeout); }
            if (expirationMillis) { this._expiryTimeout = setTimeout(() => this.expire(), expirationMillis); }
            this._workersPool.init(this._puzzle, startWorkers);
        } catch (e) {
            console.error('[privatecaptcha]', e);
            if (this._expiryTimeout) { clearTimeout(this._expiryTimeout); }
            this._errorCode = ERROR_FETCH_PUZZLE;
            this.setState(STATE_ERROR);
            this.setProgressState(this._userStarted ? STATE_VERIFIED : STATE_EMPTY);
            this.saveSolutions();
            if (this._userStarted) {
                this.signalErrored();
            }
        }
    }

    start() {
        if (this._state !== STATE_READY) {
            console.warn(`[privatecaptcha] solving has already been started. state=${this._state}`);
            return;
        }

        this.trace('starting solving captcha');

        try {
            this.setState(STATE_IN_PROGRESS);
            this._workersPool.solve(this._puzzle);
        } catch (e) {
            console.error('[privatecaptcha]', e);
        }
    }

    signalStarted() {
        const callback = this._element.dataset['startedCallback'];
        if (callback) {
            window[callback](this);
        }
    }

    signalFinished() {
        const callback = this._element.dataset['finishedCallback'];
        if (callback) {
            window[callback](this);
        }
    }

    signalErrored() {
        const callback = this._element.dataset['erroredCallback'];
        if (callback) {
            window[callback](this);
        }
    }

    ensureNoSolutionField() {
        const solutionField = this._element.querySelector(`input[name="${this._options.fieldName}"]`);
        if (solutionField) {
            try {
                this._element.removeChild(solutionField);
            } catch (e) {
                console.warn('[privatecaptcha]', e);
            }
        }
    }

    reset(options = {}) {
        this.trace('reset captcha')

        if (this._workersPool) { this._workersPool.stop(); }
        if (this._expiryTimeout) { clearTimeout(this._expiryTimeout); }

        this.setState(STATE_EMPTY);
        this.setProgressState(STATE_EMPTY);
        this.ensureNoSolutionField();
        this._userStarted = false;
        this.setOptions(options);
        this.init(false /*start*/);
    }

    expire() {
        this.trace('time to expire captcha');
        this.setState(STATE_EMPTY);
        this.setProgressState(STATE_EMPTY);
        this.ensureNoSolutionField();
        this.init(this._userStarted);
    }

    solution() {
        return this._solution;
    }

    onFocusIn(event) {
        this.trace('onFocusIn event handler');
        const pcElement = this._element.querySelector('private-captcha');
        if (pcElement && (event.target == pcElement)) {
            this.trace('skipping focusin event on captcha element')
            return;
        }
        this.init(false /*start*/);
        // this handles both STATE_LOADING and STATE_EMPTY (reset)
        this.setProgressState(this._state);
    }

    execute() {
        this.onChecked();
        // this promise intentionally does not resolve so that the form can be submitted via the callbacks
        return new Promise(() => { });
    }

    onChecked() {
        this.trace(`onChecked event handler. state=${this._state}`);
        this._userStarted = true;

        // always show spinner when user clicked
        let progressState = STATE_IN_PROGRESS;
        let signal = false;

        switch (this._state) {
            case STATE_READY:
                // NOTE: in case of short-circuit (zero/test puzzle), start() can call all callbacks before exit
                this.start();
                break;
            case STATE_EMPTY:
            case STATE_ERROR:
                this.init(true /*start*/);
                break;
            case STATE_LOADING:
                // this will be handled in onWorkersReady()
                break;
            case STATE_IN_PROGRESS:
                setTimeout(() => this.setProgress(this._lastProgress), 500);
                break;
            case STATE_VERIFIED:
                // happens when we finished verification fully in the background, still should animate "the end"
                progressState = STATE_VERIFIED;
                signal = true;
                break;
            default:
                console.warn('[privatecaptcha] onChecked: unexpected state. state=' + this._state);
        };

        this.setProgressState(progressState);
        if (signal) { this.signalFinished(); }
    }

    onWorkersReady(autoStart) {
        this.trace(`workers are ready. autostart=${autoStart}`);

        this.setState(STATE_READY);
        if (!this._userStarted) {
            this.setProgressState(STATE_READY);
        }

        if (autoStart || this._userStarted) {
            this.start();
        }
    }

    onWorkerError(error) {
        console.error('[privatecaptcha] error in worker:', error)
        this._errorCode = ERROR_SOLVE_PUZZLE;
    }

    onWorkStarted() {
        this.signalStarted();
    }

    onWorkCompleted() {
        this.trace('[privatecaptcha] work completed');

        if (this._state !== STATE_IN_PROGRESS) {
            console.warn(`[privatecaptcha] solving has not been started. state=${this._state}`);
            return;
        }

        this.setState(STATE_VERIFIED);
        if (this._userStarted) {
            this.setProgressState(STATE_VERIFIED);
        }

        this.saveSolutions();

        if (this._userStarted) {
            this.signalFinished();
        }
    }

    onWorkProgress(percent) {
        if (this._state !== STATE_IN_PROGRESS) {
            console.warn(`[privatecaptcha] skipping progress update. state=${this._state}`);
            return;
        }

        this.trace(`progress changed. percent=${percent}`);
        this.setProgress(percent);
    }

    saveSolutions() {
        const solutions = this._workersPool.serializeSolutions(this._errorCode);
        const payload = `${solutions}.${this._puzzle.rawData}`;

        this.ensureNoSolutionField();
        this._element.insertAdjacentHTML('beforeend', `<input name="${this._options.fieldName}" type="hidden" value="${payload}">`);

        this._solution = payload;

        this.trace(`saved solutions. payload=${payload}`);
    }

    // this updates the "UI" state of the widget
    setProgressState(state) {
        // NOTE: hidden display mode is taken care of inside setState() even when (_userStarted == true)
        const canShow = this._userStarted || (DISPLAY_WIDGET === this._options.displayMode);
        const pcElement = this._element.querySelector('private-captcha');
        if (pcElement) { pcElement.setState(state, canShow); }
        else { console.error('[privatecaptcha] component not found when changing state'); }
    }

    // this updates the "internal" (actual) state
    setState(state) {
        this.trace(`change state. old=${this._state} new=${state}`);
        this._state = state;
        if (this._options.debug ||
            !this._puzzle ||
            this._puzzle.isZero()) {
            const pcElement = this._element.querySelector('private-captcha');
            if (pcElement) {
                if (STATE_ERROR == state) {
                    pcElement.setError('error');
                } else if (this._puzzle && this._puzzle.isZero()) {
                    pcElement.setError('testing');
                } else if (!this._puzzle) {
                    pcElement.setError(null);
                }

                pcElement.setDebugState(state);
            }
        }
    }

    setProgress(progress) {
        this._lastProgress = progress;
        if ((STATE_IN_PROGRESS == this._state) || (STATE_VERIFIED == this._state)) {
            const pcElement = this._element.querySelector('private-captcha');
            if (pcElement) { pcElement.setProgress(progress); }
            else { console.error('[privatecaptcha] component not found when updating progress'); }
        }
    }

    trace(str) {
        if (this._options.debug) {
            console.debug('[privatecaptcha]', str)
        }
    }
}
