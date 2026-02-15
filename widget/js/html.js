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
</svg>`;

function htmlToElement(html) {
    const template = document.createElement('template');
    template.innerHTML = html.trim();
    return template.content.firstChild;
}

function checkbox(cls) {
    const el = document.createElement('input');
    el.type = 'checkbox';
    el.id = CHECKBOX_ID;
    el.className = cls;
    el.required = true;
    return el;
}

function label(text, forElement) {
    const el = document.createElement('label');
    el.htmlFor = forElement;
    el.textContent = text;
    return el;
}

function progressRing() {
    const el = document.createElement('progress-ring');
    el.id = PROGRESS_ID;
    el.setAttribute('stroke', '12');
    el.setAttribute('progress', '0');
    return el;
}

function progressLabel(text) {
    const el = label(text, PROGRESS_ID);
    const dots = document.createElement('span');
    dots.className = 'dots';
    for (let i = 0; i < 3; i++) {
        const dot = document.createElement('span');
        dot.textContent = '.';
        dots.appendChild(dot);
    }
    el.appendChild(dots);
    return el;
}

function debugSpan(text, isError, internalError) {
    const el = document.createElement('span');
    el.id = DEBUG_ID;
    if (isError) { el.className = DEBUG_ERROR_CLASS; }
    
    if (internalError) {
        el.style.textDecoration = 'underline';
        el.style.textDecorationStyle = 'dashed';
        el.style.cursor = 'help';
        el.title = internalError;
    }
    
    el.textContent = text;
    return el;
}

function infoSection() {
    const info = document.createElement('div');
    info.className = 'pc-info';
    info.appendChild(htmlToElement(privateCaptchaSVG));
    const link = document.createElement('a');
    link.href = 'https://privatecaptcha.com';
    link.className = 'pc-link';
    link.rel = 'noopener nofollow';
    link.target = '_blank';
    link.appendChild(document.createTextNode('Private'));
    link.appendChild(document.createElement('br'));
    link.appendChild(document.createTextNode('Captcha'));
    info.appendChild(link);
    return info;
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
        this._internalError = null;
        this._displayMode = DISPLAY_HIDDEN;
        this._lang = 'en';

        // Add CSS
        const sheet = new CSSStyleSheet();
        sheet.replaceSync(styles);
        this._root.adoptedStyleSheets = [sheet];
        this._overridesSheet = null;
    }

    update() {
        this._debug = this.getAttribute('debug');
        this._error = null;
        this._internalError = null;
        this._displayMode = this.getAttribute('display-mode');
        this._lang = this.getAttribute('lang');
        if (!(this._lang in i18n.STRINGS)) {
            console.warn(`[privatecaptcha][progress] Localization not found. lang=${this._lang}`);
            this._lang = 'en';
        }

        // add CSS overrides
        const extraStyles = this.getAttribute('extra-styles');
        this.updateStyles(extraStyles);

    }

    connectedCallback() {
        this.update();

        // init
        const canShow = (this._displayMode == DISPLAY_WIDGET);
        this.setState(STATE_EMPTY, canShow);
    }

    /**
     * @param {string} state
     * @param {boolean} canShow
     * @returns {boolean} if changed
     */
    setState(state, canShow) {
        if (state == this._state) {
            console.debug('[privatecaptcha][progress] already in this state: ' + state);
            if (DISPLAY_POPUP === this._displayMode) {
                this._syncHostClass(canShow);
            }
            return false;
        }

        if (this._debug) { console.debug(`[privatecaptcha][progress] change state. old=${this._state} new=${state}`); }

        this.render(state, canShow);
        this._state = state;

        return true;
    }

    /**
     * Rebuilds the widget UI inside the shadow DOM based on the given state and
     * visibility settings. This updates the interactive area contents, applies
     * host element CSS classes (including popup-related behavior), and binds
     * the checkbox event handler when needed.
     *
     * @param {string} state - The new widget state (for example, empty, loading, or error).
     * @param {boolean} canShow - Whether the widget is allowed to be shown / expanded to the user.
     * @returns {void}
     */
    render(state, canShow) {
        const activeArea = document.createElement('div');
        activeArea.className = 'pc-interactive-area';
        let bindCheckEvent = false;
        let showPopupIfNeeded = false;
        const strings = i18n.STRINGS[this._lang];

        switch (state) {
            case STATE_EMPTY:
                bindCheckEvent = true;
                activeArea.appendChild(checkbox(''));
                activeArea.appendChild(label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID));
                break;
            case STATE_LOADING:
                bindCheckEvent = true;
                activeArea.appendChild(checkbox('loading'));
                activeArea.appendChild(label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID));
                break;
            case STATE_READY:
                bindCheckEvent = true;
                activeArea.appendChild(checkbox('ready'));
                activeArea.appendChild(label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID));
                showPopupIfNeeded = canShow;
                break;
            case STATE_IN_PROGRESS:
                activeArea.appendChild(progressRing());
                activeArea.appendChild(progressLabel(strings[i18n.VERIFYING]));
                showPopupIfNeeded = canShow;
                break;
            case STATE_VERIFIED:
                activeArea.appendChild(htmlToElement(verifiedSVG));
                activeArea.appendChild(label(strings[i18n.SUCCESS], PROGRESS_ID));
                showPopupIfNeeded = canShow;
                break;
            case STATE_INVALID:
                activeArea.appendChild(checkbox('invalid'));
                activeArea.appendChild(label(strings[i18n.UNAVAILABLE], CHECKBOX_ID));
                break;
            default:
                console.error(`[privatecaptcha][progress] unknown state: ${state}`);
                break;
        }

        if (this._debug || this._error) {
            const text = this._error ? errorDescription(this._error, strings) : `[${state}]`;
            activeArea.appendChild(debugSpan(text, this._error, (this._debug && this._error) ? this._internalError : null));
        }

        this._syncHostClass(showPopupIfNeeded);

        const widget = document.createElement('div');
        widget.className = 'pc-captcha-widget';
        widget.appendChild(activeArea);
        const spacer = document.createElement('div');
        spacer.className = 'pc-spacer';
        widget.appendChild(spacer);
        widget.appendChild(infoSection());

        this._root.replaceChildren(widget);

        if (bindCheckEvent) {
            const checkboxEl = this._root.getElementById(CHECKBOX_ID);
            if (checkboxEl) {
                checkboxEl.addEventListener('change', this.onCheckboxClicked.bind(this));
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
     * @param {string} internalError
     */
    setError(value, internalError) {
        this._error = value;
        this._internalError = internalError;
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
        if (oldValue === newValue) { return; }

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
