'use strict';

const AUTOMATION_PROPERTIES = [
    'domAutomation', 'domAutomationController',
    '_WEBDRIVER_ELEM_CACHE',
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
            functionSource.includes('__playwright_evaluation_script__')) {
            return ['automation'];
        }
    } catch {
        // Best-effort detection must not prevent loading a puzzle.
    }

    return [];
}
