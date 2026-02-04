import test from 'node:test';
import assert from 'node:assert';
import { Window } from 'happy-dom';

const window = new Window({
    url: 'https://localhost:8080'
});

global.window = window;
global.document = window.document;
global.HTMLElement = window.HTMLElement;
global.CustomEvent = window.CustomEvent;
global.CSSStyleSheet = window.CSSStyleSheet;

const originalFetch = window.fetch.bind(window);
const patchedFetch = (url, options = {}) => {
    const headers = new window.Headers(options.headers || {});
    if (!headers.has('Origin')) {
        headers.set('Origin', 'not.empty');
    }

    return originalFetch(url, {
        ...options,
        headers
    });
};
window.fetch = patchedFetch;
globalThis.fetch = patchedFetch;

const testSitekey = 'aaaaaaaabbbbccccddddeeeeeeeeeeee';

// we have to mock worker too
global.Worker = class Worker {
    constructor() {
        this.onmessage = null;
        this.onerror = null;
    }

    postMessage(data) {
        setTimeout(() => {
            if (data.command === 'init') {
                this.onmessage?.({ data: { command: 'init' } });
            } else if (data.command === 'solve') {
                // Simulate immediate solution for zero puzzle
                this.onmessage?.({
                    data: {
                        command: 'solve',
                        argument: {
                            id: BigInt(data.argument.id || 0),
                            solution: new Uint8Array(8),
                            wasm: false
                        }
                    }
                });
            }
        }, 10);
    }

    terminate() { }
};


test('CaptchaWidget execute() fires finished event and callback', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha"
                 data-finished-callback="testFinishedCallback">
            </div>
        </form>
    `;

    let callbackCalled = false;
    global.window.testFinishedCallback = (widget) => {
        callbackCalled = true;
    };

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    assert.ok(element, 'Should find captcha element');

    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    // Set up event listener
    let eventFired = false;
    element.addEventListener('privatecaptcha:finish', (event) => {
        eventFired = true;
        assert.ok(event.detail.widget, 'Event should include widget in detail');
        assert.strictEqual(event.detail.element, element, 'Event should include element in detail');
    });

    widget.execute();

    await new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:finish', () => {
            clearTimeout(timeout);
            resolve();
        }, { once: true });
    });

    assert.strictEqual(eventFired, true, 'privatecaptcha:finish event should be fired');
    assert.strictEqual(callbackCalled, true, 'Finished callback should be called');

    // Verify solution is available
    const solution = widget.solution();
    assert.ok(solution, 'Widget should have a solution');
    assert.ok(typeof solution === 'string', 'Solution should be a string');

    console.log('✓ Widget execute test passed');
});

test('CaptchaWidget init() fires init event and callback', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha"
                 data-init-callback="testInitCallback">
            </div>
        </form>
    `;

    let callbackCalled = false;
    global.window.testInitCallback = (widget) => {
        callbackCalled = true;
    };

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    assert.ok(element, 'Should find captcha element');

    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    // Set up event listener
    let eventFired = false;
    element.addEventListener('privatecaptcha:init', (event) => {
        eventFired = true;
        assert.ok(event.detail.widget, 'Event should include widget in detail');
        assert.strictEqual(event.detail.element, element, 'Event should include element in detail');
    });

    widget.init(false);

    await new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:init', () => {
            clearTimeout(timeout);
            resolve();
        }, { once: true });
    });

    assert.strictEqual(eventFired, true, 'privatecaptcha:init event should be fired');
    assert.strictEqual(callbackCalled, true, 'Init callback should be called');

    console.log('✓ Widget init test passed');
});

