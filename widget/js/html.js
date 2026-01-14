'use strict';

import { ProgressRing } from './progress.js';
import { SafeHTMLElement } from "./utils.js";
import styles from "./styles.css" with { type: 'css' };
import * as i18n from './strings.js';
import * as errors from './errors.js';

if (typeof window !== "undefined" && window.customElements && !window.customElements.get('progress-ring')) {
    window.customElements.define('progress-ring', ProgressRing);
}

export const STATE_EMPTY = 'empty';
export const STATE_ERROR = 'error';
export const STATE_LOADING = 'loading';
export const STATE_READY = 'ready';
export const STATE_IN_PROGRESS = 'inprogress';
export const STATE_VERIFIED = 'verified';
export const STATE_INVALID = 'invalid';

export const DISPLAY_POPUP = 'popup';
const DISPLAY_HIDDEN = 'hidden';
export const DISPLAY_WIDGET = 'widget';

const CHECKBOX_ID = 'pc-checkbox';
const PROGRESS_ID = 'pc-progress';
const DEBUG_ID = 'pc-debug';
const DEBUG_ERROR_CLASS = 'warn';

const privateCaptchaSVG = `<svg viewBox="0 0 39.4 41.99" xml:space="preserve" xmlns="http://www.w3.org/2000/svg" class="pc-logo" preserveAspectRatio="xMidYMid meet">
<path d="M0 0v30.62l4.29 2.48V4.85h30.83v23.29l-15.41 8.9-6.83-3.94v-4.95l6.83 3.94 11.12-6.42V9.91H8.58v25.66l11.12 6.42 19.7-11.37V0Zm12.87 14.86h13.66v8.32l-6.83 3.94-6.83-3.94z" fill="currentColor"/>
</svg>`;

const verifiedSVG = `<svg class="verified" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 154 154">
<g fill="none"><circle cx="77" cy="77" r="76"></circle>
<polyline class="st0" stroke-width="12" points="43.5,77.8 63.7,97.9 112.2,49.4" style="stroke-dasharray:100px, 100px; stroke-dashoffset: 200px;"/></g>
</svg>
`;

/**
 * @param {string} cls
 * @returns {string} checkbox input definition string
 */
function checkbox(cls) {
    return `<input type="checkbox" id="${CHECKBOX_ID}" class="${cls}" required>`
}

/**
 * @param {string} text
 * @param {string} forElement
 * @returns {string} checkbox label definition string
 */
function label(text, forElement) {
    return `<label for="${forElement}">${text}</label>`;
}

/**
 * @param {number} code
 * @param {Object<string, string>} strings
 * @returns {string} error message
 */
function errorDescription(code, strings) {
    switch (code) {
        case errors.ERROR_NO_ERROR:
            return '';
        case errors.ERROR_NOT_CONFIGURED:
            return strings[i18n.INCOMPLETE];
        case errors.ERROR_ZERO_PUZZLE:
            return strings[i18n.TESTING];
        default:
            return strings[i18n.ERROR];
    }
}

export class CaptchaElement extends SafeHTMLElement {
    constructor() {
        super();

        this._state = '';
        this._root = this.attachShadow({ mode: 'open' });

        this._debug = false;
        this._error = null;
        this._displayMode = DISPLAY_HIDDEN;
        this._lang = 'en';

        // Add CSS
        const sheet = new CSSStyleSheet();
        sheet.replaceSync(styles);
        this._root.adoptedStyleSheets = [sheet];
        this._overridesSheet = null;
    }

    connectedCallback() {
        this._debug = this.getAttribute('debug');
        this._error = null;
        this._displayMode = this.getAttribute('display-mode');
        this._lang = this.getAttribute('lang');
        if (!(this._lang in i18n.STRINGS)) {
            console.warn(`[privatecaptcha][progress] Localization not found. lang=${this._lang}`);
            this._lang = 'en';
        }

        // add CSS overrides
        const extraStyles = this.getAttribute('extra-styles');
        this.updateStyles(extraStyles);

        // init
        const canShow = (this._displayMode == DISPLAY_WIDGET);
        this.setState(STATE_EMPTY, canShow);
    }

