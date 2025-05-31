'use strict';

import { CaptchaWidget } from './widget.js';

window.privateCaptcha = {
    setup: setupPrivateCaptcha,
    // just a class declaration
    //CaptchaWidget: CaptchaWidget,
};

function findCaptchaElements() {
    const elements = document.querySelectorAll('.private-captcha');
    if (elements.length === 0) {
        console.warn('PrivateCaptcha: No div was found with .private-captcha class');
    }
    return elements;
}

function setupPrivateCaptcha() {
    let autoWidget = window.privateCaptcha.autoWidget;

    const elements = findCaptchaElements();
    for (let htmlElement of elements) {
        if (htmlElement && !htmlElement.dataset['attached']) {
            autoWidget = new CaptchaWidget(htmlElement);
            // We set the "data-attached" attribute so we don't attach to the same element twice.
            htmlElement.dataset['attached'] = '1';
        }
    }
    window.privateCaptcha.autoWidget = autoWidget;
}

if (document.readyState !== 'loading') {
    setupPrivateCaptcha();
} else {
    document.addEventListener('DOMContentLoaded', setupPrivateCaptcha);
}