test('CaptchaWidget execute() fires started event and callback', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha"
                 data-started-callback="testStartedCallback">
            </div>
        </form>
    `;

    let callbackCalled = false;
    global.window.testStartedCallback = (widget) => {
        callbackCalled = true;
    };

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    assert.ok(element, 'Should find captcha element');

    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    // Set up event listener
    let eventFired = false;
    element.addEventListener('privatecaptcha:start', (event) => {
        eventFired = true;
        assert.ok(event.detail.widget, 'Event should include widget in detail');
        assert.strictEqual(event.detail.element, element, 'Event should include element in detail');
    });

    widget.execute();

    await new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:start', () => {
            clearTimeout(timeout);
            resolve();
        }, { once: true });
    });

    assert.strictEqual(eventFired, true, 'privatecaptcha:start event should be fired');
    assert.strictEqual(callbackCalled, true, 'Started callback should be called');

    console.log('✓ Widget started test passed');
});

test('CaptchaWidget checkConfigured() shows invalid state without sitekey', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha">
            </div>
        </form>
    `;

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    // No sitekey provided
    const widget = new CaptchaWidget(element, { debug: true });

    const pcElement = element.querySelector('private-captcha');
    assert.ok(pcElement, 'Should find private-captcha element');

    // The widget should be in invalid state when no sitekey is provided
    // The checkConfigured() is called in constructor and sets the state to invalid
    // In invalid state, the checkbox should have class 'invalid'
    const shadowRoot = pcElement.shadowRoot;
    assert.ok(shadowRoot, 'Should have shadow root');
    const checkboxEl = shadowRoot.querySelector('input[type="checkbox"].invalid');
    assert.ok(checkboxEl, 'Should have invalid checkbox in invalid state');

    console.log('✓ Widget checkConfigured test passed');
});

test('CaptchaWidget constructor reads displayMode from data attribute', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha" data-display-mode="popup">
            </div>
        </form>
    `;

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    const pcElement = element.querySelector('private-captcha');
    assert.ok(pcElement, 'Should find private-captcha element');
    assert.strictEqual(pcElement.getAttribute('display-mode'), 'popup', 'Display mode should be popup from data attribute');

    console.log('✓ Widget displayMode from data attribute test passed');
});

test('CaptchaWidget storeVariable option stores widget reference on element', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha" data-store-variable="captchaWidget">
            </div>
        </form>
    `;

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    assert.strictEqual(element.captchaWidget, widget, 'Widget should be stored on element under storeVariable name');

    console.log('✓ Widget storeVariable test passed');
});

test('captcha.js renderCaptchaWidget prevents double attachment', async (t) => {
    document.body.innerHTML = `
        <form>
            <div id="test-double-attach" class="private-captcha">
            </div>
        </form>
    `;

    // Import the module to trigger the global setup
    await import('../js/captcha.js');

    const element = document.getElementById('test-double-attach');

    // Clear any previous attachment from auto setup
    delete element.dataset.attached;

    // First render
    const widget1 = window.privateCaptcha.render(element, { sitekey: testSitekey, debug: true });
    assert.ok(widget1, 'First render should return a widget');

    // Second render on same element should return null (already attached)
    const widget2 = window.privateCaptcha.render(element, { sitekey: testSitekey, debug: true });
    assert.strictEqual(widget2, null, 'Second render should return null for already attached element');

    console.log('✓ renderCaptchaWidget prevents double attachment test passed');
});

test('CaptchaWidget reset() clears internal state', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha" data-theme="light" data-lang="en">
            </div>
        </form>
    `;

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    assert.ok(element, 'Should find captcha element');

    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true,
        theme: 'light',
        lang: 'en'
    });

    // Manually set internal state to simulate a finished widget
    widget._solution = 'test-solution';
    assert.strictEqual(widget.solution(), 'test-solution', 'Solution should be set');

    // Add a solution field manually to test removal
    element.insertAdjacentHTML('beforeend', '<input name="private-captcha-solution" type="hidden" value="test">');
    const fieldBeforeReset = element.querySelector('input[name="private-captcha-solution"]');
    assert.ok(fieldBeforeReset, 'Solution field should exist before reset');

    // Reset with new options
    widget.reset({ theme: 'dark', lang: 'fr' });

    const solutionAfterReset = widget.solution();
    assert.strictEqual(solutionAfterReset, null, 'Solution should be null after reset');

    // Verify solution field is removed
    const fieldAfterReset = element.querySelector('input[name="private-captcha-solution"]');
    assert.strictEqual(fieldAfterReset, null, 'Solution field should be removed after reset');

    // Verify new options are reflected in the custom element attributes
    const pcElement = element.querySelector('private-captcha');
    assert.ok(pcElement, 'Should find private-captcha element');
    assert.strictEqual(pcElement.getAttribute('theme'), 'dark', 'Theme should be updated to dark');
    assert.strictEqual(pcElement.getAttribute('lang'), 'fr', 'Lang should be updated to fr');

    console.log('✓ Widget reset test passed');
});

test('CaptchaWidget setOptions() configures fieldName for recaptcha compat mode', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha">
            </div>
        </form>
    `;

    const { CaptchaWidget, RECAPTCHA_COMPAT } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true,
        compat: RECAPTCHA_COMPAT
    });

    // Verify the fieldName option is set for recaptcha compat
    assert.strictEqual(widget._options.fieldName, 'g-recaptcha-response', 'Field name should be g-recaptcha-response in recaptcha compat mode');

    console.log('✓ Widget recaptcha compat field name test passed');
});

