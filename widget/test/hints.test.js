import test from 'node:test';
import assert from 'node:assert';
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
    'cdc_adoQpoasnfa76pfcZLmcfl_Symbol', 'webdriver', 'driver', 'selenium',
]) {
    test(`client hints detect the ${property} automation leak`, () => {
        assert.deepStrictEqual(getClientHints({ [property]: {} }), ['automation']);
        assert.deepStrictEqual(getClientHints({ [property]: false }), []);
    });
}

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
        selenium: true,
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
