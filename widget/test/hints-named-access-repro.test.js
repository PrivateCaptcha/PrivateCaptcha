import test from 'node:test';
import assert from 'node:assert';
import { Window } from 'happy-dom';
import { getClientHints } from '../js/hints.js';

// Per the HTML spec ("named access on the Window object"), any element with
// id="webdriver" or id="selenium" is exposed as a truthy window property.
// The widget runs in the host page's top window, so these generic names must
// not be probed as automation leaks or every real visitor on such a page is
// falsely flagged. The W3C signal is already covered by navigator.webdriver.

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