test('CaptchaWidget setOptions() reads sitekey from data attribute', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha" data-sitekey="${testSitekey}">
            </div>
        </form>
    `;

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    // Do not pass sitekey in options
    const widget = new CaptchaWidget(element, { debug: true });

    // Verify the sitekey was read from data attribute
    assert.strictEqual(widget._options.sitekey, testSitekey, 'Sitekey should be read from data attribute');

    console.log('✓ Widget sitekey from data attribute test passed');
});

test('CaptchaWidget setOptions() uses EU endpoint when data-eu is set', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha" data-eu="true">
            </div>
        </form>
    `;

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    // Verify the EU endpoint is used
    assert.ok(widget._options.puzzleEndpoint.includes('eu'), 'Puzzle endpoint should use EU endpoint');

    console.log('✓ Widget EU endpoint test passed');
});

test('CaptchaWidget respects puzzle timeout option', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha">
            </div>
        </form>
    `;

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    const originalFetch = globalThis.fetch;
    const originalSetTimeout = globalThis.setTimeout;
    const abortTimings = [];
    const TEST_PUZZLE_TIMEOUT_SHORT_MS = 30;
    const TIMEOUT_ACCELERATION_THRESHOLD_MS = 100;
    const MOCK_FETCH_DELAY_MS = 1000;
    const MAX_TIMEOUT_ASSERT_MS = 500;
    const MAX_ABORT_ASSERT_MS = 200;
    globalThis.setTimeout = (fn, delay, ...args) => {
        if (delay >= TIMEOUT_ACCELERATION_THRESHOLD_MS) {
            return originalSetTimeout(fn, 0, ...args);
        }
        return originalSetTimeout(fn, delay, ...args);
    };
    globalThis.fetch = (url, options = {}) => {
        return new Promise((resolve, reject) => {
            const startedAt = Date.now();
            const waitTimer = originalSetTimeout(() => {
                resolve(new window.Response('', { status: 500 }));
            }, MOCK_FETCH_DELAY_MS);
            if (options.signal) {
                options.signal.addEventListener('abort', () => {
                    abortTimings.push(Date.now() - startedAt);
                    clearTimeout(waitTimer);
                    reject(new DOMException('Aborted', 'AbortError'));
                }, { once: true });
            }
        });
    };

    try {
        const widget = new CaptchaWidget(element, {
            sitekey: testSitekey,
            debug: true,
            puzzleEndpoint: 'https://privatecaptcha.invalid/puzzle',
            puzzleTimeoutMs: TEST_PUZZLE_TIMEOUT_SHORT_MS
        });

        const startedAt = Date.now();
        await widget.init(false);
        const elapsedMs = Date.now() - startedAt;
        assert.ok(elapsedMs < MAX_TIMEOUT_ASSERT_MS, `Expected timeout before ${MAX_TIMEOUT_ASSERT_MS}ms, got ${elapsedMs}ms`);
        assert.ok(abortTimings.every((timing) => timing < MAX_ABORT_ASSERT_MS), `Expected aborts under ${MAX_ABORT_ASSERT_MS}ms, got ${abortTimings.join(',')}`);
    } finally {
        globalThis.fetch = originalFetch;
        globalThis.setTimeout = originalSetTimeout;
    }

    console.log('✓ Widget puzzle timeout respected test passed');
});

test('captcha.js resetCaptchaWidget clears widget solution', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha">
            </div>
        </form>
    `;

    await import('../js/captcha.js');
    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    // Manually set internal state
    widget._solution = 'test-solution';
    assert.ok(widget.solution(), 'Widget should have solution before reset');

    window.privateCaptcha.reset(widget);

    assert.strictEqual(widget.solution(), null, 'Widget solution should be null after reset');

    console.log('✓ resetCaptchaWidget test passed');
});

test('captcha.js getResponse returns widget solution', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha">
            </div>
        </form>
    `;

    await import('../js/captcha.js');
    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    // Manually set solution
    widget._solution = 'test-solution-payload';

    const response = window.privateCaptcha.getResponse(widget);
    assert.strictEqual(response, 'test-solution-payload', 'getResponse should return the widget solution');

    console.log('✓ getResponse test passed');
});
