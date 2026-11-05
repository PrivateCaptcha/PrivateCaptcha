import test from 'node:test';
import assert from 'node:assert';
import { Window } from 'happy-dom';
import { getClientHints } from '../js/hints.js';

test('client hints are empty for a normal browser', () => {
    assert.deepStrictEqual(getClientHints({
        navigator: { userAgent: 'Mozilla/5.0 Chrome/132.0.0.0 Safari/537.36', webdriver: false },
        Function,
    }), []);
});

for (const property of [
    'domAutomation', 'domAutomationController', '_WEBDRIVER_ELEM_CACHE',
    'callPhantom', '_phantom', '__nightmare',
    'cdc_adoQpoasnfa76pfcZLmcfl_Array', 'cdc_adoQpoasnfa76pfcZLmcfl_Promise',
    'cdc_adoQpoasnfa76pfcZLmcfl_Symbol',
]) {
    test(`client hints detect the ${property} automation leak`, () => {
        assert.deepStrictEqual(getClientHints({ [property]: {} }), ['automation']);
        assert.deepStrictEqual(getClientHints({ [property]: false }), []);
    });
}

test('client hints do not flag host-page elements exposed via Window named access', () => {
    // Per the HTML spec ("named access on the Window object"), any element with
    // id="webdriver" or id="selenium" is exposed as a truthy window property.
    // The widget runs in the host page's top window, so these generic names must
    // not be probed as automation leaks or every real visitor on such a page is
    // falsely flagged. The W3C signal is already covered by navigator.webdriver.
    for (const property of ['webdriver', 'selenium']) {
        assert.deepStrictEqual(getClientHints({
            navigator: { userAgent: 'Mozilla/5.0 Chrome/132.0.0.0 Safari/537.36', webdriver: false },
            [property]: {},
            Function,
        }), []);
    }
});

test('real DOM with <section id="selenium"> does not flag automation via Window named access', async () => {
    const window = new Window();
    window.document.write('<!DOCTYPE html><html><body><section id="selenium">Selenium docs</section></body></html>');

    assert.ok(window.selenium, 'window.selenium should be truthy via named access');
    assert.ok(window.selenium instanceof window.HTMLElement, 'window.selenium should be an HTMLElement');

    Object.defineProperty(window.navigator, 'webdriver', { value: false, configurable: true });

    assert.deepStrictEqual(getClientHints(window), [], 'getClientHints should not flag automation for id="selenium"');
});

test('real DOM with <section id="webdriver"> does not flag automation via Window named access', async () => {
    const window = new Window();
    window.document.write('<!DOCTYPE html><html><body><section id="webdriver">WebDriver docs</section></body></html>');

    assert.ok(window.webdriver, 'window.webdriver should be truthy via named access');
    assert.ok(window.webdriver instanceof window.HTMLElement, 'window.webdriver should be an HTMLElement');

    Object.defineProperty(window.navigator, 'webdriver', { value: false, configurable: true });

    assert.deepStrictEqual(getClientHints(window), [], 'getClientHints should not flag automation for id="webdriver"');
});

for (const property of ['webdriver', 'seleniumWebdriver']) {
    test(`client hints detect navigator.${property}`, () => {
        assert.deepStrictEqual(getClientHints({ navigator: { [property]: true } }), ['automation']);
        assert.deepStrictEqual(getClientHints({ navigator: { [property]: false } }), []);
    });
}

test('client hints detect headless user agents case insensitively', () => {
    for (const userAgent of ['Mozilla/5.0 HeadlessChrome/132.0.0.0', 'HEADLESS', 'headless']) {
        assert.deepStrictEqual(getClientHints({ navigator: { userAgent } }), ['automation']);
    }
});

for (const marker of ['__puppeteer_evaluation_script__', '__playwright_evaluation_script__']) {
    test(`client hints detect the ${marker} function wrapper`, () => {
        const browser = {
            Function: { prototype: { toString: { toString: () => `function toString() { ${marker} }` } } },
        };
        assert.deepStrictEqual(getClientHints(browser), ['automation']);
    });
}

test('client hints report automation only once when several checks match', () => {
    assert.deepStrictEqual(getClientHints({
        navigator: { webdriver: true, userAgent: 'HeadlessChrome' },
        domAutomation: true,
        _phantom: true,
    }), ['automation']);
});

test('client hints tolerate missing browser APIs and throwing getters', () => {
    assert.deepStrictEqual(getClientHints({}), []);
    assert.deepStrictEqual(getClientHints({
        get navigator() { throw new Error('navigator unavailable'); },
    }), []);
    assert.deepStrictEqual(getClientHints({
        Function: { prototype: { toString: { toString() { throw new Error('toString unavailable'); } } } },
    }), []);
});

test('client hints detect CDP serialization of a console error stack', () => {
    const browser = {
        location: { hostname: 'example.com', search: '' },
        console: { log(error) { return error.stack; } },
    };
    assert.deepStrictEqual(getClientHints(browser), ['automation']);
});

test('client hints stay empty when the console does not read the error stack', () => {
    let loggedError;
    const browser = {
        location: { hostname: 'example.com', search: '' },
        console: { log(error) { loggedError = error; } },
    };
    assert.deepStrictEqual(getClientHints(browser), []);
    assert.ok(loggedError instanceof Error);
});

for (const location of [
    { hostname: 'localhost', search: '' },
    { hostname: 'example.com', search: '?botDebug' },
    { hostname: 'example.com', search: '?other=1&botDebug=true' },
]) {
    test(`client hints skip the CDP probe on ${location.hostname}${location.search}`, () => {
        let logged = false;
        const browser = {
            location,
            console: { log(error) { logged = true; return error.stack; } },
        };
        assert.deepStrictEqual(getClientHints(browser), []);
        assert.strictEqual(logged, false);
        assert.deepStrictEqual(getClientHints({ ...browser, navigator: { webdriver: true } }), ['automation']);
    });
}

test('client hints tolerate an unavailable or throwing console', () => {
    const location = { hostname: 'example.com', search: '' };
    assert.deepStrictEqual(getClientHints({ location }), []);
    assert.deepStrictEqual(getClientHints({
        location,
        console: { log() { throw new Error('console unavailable'); } },
    }), []);
});
