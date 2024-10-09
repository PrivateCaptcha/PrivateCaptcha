'use strict';

import { ProgressRing } from './progress.js';
import styles from "./styles.css" assert {type: 'css'};
import * as i18n from './strings.js';

window.customElements.define('progress-ring', ProgressRing);

export const STATE_EMPTY = 'empty';
export const STATE_LOADING = 'loading';
export const STATE_READY = 'ready';
export const STATE_IN_PROGRESS = 'inprogress';
export const STATE_VERIFIED = 'verified';

export const DISPLAY_POPUP = 'popup';
const DISPLAY_HIDDEN = 'hidden';
const DISPLAY_WIDGET = 'widget';

const RING_SIZE = 36;
const CHECKBOX_ID = 'pc-checkbox';
const PROGRESS_ID = 'pc-progress';
const DEBUG_ID = 'pc-debug';

const privateCaptchaSVG = `<svg viewBox="0 0 39.4 41.99" xml:space="preserve" width="39.4" height="41.99" xmlns="http://www.w3.org/2000/svg" class="pc-logo">
<path d="M0 0v30.62l4.29 2.48V4.85h30.83v23.29l-15.41 8.9-6.83-3.94v-4.95l6.83 3.94 11.12-6.42V9.91H8.58v25.66l11.12 6.42 19.7-11.37V0Zm12.87 14.86h13.66v8.32l-6.83 3.94-6.83-3.94z" fill="currentColor"/>
</svg>`;

const verifiedSVG = `<svg class="verified" xmlns="http://www.w3.org/2000/svg" width="${RING_SIZE}px" height="${RING_SIZE}px" viewBox="0 0 154 154">
<g fill="none"><circle fill="#0080A0" cx="77" cy="77" r="76"></circle>
<polyline class="st0" stroke="#F2F2F2" stroke-width="12" points="43.5,77.8 63.7,97.9 112.2,49.4" style="stroke-dasharray:100px, 100px; stroke-dashoffset: 200px;"/></g>
</svg>
`;

const activeAreaEmptyCheckbox = `<input type="checkbox" id="${CHECKBOX_ID}" required>`;

function checkbox(cls) {
    return `<input type="checkbox" id="${CHECKBOX_ID}" class="${cls}" required>`
}

function label(text, forElement) {
    return `<label for="${forElement}">${text}</label>`;
}

export class CaptchaElement extends HTMLElement {
    constructor() {
        super();
        this._state = '';
        // create shadow dom root
        this._root = this.attachShadow({ mode: 'open' });
        this._debug = this.getAttribute('debug');
        this._displayMode = this.getAttribute('display-mode');
        this._lang = this.getAttribute('lang');
        if (!(this._lang in i18n.STRINGS)) {
            console.warn(`[privatecaptcha][progress] Localization not found. lang=${this._lang}`);
            this._lang = 'en';
        }
        // custom click event
        this.checkEvent = new CustomEvent("check", {
            bubbles: true,
            cancelable: false,
            composed: true
        });
        // add CSS
        const sheet = new CSSStyleSheet();
        sheet.replace(styles);
        this._root.adoptedStyleSheets.push(sheet);
        // add CSS overrides
        const extraStyles = this.getAttribute('extra-styles');
        if (extraStyles) {
            const overridesSheet = new CSSStyleSheet();
            overridesSheet.replace(extraStyles);
            this._root.adoptedStyleSheets.push(overridesSheet);
        }
        // init
        this.setState(STATE_EMPTY);
    }

    setState(state) {
        if (state == this._state) {
            console.debug('[privatecaptcha][progress] already in this state: ' + state);
            return;
        }

        if (this._debug) { console.debug(`[privatecaptcha][progress] change state old=${this._state} new=${state}`); }

        let activeArea = '';
        let bindCheckEvent = false;
        let showPopupIfNeeded = false;
        const strings = i18n.STRINGS[this._lang];

        switch (state) {
            case STATE_EMPTY:
                bindCheckEvent = true;
                activeArea = activeAreaEmptyCheckbox + label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID);
                break;
            case STATE_LOADING:
                bindCheckEvent = true;
                activeArea = checkbox('loading') + label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID);
                break;
            case STATE_READY:
                bindCheckEvent = true;
                activeArea = checkbox('ready') + label(strings[i18n.CLICK_TO_VERIFY], CHECKBOX_ID);
                showPopupIfNeeded = true;
                break;
            case STATE_IN_PROGRESS:
                const text = strings[i18n.VERIFYING];
                activeArea = `<progress-ring id="${PROGRESS_ID}" stroke="12" size="${RING_SIZE}" progress="0"></progress-ring><label for="${PROGRESS_ID}">${text}<span class="dots"><span>.</span><span>.</span><span>.</span></span></label>`;
                showPopupIfNeeded = true;
                break;
            case STATE_VERIFIED:
                activeArea = verifiedSVG + label(strings[i18n.SUCCESS], PROGRESS_ID);
                showPopupIfNeeded = true;
                break;
            default:
                console.error(`[privatecaptcha][progress] unknown state: ${state}`);
                break;
        }

        if (this._debug) {
            activeArea += `<span id="${DEBUG_ID}">[${state}]</span>`;
        }

        let displayClass = '';
        switch (this._displayMode) {
            case DISPLAY_HIDDEN:
                displayClass = 'hidden';
                break;
            case DISPLAY_POPUP:
                displayClass = showPopupIfNeeded ? 'floating' : 'hidden';
                break;
            case DISPLAY_WIDGET:
                break;
        };

        this._state = state;
        this._root.innerHTML = `<div class="pc-captcha-widget ${displayClass}">
            <div class="pc-interactive-area">
                ${activeArea}
            </div>
            <div class="pc-info">
                ${privateCaptchaSVG}
                <a href="https://privatecaptcha.com" class="pc-link" rel="noopener" target="_blank">Private<br />Captcha</a>
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

    onCheckboxClicked(event) {
        event.preventDefault();
        if (this._debug) {
            console.debug("[privatecaptcha][progress] checkbox was clicked");
        }
        if (event.target.checked) {
            this.dispatchEvent(this.checkEvent);
        } else {
            console.warn('[privatecaptcha][progress] checkbox was unchecked');
        }
    }

    setProgress(percent) {
        if (STATE_IN_PROGRESS == this._state) {
            const progressBar = this._root.getElementById(PROGRESS_ID);
            if (progressBar) {
                progressBar.setProgress(percent);
            } else {
                console.warn('[privatecaptcha][progress] progress element not found');
            }
        }
    }

    setDebugState(state) {
        const debugElement = this._root.getElementById(DEBUG_ID);
        if (debugElement) {
            debugElement.innerHTML = `[${state}]`;
        }
    }

    static get observedAttributes() {
        return ['state', 'progress'];
    }

    attributeChangedCallback(name, oldValue, newValue) {
        if ('progress' === name) {
            this.setProgress(newValue);
        }
    }
}
