'use strict';

import { getPuzzle, Puzzle } from './puzzle.js'
import { WorkersPool } from './workerspool.js'
import { CaptchaElement, STATE_EMPTY, STATE_ERROR, STATE_READY, STATE_IN_PROGRESS, STATE_VERIFIED, STATE_LOADING, STATE_INVALID, DISPLAY_POPUP, DISPLAY_WIDGET } from './html.js';
import * as errors from './errors.js';

if (typeof window !== "undefined" && window.customElements && !window.customElements.get('private-captcha')) {
    window.customElements.define('private-captcha', CaptchaElement);
}

const PUZZLE_ENDPOINT_URL = 'https://api.privatecaptcha.com/puzzle';
const PUZZLE_EU_ENDPOINT_URL = 'https://api.eu.privatecaptcha.com/puzzle';
export const RECAPTCHA_COMPAT = 'recaptcha';


/**
 * @param {HTMLElement} element
 * @returns {HTMLFormElement | null}
 */
function findParentFormElement(element) {
    while (element && element.tagName !== 'FORM') {
        element = element.parentElement;
    }
    return element;
}

export class CaptchaWidget {
    /**
     * @param {HTMLElement} element
     * @param {Object} options
     */
    constructor(element, options = {}) {
        this._element = element;
        this._puzzle = null;
        this._expiryTimeout = null;
        this._state = STATE_EMPTY;
        this._lastProgress = null;
        this._solution = null;
        this._userStarted = false; // aka 'user started while we were initializing'
        this._apiTriggered = false; // aka execute() for programmatic triggering
        this._options = {};
        this._errorCode = errors.ERROR_NO_ERROR;

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
            // "passive" - cannot use preventDefault()
            form.addEventListener('focusin', this.onFocusIn.bind(this), { passive: true });
            this._element.innerHTML = `<private-captcha display-mode="${this._options.displayMode}" lang="${this._options.lang}" theme="${this._options.theme}" extra-styles="${this._options.styles}"${this._options.debug ? ' debug="true"' : ''}></private-captcha>`;
            this._element.addEventListener('privatecaptcha:checked', this.onChecked.bind(this));

            if (this._options.storeVariable) {
                this._element[this._options.storeVariable] = this;
            }

            if (DISPLAY_POPUP === this._options.displayMode) {
                const anchor = form.querySelector(".private-captcha-anchor");
                if (anchor) {
                    anchor.style.position = "relative";
                } else {
                    console.warn('[privatecaptcha] cannot find anchor for popup');
                }
            }

            this.checkConfigured();
        } else {
            console.warn('[privatecaptcha] cannot find form element');
        }
    }

    /**
     * @param {Object} options
     */
    setOptions(options) {
        const euOnly = this._element.dataset["eu"] || null;
        const defaultEndpoint = euOnly ? PUZZLE_EU_ENDPOINT_URL : PUZZLE_ENDPOINT_URL;

        let defaultField = "private-captcha-solution";
        if (options.hasOwnProperty('compat') && options.compat === RECAPTCHA_COMPAT) {
            defaultField = "g-recaptcha-response";
        }

        let sitekey = "";
        if (options.hasOwnProperty('sitekey')) {
            sitekey = options.sitekey;
        }

        this._options = Object.assign({
            startMode: this._element.dataset["startMode"] || "auto",
            debug: this._element.dataset["debug"],
            fieldName: this._element.dataset["solutionField"] || defaultField,
            puzzleEndpoint: this._element.dataset["puzzleEndpoint"] || defaultEndpoint,
            sitekey: sitekey || this._element.dataset["sitekey"] || "",
            displayMode: this._element.dataset["displayMode"] || "widget",
            lang: this._element.dataset["lang"] || "auto",
            theme: this._element.dataset["theme"] || "light",
            styles: this._element.dataset["styles"] || "",
            storeVariable: this._element.dataset["storeVariable"] || null,
        }, options);

        if ('auto' === this._options.lang) {
            let lang = '';
            if (typeof document !== 'undefined' && document.documentElement) {
                lang = document.documentElement.lang;
            }
            if (!lang && typeof navigator !== 'undefined') {
                lang = navigator.language || navigator.userLanguage || '';
            }
            if (typeof lang === 'string' && lang.length >= 2) {
                this._options.lang = lang.split('-')[0].toLowerCase();
            }
        }
    }

    /**
     * Fetches puzzle from the server and sets up workers.
     * @param {boolean} autoStart
     */
    async init(autoStart) {
        this.trace(`init() was called. state=${this._state}`);

        this._puzzle = null;
        this._solution = null;
        this._errorCode = errors.ERROR_NO_ERROR;

        const sitekey = this.checkConfigured();
        if (!sitekey) { return; }

        if ((STATE_EMPTY !== this._state) && (STATE_ERROR !== this._state)) {
            console.warn(`[privatecaptcha] captcha has already been initialized. state=${this._state}`);
            return;
        }

        if (this._workersPool) {
            this._workersPool.stop();
            this._workersPool.reset();
        }

        const startWorkers = ('auto' === this._options.startMode) || autoStart;

        try {
            this.setState(STATE_LOADING);
            this.setProgressState(STATE_LOADING);
            this.trace(`fetching puzzle. sitekey=${sitekey}`);
            const puzzleData = await getPuzzle(this._options.puzzleEndpoint, sitekey);
            this._puzzle = new Puzzle(puzzleData);
            if (this._puzzle && this._puzzle.isZero()) { this._errorCode = errors.ERROR_ZERO_PUZZLE; }
            const expirationMillis = this._puzzle.expirationMillis();
            this.trace(`parsed puzzle buffer. isZero=${this._puzzle.isZero()} ttl=${expirationMillis / 1000}`);
            if (this._expiryTimeout) { clearTimeout(this._expiryTimeout); }
            if (expirationMillis) { this._expiryTimeout = setTimeout(() => this.expire(), expirationMillis); }
            this._workersPool.init(this._puzzle, startWorkers);
            this.signalInit();
        } catch (e) {
            console.error('[privatecaptcha]', e);
            if (this._expiryTimeout) { clearTimeout(this._expiryTimeout); }
            this._errorCode = errors.ERROR_FETCH_PUZZLE;
            this.setState(STATE_ERROR);
            this.setProgressState((this._userStarted || this._apiTriggered) ? STATE_VERIFIED : STATE_EMPTY);
            if (this._userStarted || this._apiTriggered) {
                this.saveSolutions();
                this.signalErrored();
            }
        }
    }

    /**
     * Ensures that we have a sitekey available (defined or passed through options)
     * @returns {string | null}
     */
    checkConfigured() {
        const sitekey = this._options.sitekey || this._element.dataset["sitekey"];
        if (!sitekey) {
            console.error("[privatecaptcha] sitekey not set on captcha element");
            this._errorCode = errors.ERROR_NOT_CONFIGURED;
            this.setState(STATE_INVALID);
            this.setProgressState(STATE_INVALID);
            return null;
        }

        return sitekey;
    }

    start() {
        if (STATE_READY !== this._state) {
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

    dispatchEvent(eventName, detail = {}) {
        const event = new CustomEvent(`privatecaptcha:${eventName}`, {
            bubbles: false,
            detail: { widget: this, element: this._element, ...detail }
        });
        this._element.dispatchEvent(event);
    }

    signalInit() {
        this.dispatchEvent("init");

        const callback = this._element.dataset['initCallback'];
        if (callback) {
            try {
                window[callback](this);
            } catch (e) {
                console.error('[privatecaptcha] Error in init callback:', e);
            }
        }
    }

    signalStarted() {
        this.dispatchEvent("start");

        const callback = this._element.dataset['startedCallback'];
        if (callback) {
            try {
                window[callback](this);
            } catch (e) {
                console.error('[privatecaptcha] Error in started callback:', e);
            }
        }
    }

    signalFinished() {
        this.dispatchEvent("finish");

        const callback = this._element.dataset['finishedCallback'];
        if (callback) {
            try {
                window[callback](this);
            } catch (e) {
                console.error('[privatecaptcha] Error in finished callback:', e);
            }
        }
    }

    signalErrored() {
        this.dispatchEvent("error");

        const callback = this._element.dataset['erroredCallback'];
        if (callback) {
            try {
                window[callback](this);
            } catch (e) {
                console.error('[privatecaptcha] Error in errored callback:', e);
            }
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

    /**
     * Resets widget to a state when `start()` or `execute()` can be called again
     * @param {Object} options
     */
    reset(options = {}) {
        this.trace('reset captcha');

        if (this._expiryTimeout) { clearTimeout(this._expiryTimeout); }
        if (this._workersPool) {
            this._workersPool.stop();
            this._workersPool.reset();
        }

        this._puzzle = null;
        this._solution = null;
        this._errorCode = errors.ERROR_NO_ERROR;
        this.setState(STATE_EMPTY);
        this.setProgressState(STATE_EMPTY);
        this.ensureNoSolutionField();
        this._userStarted = false;
        this._apiTriggered = false;
        this.setOptions(options);
    }

    updateStyles() {
        const newStyles = this._element.dataset["styles"] || "";
        if (newStyles !== this._options.styles) {
            this._options.styles = newStyles;
            const pcElement = this._element.querySelector('private-captcha');
            if (pcElement) {
                pcElement.setAttribute('extra-styles', newStyles);
            }
        }
    }

    expire() {
        this.trace('expire captcha');

        // we immediately call init so reset() will be called there for workers pool
        if (this._workersPool) { this._workersPool.stop(); }

        this.setState(STATE_EMPTY);
        this.setProgressState(STATE_EMPTY);
        this.ensureNoSolutionField();
        this.init(this._userStarted);
    }

    /**
     * @returns {string} value of the puzzle solution that needs to be sent for verification
     */
    solution() {
        return this._solution;
    }

    /**
     * @param {FocusEvent} event
     */
    onFocusIn(event) {
        this.trace('onFocusIn event handler');

        if (STATE_EMPTY !== this._state) {
            this.trace(`skipping focusin event with non-empty state. state=${this._state}`)
            return;
        }

        const pcElement = this._element.querySelector('private-captcha');
        if (pcElement && (event.target == pcElement)) {
            this.trace('skipping focusin event on captcha element')
            return;
        }

        this.init(false /*start*/);
    }

    /**
     * A programmatic way of starting solving the puzzle (as opposed to user input way)
     * @returns {Promise<never>} promise intentionally does not resolve so that the form can be submitted via the callbacks
     */
    execute() {
        if (this._apiTriggered && (STATE_ERROR !== this._state)) {
            this.trace(`skipping duplicate execute event handler`);
            return new Promise(() => { });
        }

        this.trace(`execute event handler. state=${this._state}`);
        this._apiTriggered = true;

        // show spinner when in auto mode
        let progressState = ('auto' === this._options.startMode) ? STATE_IN_PROGRESS : this._state;
        this._triggerSolving(progressState);

        return new Promise(() => { });
    }

    onChecked(event) {
        if (event) {
            event.stopPropagation();
        }

        if (this._userStarted && (STATE_ERROR !== this._state)) {
            this.trace('skipping duplicate onChecked handler')
            return;
        }

        this.trace(`onChecked event handler. state=${this._state}`);
        this._userStarted = true;

        // always show spinner when user clicked
        this._triggerSolving(STATE_IN_PROGRESS);
    }

    _triggerSolving(progressState) {
        let finished = false;

        switch (this._state) {
            case STATE_READY:
                // NOTE: in case of short-circuit (zero/test puzzle), start() can call all callbacks before exit
                this.start();
                break;
            case STATE_EMPTY:
            case STATE_ERROR:
                const autoStart = this._userStarted || this._apiTriggered;
                this.init(autoStart);
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
                finished = true;
                break;
            default:
                console.warn(`[privatecaptcha] triggerSolving: unexpected state. state=${this._state}`);
                return;
        }

        this.setProgressState(progressState);
        if (finished) {
            this.saveSolutions();
            this.signalFinished();
        }
    }

    /**
     * @param {boolean} autoStart
     */
    onWorkersReady(autoStart) {
        this.trace(`workers are ready. autostart=${autoStart}`);

        this.setState(STATE_READY);
        // if user started we always show "in progress"
        if (!this._userStarted && !(this._apiTriggered && autoStart)) {
            this.setProgressState(STATE_READY);
        }

        if (autoStart || this._userStarted) {
            this.start();
        }
    }

    /**
     * @param {Error} error
     */
    onWorkerError(error) {
        console.error('[privatecaptcha] error in worker:', error)
        this._errorCode = errors.ERROR_SOLVE_PUZZLE;
    }

    onWorkStarted() {
        this.signalStarted();
    }

    onWorkCompleted() {
        this.trace('[privatecaptcha] work completed');

        if (STATE_IN_PROGRESS !== this._state) {
            console.warn(`[privatecaptcha] solving has not been started. state=${this._state}`);
            return;
        }

        this.setState(STATE_VERIFIED);
        if (this._userStarted || this._apiTriggered) {
            this.setProgressState(STATE_VERIFIED);
        }

        if (this._userStarted || this._apiTriggered) {
            this.saveSolutions();

            // give time for checkbox animation to complete
            setTimeout(() => this.signalFinished(), 500);
        }
    }

    /**
     * @param {number} percent
     */
    onWorkProgress(percent) {
        if (STATE_IN_PROGRESS !== this._state) {
            console.warn(`[privatecaptcha] skipping progress update. state=${this._state}`);
            return;
        }

        this.trace(`progress changed. percent=${percent}`);
        this.setProgress(percent);
    }

    saveSolutions() {
        const solutions = this._workersPool.serializeSolutions(this._errorCode);
        const payload = `${solutions}.${this._puzzle ? this._puzzle.rawData : ''}`;

        this.ensureNoSolutionField();
        this._element.insertAdjacentHTML('beforeend', `<input name="${this._options.fieldName}" type="hidden" value="${payload}">`);

        this._solution = payload;

        this.trace(`saved solutions. field=${this._options.fieldName} payload=${payload}`);
    }

    /**
     * Updates the "UI" state of the widget.
     * @param {string} state
     */
    setProgressState(state) {
        // NOTE: hidden display mode is taken care of inside setState() even when (_userStarted == true)
        const canShow = this._userStarted ||
            (DISPLAY_WIDGET === this._options.displayMode) ||
            (this._apiTriggered && (DISPLAY_POPUP === this._options.displayMode));
        const pcElement = this._element.querySelector('private-captcha');
        if (pcElement) {
            pcElement.setError(this._errorCode);
            pcElement.setState(state, canShow);
        }
        else {
            console.error('[privatecaptcha] component not found when changing state');
        }
    }

    /**
     * Updates the "internal" (actual) state.
     * @param {string} state
     */
    setState(state) {
        this.trace(`change state. old=${this._state} new=${state}`);
        this._state = state;

        if (this._options.debug) {
            const pcElement = this._element.querySelector('private-captcha');
            if (pcElement) {
                pcElement.setDebugText(state, (STATE_ERROR == state));
            }
        }
    }

    /**
     * @param {number} progress
     */
    setProgress(progress) {
        this._lastProgress = progress;
        if ((STATE_IN_PROGRESS === this._state) || (STATE_VERIFIED === this._state)) {
            const pcElement = this._element.querySelector('private-captcha');
            if (pcElement) { pcElement.setProgress(progress); }
            else { console.error('[privatecaptcha] component not found when updating progress'); }
        }
    }

    /**
     * @param {string} str
     */
    trace(str) {
        if (this._options.debug) {
            console.debug('[privatecaptcha]', str)
        }
    }
}
