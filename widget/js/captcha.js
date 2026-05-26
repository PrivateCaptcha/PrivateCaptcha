'use strict';

import { CaptchaWidget, RECAPTCHA_COMPAT } from './widget.js';

window.privateCaptcha = {
    setup: setupPrivateCaptcha,
    render: renderCaptchaWidget,
    getResponse: getCaptchaResponse,
    reset: resetCaptchaWidget,
    autoWidget: null,
};

const RENDER_EXPLICIT = "explicit";

/**
 * Finds all captcha elements on the page
 * @param {string} compatMode
 */
function findCaptchaElements(compatMode) {
    const selector = (compatMode === RECAPTCHA_COMPAT) ? '.g-recaptcha' : '.private-captcha';
    const elements = document.querySelectorAll(selector);
    if (elements.length === 0) {
        console.warn(`'PrivateCaptcha: No element was found with ${selector} class`);
    }
    return elements;
}

function getBaseOptions() {
    let scriptTag;
    const scripts = document.getElementsByTagName('script');
    for (let script of scripts) {
        if (script.src.includes('widget/js/privatecaptcha.js')) {
            scriptTag = script;
            break;
        }
    }

    let options = {
        compat: null,
        render: null,
    };

    if (scriptTag) {
        const scriptUrl = new URL(scriptTag.src);
        const params = scriptUrl.searchParams;

        const compatMode = params.get('compat');
        if (compatMode && (typeof compatMode === 'string') && (compatMode.length > 0)) {
            options.compat = compatMode;
        }

        const renderMode = params.get('render');
        if (renderMode && (typeof renderMode === 'string') && (renderMode.length > 0)) {
            options.render = renderMode;
        }
    }

    return options;
}

function setupPrivateCaptcha() {
    let options = getBaseOptions();
    const newWidgets = [];

    if (options.render !== RENDER_EXPLICIT) {
        let autoWidget = window.privateCaptcha.autoWidget;

        const elements = findCaptchaElements(options.compat);
        for (let htmlElement of elements) {
            const widget = renderCaptchaWidget(htmlElement, options);
            if (widget) {
                newWidgets.push(widget);
                autoWidget = widget;
            }
        }

        window.privateCaptcha.autoWidget = autoWidget;
    }

    if (options.compat === RECAPTCHA_COMPAT) {
        window.grecaptcha = window.privateCaptcha;
    }

    return newWidgets;
}

/**
 * Google reCAPTCHA (and hCAPTCHA) compatibility layer: render
 * @param {HTMLElement} element
 * @param {Object} options
 * @returns {CaptchaWidget} instance or null if already attached
 */
function renderCaptchaWidget(element, options) {
    let widget = null;

    if (element && !element.dataset['attached']) {
        widget = new CaptchaWidget(element, options);
        // We set the "data-attached" attribute so we don't attach to the same element twice.
        element.dataset['attached'] = '1';
    }

    return widget;
}

/**
 * Google reCAPTCHA compatibility layer: reset
 * @param {CaptchaWidget} widget created with renderCaptchaWidget()
 */
function resetCaptchaWidget(widget) {
    if (widget) {
        widget.reset();
    } else if (window.privateCaptcha.autoWidget) {
        window.privateCaptcha.autoWidget.reset();
    }
}

/**
 * Google reCAPTCHA compatibility layer: getResponse
 * @param {CaptchaWidget} widget created with renderCaptchaWidget()
 * @returns {string} the response for the Private Captcha widget
 */
function getCaptchaResponse(widget) {
    if (widget) {
        return widget.solution();
    } else if (window.privateCaptcha.autoWidget) {
        return window.privateCaptcha.autoWidget.solution();
    } else {
        return null;
    }
}

if (document.readyState !== 'loading') {
    setupPrivateCaptcha();
} else {
    document.addEventListener('DOMContentLoaded', setupPrivateCaptcha);
}
