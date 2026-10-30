'use strict';

const AUTOMATION_PROPERTIES = [
    'domAutomation', 'domAutomationController',
    '_WEBDRIVER_ELEM_CACHE', 'webdriver', 'selenium',
    'callPhantom', '_phantom', '__nightmare',
    'cdc_adoQpoasnfa76pfcZLmcfl_Array',
    'cdc_adoQpoasnfa76pfcZLmcfl_Promise',
    'cdc_adoQpoasnfa76pfcZLmcfl_Symbol',
];

export function getClientHints(browser = globalThis) {
    try {
        const navigator = browser.navigator;
        if (navigator?.webdriver || navigator?.seleniumWebdriver ||
            AUTOMATION_PROPERTIES.some((property) => !!browser[property]) ||
            /headless/i.test(navigator?.userAgent ?? '')) {
            return ['automation'];
        }

        // Some automation tools leave their evaluation markers in function wrappers.
        const functionSource = browser.Function?.prototype.toString.toString() ?? '';
        if (functionSource.includes('__puppeteer_evaluation_script__') ||
            functionSource.includes('__playwright_evaluation_script__') ||
            hasCDP(browser)) {
            return ['automation'];
        }
    } catch {
        // Best-effort detection must not prevent loading a puzzle.
    }

    return [];
}

function hasCDP(browser) {
    const location = browser.location;
    if (!location || location.hostname === 'localhost' || location.search?.includes('botDebug')) {
        return false;
    }

    let detected = false;
    const error = new Error('');
    // CDP may read the stack when serializing a console error.
    Object.defineProperty(error, 'stack', {
        get() {
            detected = true;
            return '';
        },
    });
    browser.console?.log(error);
    return detected;
}