    /**
     * @param {string} state
     * @param {boolean} canShow
     */
    setState(state, canShow) {
        if (state == this._state) {
            console.debug('[privatecaptcha][progress] already in this state: ' + state);
            if (DISPLAY_POPUP === this._displayMode) {
                this._syncHostClass(canShow);
            }
            return;
        }

        if (this._debug) { console.debug(`[privatecaptcha][progress] change state. old=${this._state} new=${state}`); }

        let activeArea = '';
        let bindCheckEvent = false;
        let showPopupIfNeeded = false;
        const strings = i18n.STRINGS[this._lang];

        switch (state) {
            case STATE_EMPTY:
                bindCheckEvent = true;
                activeArea = checkbox('') + label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID);
                break;
            case STATE_LOADING:
                bindCheckEvent = true;
                activeArea = checkbox('loading') + label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID);
                break;
            case STATE_READY:
                bindCheckEvent = true;
                activeArea = checkbox('ready') + label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID);
                showPopupIfNeeded = canShow;
                break;
            case STATE_IN_PROGRESS:
                const text = strings[i18n.VERIFYING];
                activeArea = `<progress-ring id="${PROGRESS_ID}" stroke="12" progress="0"></progress-ring><label for="${PROGRESS_ID}">${text}<span class="dots"><span>.</span><span>.</span><span>.</span></span></label>`;
                showPopupIfNeeded = canShow;
                break;
            case STATE_VERIFIED:
                activeArea = verifiedSVG + label(strings[i18n.SUCCESS], PROGRESS_ID);
                showPopupIfNeeded = canShow;
                break;
            case STATE_INVALID:
                activeArea = checkbox('invalid') + label(strings[i18n.UNAVAILABLE], CHECKBOX_ID);
                break;
            default:
                console.error(`[privatecaptcha][progress] unknown state: ${state}`);
                break;
        }

        if (this._debug || this._error) {
            const debugText = this._error ? errorDescription(this._error, strings) : `[${state}]`;
            activeArea += `<span id="${DEBUG_ID}" class="${this._error ? DEBUG_ERROR_CLASS : ''}">${debugText}</span>`;
        }

        this._syncHostClass(showPopupIfNeeded);

        this._state = state;
        this._root.innerHTML = `<div class="pc-captcha-widget">
            <div class="pc-interactive-area">
                ${activeArea}
            </div>
            <div class="pc-spacer"></div>
            <div class="pc-info">
                ${privateCaptchaSVG}
                <a href="https://privatecaptcha.com" class="pc-link" rel="noopener nofollow" target="_blank">Private<br />Captcha</a>
            </div>
        </div>`;

        if (bindCheckEvent) {
            const checkbox = this._root.getElementById(CHECKBOX_ID);
            if (checkbox) {
                checkbox.addEventListener('change', this.onCheckboxClicked.bind(this));
            } else {
                console.warn('[privatecaptcha][progress] checkbox not found in the Shadow DOM');
            }
        }
    }

    _syncHostClass(showPopupIfNeeded) {
        let hostClass = '';
        switch (this._displayMode) {
            case DISPLAY_HIDDEN:
                hostClass = 'hidden';
                break;
            case DISPLAY_POPUP:
                hostClass = showPopupIfNeeded ? 'floating' : 'hidden';
                break;
            case DISPLAY_WIDGET:
                break;
        }

        this.classList.remove('hidden', 'floating');
        if (hostClass) { this.classList.add(hostClass); }
    }

    /**
     * @param {Event} event
     */
    onCheckboxClicked(event) {
        event.preventDefault();
        if (this._debug) {
            console.debug("[privatecaptcha][progress] checkbox was clicked");
        }
        if (event.target.checked) {
            const checkEvent = new CustomEvent("privatecaptcha:checked", {
                bubbles: true,
                composed: true
            });

            this.dispatchEvent(checkEvent);
        } else {
            console.warn('[privatecaptcha][progress] checkbox was unchecked');
        }
    }

    /**
     * @param {number} percent
     */
    setProgress(percent) {
        if (STATE_IN_PROGRESS == this._state) {
            const progressBar = this._root.getElementById(PROGRESS_ID);
            if (progressBar) {
                progressBar.setProgress(percent);
            } else {
                console.warn('[privatecaptcha][progress] progress element not found');
            }
        } else {
            if (this._debug) {
                console.debug(`[privatecaptcha][progress] skipping updating progress when not in progress. state=${this._state}`);
            }
        }
    }

    /**
     * @param {number} value
     */
    setError(value) {
        this._error = value;
    }

    /**
     * @param {string} text
     * @param {boolean} error
     */
    setDebugText(text, error) {
        const debugElement = this._root.getElementById(DEBUG_ID);
        if (debugElement) {
            let debugText = '';
            if (this._error) {
                const strings = i18n.STRINGS[this._lang];
                debugText = errorDescription(this._error, strings);
            } else {
                debugText = `[${text}]`;
            }
            debugElement.textContent = debugText;
            if (error || this._error) {
                debugElement.classList.add(DEBUG_ERROR_CLASS);
            } else {
                debugElement.classList.remove(DEBUG_ERROR_CLASS);
            }
        }
    }

    static get observedAttributes() {
        return ['state', 'progress', 'extra-styles'];
    }

    updateStyles(newStyles) {
        const baseSheets = this._root.adoptedStyleSheets.filter(
            sheet => sheet !== this._overridesSheet
        );

        if (newStyles) {
            const cssText = `@layer custom { :host { ${newStyles} } }`;
            if (!this._overridesSheet) {
                this._overridesSheet = new CSSStyleSheet();
            }
            this._overridesSheet.replaceSync(cssText);
            this._root.adoptedStyleSheets = [...baseSheets, this._overridesSheet];
        } else {
            this._overridesSheet = null;
            this._root.adoptedStyleSheets = baseSheets;
        }
    }

    /**
     * @param {string} name
     * @param {string} oldValue
     * @param {string} newValue
     */
    attributeChangedCallback(name, oldValue, newValue) {
        if (oldValue === newValue) return;

        switch (name) {
            case 'progress':
                const progressValue = newValue !== null ? parseFloat(newValue) : NaN;
                if (!Number.isNaN(progressValue)) {
                    this.setProgress(progressValue);
                }
                break;
            case 'extra-styles':
                this.updateStyles(newValue);
                break;
        };
    }
}
