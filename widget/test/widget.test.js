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
