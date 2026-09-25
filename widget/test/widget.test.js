import test from 'node:test';
import assert from 'node:assert';
import { readFile } from 'node:fs/promises';
import { Window } from 'happy-dom';
import { encode } from 'base64-arraybuffer';
import './providers.test.js';
import './argon2-wasm.test.js';
import './argon2-search.test.js';
import './benchmark.test.js';
import './workerspool.test.js';
import { createWorkerSolver as createBlake2bWorkerSolver } from '../js/worker-solver-blake2b.js';
import { blake2b, blake2bInit, blake2bUpdate, blake2bFinal } from 'blakejs/blake2b.js';

function fallbackHasher(outlen) {
    const context = blake2bInit(outlen);
    return {
        update(input) {
            blake2bUpdate(context, input);
            return this;
        },
        digest(output) {
            const result = blake2bFinal(context);
            if (output) {
                output.set(result);
                return output;
            }
            return result;
        },
    };
}

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

test('Blake2b worker solves Blake2b and rejects Argon2id', async () => {
    const body = new Uint8Array(128);
    const solver = await createBlake2bWorkerSolver(0, body);
    assert.strictEqual(typeof solver.solve, 'function');
    await assert.rejects(createBlake2bWorkerSolver(1, body), /Unsupported puzzle challenge: 1/);
});

const protocolFixtures = {
    v1: {
        version: 1, challenge: 0, userData: '101112131415161718191a1b1c1d1e1f',
        body: '01000102030405060708090a0b0c0d0e0f0807060504030201981880857467101112131415161718191a1b1c1d1e1f',
    },
    v2: {
        version: 2, challenge: 1, userData: '101112131415161718191a1b1c1d1e1f',
        body: '0201000102030405060708090a0b0c0d0e0f0807060504030201981880857467101112131415161718191a1b1c1d1e1f',
    },
};

function bytesFromHex(value) {
    return Uint8Array.from(value.match(/.{2}/g), (byte) => Number.parseInt(byte, 16));
}

function hexFromBytes(value) {
    return Array.from(value, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

function puzzlePayload(body) {
    return `${encode(body.buffer)}.signature`;
}

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

test('default widget fails when an Argon2id property issues an Argon2id puzzle', { timeout: 2000 }, async () => {
    const { CaptchaWidget } = await import('../js/widget.js');
    const { WorkersPool } = await import('../js/workerspool.js');
    const { STATE_ERROR } = await import('../js/html.js');
    document.body.innerHTML = '<form><div class="private-captcha"></div></form>';
    const element = document.querySelector('.private-captcha');
    const previousFetch = globalThis.fetch;
    globalThis.fetch = async () => {
        return { ok: true, text: async () => puzzlePayload(bytesFromHex(protocolFixtures.v2.body)), headers: { get: () => null } };
    };

    class BlakeOnlyWorker {
        postMessage(message) {
            if (message.command === 'init') {
                void createBlake2bWorkerSolver(message.argument.challenge, message.argument.buffer)
                    .then(() => this.onmessage?.({ data: { command: 'init' } }),
                        (error) => this.onmessage?.({ data: { command: 'error', error: error.message } }));
            }
        }
        terminate() { }
    }

    try {
        const widget = new CaptchaWidget(element, { sitekey: testSitekey });
        widget._workersPool = new WorkersPool({
            workersReady: widget.onWorkersReady.bind(widget),
            workerError: widget.onWorkerError.bind(widget),
        }, false, BlakeOnlyWorker);
        const failed = new Promise((resolve) => element.addEventListener('privatecaptcha:error', resolve, { once: true }));
        await widget.init(false);
        await failed;
        assert.strictEqual(widget._state, STATE_ERROR);
        assert.strictEqual(widget.solution(), null);
        assert.strictEqual(element.querySelector('input[name="private-captcha-solution"]'), null);
    } finally {
        globalThis.fetch = previousFetch;
    }
});


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

    let eventFired = false;
    const finishedEvent = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:finish', (event) => {
            clearTimeout(timeout);
            eventFired = true;
            assert.ok(event.detail.widget, 'Event should include widget in detail');
            assert.strictEqual(event.detail.element, element, 'Event should include element in detail');
            resolve();
        }, { once: true });
    });

    widget.execute();

    await finishedEvent;

    assert.strictEqual(eventFired, true, 'privatecaptcha:finish event should be fired');
    assert.strictEqual(callbackCalled, true, 'Finished callback should be called');

    // Verify solution is available
    const solution = widget.solution();
    assert.ok(solution, 'Widget should have a solution');
    assert.ok(typeof solution === 'string', 'Solution should be a string');

    console.log('✓ Widget execute test passed');
});

test('CaptchaWidget execute() in click popup mode keeps checkbox unchecked until user clicks', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha-anchor">
                <div class="private-captcha"
                     data-display-mode="popup"
                     data-start-mode="click"
                     data-finished-callback="testClickModeFinishedCallback">
                </div>
            </div>
        </form>
    `;

    let callbackCalled = false;
    global.window.testClickModeFinishedCallback = (widget) => {
        callbackCalled = true;
    };

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    assert.ok(element, 'Should find captcha element');

    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    let finishEventFired = false;
    element.addEventListener('privatecaptcha:finish', () => {
        finishEventFired = true;
    });

    const startedEvent = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Started event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:start', () => {
            clearTimeout(timeout);
            resolve();
        }, { once: true });
    });

    widget.execute();
    await startedEvent;

    await new Promise((resolve) => setTimeout(resolve, 700));

    const pcElement = element.querySelector('private-captcha');
    assert.ok(pcElement, 'Should find private-captcha element');
    const checkboxEl = pcElement.shadowRoot.querySelector('input[type="checkbox"]');
    assert.ok(checkboxEl, 'Should still show an unchecked checkbox');
    assert.strictEqual(checkboxEl.checked, false, 'Checkbox should remain unchecked');
    assert.strictEqual(finishEventFired, false, 'Finish event should not fire before user clicks');
    assert.strictEqual(callbackCalled, false, 'Finished callback should not run before user clicks');
    assert.strictEqual(widget.solution(), null, 'Solution should not be exposed before user clicks');

    const finishAfterClick = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Finish event timeout after user click'));
        }, 5000);

        element.addEventListener('privatecaptcha:finish', () => {
            clearTimeout(timeout);
            resolve();
        }, { once: true });
    });

    checkboxEl.checked = true;
    checkboxEl.dispatchEvent(new window.Event('change', { bubbles: true }));

    await finishAfterClick;

    assert.strictEqual(finishEventFired, true, 'Finish event should fire after user clicks');
    assert.strictEqual(callbackCalled, true, 'Finished callback should run after user clicks');
    assert.ok(widget.solution(), 'Solution should be exposed after user clicks');

    console.log('✓ Widget click popup execute test passed');
});

test('CaptchaWidget execute() during hidden click-mode loading starts and finishes solving', async (t) => {
    document.body.innerHTML = `
        <form>
            <button type="button" id="submit-button">Submit</button>
            <div class="private-captcha"
                 data-display-mode="hidden"
                 data-start-mode="click"
                 data-finished-callback="testHiddenLoadingFinishedCallback">
            </div>
        </form>
    `;

    let callbackCalled = false;
    global.window.testHiddenLoadingFinishedCallback = (widget) => {
        callbackCalled = true;
    };

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    assert.ok(element, 'Should find captcha element');

    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    const finishedEvent = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Finish event timeout after execute during loading'));
        }, 5000);

        element.addEventListener('privatecaptcha:finish', () => {
            clearTimeout(timeout);
            resolve();
        }, { once: true });
    });

    widget.onFocusIn({ target: document.getElementById('submit-button') });
    assert.strictEqual(widget._state, 'loading', 'Widget should be loading after focus starts init');

    widget.execute();

    await finishedEvent;

    assert.strictEqual(callbackCalled, true, 'Finished callback should run');
    assert.ok(widget.solution(), 'Solution should be exposed after execute finishes');

    console.log('✓ Widget hidden loading execute test passed');
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

    let eventFired = false;
    const initEvent = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:init', (event) => {
            clearTimeout(timeout);
            eventFired = true;
            assert.ok(event.detail.widget, 'Event should include widget in detail');
            assert.strictEqual(event.detail.element, element, 'Event should include element in detail');
            resolve();
        }, { once: true });
    });

    widget.init(false);

    await initEvent;

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

    let eventFired = false;
    const startedEvent = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:start', (event) => {
            clearTimeout(timeout);
            eventFired = true;
            assert.ok(event.detail.widget, 'Event should include widget in detail');
            assert.strictEqual(event.detail.element, element, 'Event should include element in detail');
            resolve();
        }, { once: true });
    });

    widget.execute();

    await startedEvent;

    assert.strictEqual(eventFired, true, 'privatecaptcha:start event should be fired');
    assert.strictEqual(callbackCalled, true, 'Started callback should be called');

    console.log('✓ Widget started test passed');
});

test('CaptchaWidget reset() fires reset event and callback', async (t) => {
    document.body.innerHTML = `
        <form>
            <div class="private-captcha"
                 data-reset-callback="testResetCallback">
            </div>
        </form>
    `;

    let callbackCalled = false;
    global.window.testResetCallback = (widget) => {
        callbackCalled = true;
    };

    const { CaptchaWidget } = await import('../js/widget.js');

    const element = document.querySelector('.private-captcha');
    assert.ok(element, 'Should find captcha element');

    const widget = new CaptchaWidget(element, {
        sitekey: testSitekey,
        debug: true
    });

    let eventFired = false;
    const resetEvent = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
            reject(new Error('Event timeout after 5000ms'));
        }, 5000);

        element.addEventListener('privatecaptcha:reset', (event) => {
            clearTimeout(timeout);
            eventFired = true;
            assert.ok(event.detail.widget, 'Event should include widget in detail');
            assert.strictEqual(event.detail.element, element, 'Event should include element in detail');
            resolve();
        }, { once: true });
    });

    widget.reset();

    await resetEvent;

    assert.strictEqual(eventFired, true, 'privatecaptcha:reset event should be fired');
    assert.strictEqual(callbackCalled, true, 'Reset callback should be called');

    console.log('✓ Widget reset event test passed');
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

test('CaptchaWidget reset() recalculates data attributes and auto defaults without losing caller options', async () => {
    document.body.innerHTML = `<form><div class="private-captcha" data-sitekey="${testSitekey}" data-theme="light" data-eu="true"></div></form>`;
    const { CaptchaWidget, RECAPTCHA_COMPAT } = await import('../js/widget.js');
    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, { compat: RECAPTCHA_COMPAT });

    assert.strictEqual(widget._options.fieldName, 'g-recaptcha-response');
    assert.ok(widget._options.puzzleEndpoint.includes('api.eu.'));

    element.dataset.sitekey = 'bbbbbbbbccccddddeeeeffffffffffff';
    element.dataset.theme = 'dark';
    element.dataset.solutionField = 'updated-solution';
    delete element.dataset.eu;
    widget.reset();

    assert.strictEqual(widget._options.sitekey, element.dataset.sitekey);
    assert.strictEqual(widget._options.theme, 'dark');
    assert.strictEqual(widget._options.fieldName, 'updated-solution');
    assert.strictEqual(widget._options.puzzleEndpoint, 'https://api.privatecaptcha.com/puzzle');

    widget.reset({ theme: 'light' });
    element.dataset.theme = 'dark';
    widget.reset();
    assert.strictEqual(widget._options.theme, 'light');
    assert.strictEqual(widget._options.sitekey, element.dataset.sitekey);
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

test('extended widget script preserves recaptcha compat mode', async () => {
    await import('../js/captcha.js');
    document.body.innerHTML = '<script src="https://cdn.example.com/widget/js/privatecaptcha-ext.js?compat=recaptcha&render=explicit"></script>';
    const previous = window.grecaptcha;
    window.grecaptcha = null;
    try {
        window.privateCaptcha.setup();
        assert.strictEqual(window.grecaptcha, window.privateCaptcha);
    } finally {
        window.grecaptcha = previous;
    }
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

test('Puzzle parses canonical v1 and v2 fixtures', async () => {
    const { Puzzle, CHALLENGE_BLAKE2B, CHALLENGE_ARGON2ID } = await import('../js/puzzle.js');
    const fixtures = [
        { fixture: protocolFixtures.v1, challenge: CHALLENGE_BLAKE2B, length: 47 },
        { fixture: protocolFixtures.v2, challenge: CHALLENGE_ARGON2ID, length: 48 },
    ];

    for (const { fixture, challenge, length } of fixtures) {
        const body = bytesFromHex(fixture.body);
        const puzzle = new Puzzle(puzzlePayload(body));

        assert.strictEqual(puzzle.version, fixture.version);
        assert.strictEqual(puzzle.challenge, challenge);
        assert.strictEqual(puzzle.puzzleBytes.length, length);
        assert.deepStrictEqual(Array.from(puzzle.puzzleBytes), Array.from(body));
        assert.strictEqual(hexFromBytes(puzzle.userData), fixture.userData);
        assert.strictEqual(puzzle.puzzleBuffer.length, 128);
    }
});

test('Puzzle rejects unknown and non-canonical records', async () => {
    const { Puzzle } = await import('../js/puzzle.js');
    const v1 = bytesFromHex(protocolFixtures.v1.body);
    const v2 = bytesFromHex(protocolFixtures.v2.body);
    const unknownVersion = v1.slice();
    unknownVersion[0] = 3;
    const unknownChallenge = v2.slice();
    unknownChallenge[1] = 255;

    const invalidBodies = [
        unknownVersion,
        unknownChallenge,
        v1.slice(0, -1),
        Uint8Array.from([...v1, 0]),
        v2.slice(0, -1),
        Uint8Array.from([...v2, 0]),
    ];

    for (const body of invalidBodies) {
        assert.throws(() => new Puzzle(puzzlePayload(body)));
    }
});

test('CaptchaWidget reports Argon2id provider initialization errors', async () => {
    const { CaptchaWidget } = await import('../js/widget.js');
    const { STATE_ERROR, STATE_LOADING } = await import('../js/html.js');
    document.body.innerHTML = '<form><div class="private-captcha"></div></form>';
    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, { sitekey: testSitekey });
    let errored = 0;
    let finished = 0;
    element.addEventListener('privatecaptcha:error', () => { errored++; });
    element.addEventListener('privatecaptcha:finish', () => { finished++; });
    widget._apiTriggered = true;
    widget.setState(STATE_LOADING);
    widget._solution = 'stale proof';
    element.insertAdjacentHTML('beforeend', '<input name="private-captcha-solution" type="hidden" value="stale proof">');
    widget.onWorkerError(new Error('Argon2id provider unavailable'));
    widget.onWorkerError(new Error('late worker failure'));
    assert.strictEqual(widget._state, STATE_ERROR);
    assert.strictEqual(element.querySelector('private-captcha')._state, STATE_ERROR);
    assert.strictEqual(errored, 1);
    assert.strictEqual(finished, 0);
    assert.strictEqual(widget.solution(), null);
    assert.strictEqual(element.querySelector('input[name="private-captcha-solution"]'), null);
    assert.ok(widget._errorCode > 0);
});

for (const mode of ['wasm', 'noble', 'blake']) {
    test(`CaptchaWidget ${mode} search failure after partial work emits only error and permits retry`, async () => {
        const { CaptchaWidget } = await import('../js/widget.js');
        const { WorkersPool } = await import('../js/workerspool.js');
        const { createWorkerSolver } = await import('../js/worker-solver.js');
        const { STATE_ERROR, STATE_VERIFIED } = await import('../js/html.js');
        const isBlake = mode === 'blake';
        const body = bytesFromHex(isBlake ? protocolFixtures.v1.body : protocolFixtures.v2.body);
        const solutionCountOffset = isBlake ? 26 : 27;
        body[solutionCountOffset] = 8;
        body.fill(0, solutionCountOffset + 1, solutionCountOffset + 5);
        const previousFetch = globalThis.fetch;
        globalThis.fetch = async () => ({
            ok: true, text: async () => puzzlePayload(body), headers: { get: () => null },
        });
        document.body.innerHTML = `<form><div class="private-captcha-anchor"><div class="private-captcha"
            data-display-mode="popup" data-finished-callback="testSearchFinished"
            data-errored-callback="testSearchErrored"></div></div></form>`;
        const element = document.querySelector('.private-captcha');
        let finished = 0;
        let errored = 0;
        let finishedCallbacks = 0;
        let errorCallbacks = 0;
        let submitted = 0;
        element.closest('form').addEventListener('submit', (event) => { event.preventDefault(); submitted++; });
        element.addEventListener('privatecaptcha:finish', () => { finished++; });
        element.addEventListener('privatecaptcha:error', () => { errored++; });
        window.testSearchFinished = () => {
            finishedCallbacks++;
            element.closest('form').dispatchEvent(new window.Event('submit', { cancelable: true }));
        };
        window.testSearchErrored = () => { errorCallbacks++; };
        const workers = [];
        let fail = true;
        class SearchWorker {
            constructor() {
                this.terminated = false;
                workers.push(this);
            }

            postMessage({ command, argument }) {
                if (command === 'init') {
                    this.puzzleID = argument.id;
                    const failSearch = (index) => {
                        if (fail && index === 1) { throw new Error(`${mode} search failed`); }
                        return Uint8Array.of(index, 0, 0, 0, 0, 0, 0, 1);
                    };
                    this.solver = createWorkerSolver(argument.challenge, argument.buffer, {
                        blake: async () => ({ wasm: true, solve: async (_threshold, index) => failSearch(index) }),
                        argon: async () => mode === 'wasm'
                            ? { wasm: true, solve: (_body, _threshold, index) => failSearch(index) }
                            : { wasm: false, hash(nonce, _body, output) {
                                if (fail && nonce[0] === 1) { throw new Error('noble search failed'); }
                                output.fill(0);
                            } },
                    });
                    void this.solver.then(() => this.onmessage?.({ data: { command: 'init' } }));
                } else if (command === 'solve') {
                    void this.solver.then((solver) => solver.solve(0xffffffff, argument.puzzleIndex))
                        .then((solution) => {
                            if (!this.terminated) {
                                this.onmessage?.({ data: { command: 'solve', argument: {
                                    id: this.puzzleID, solution, wasm: mode !== 'noble',
                                } } });
                            }
                        }, (error) => {
                            if (!this.terminated) { this.onmessage?.({ data: { command: 'error', error: error.message } }); }
                        });
                }
            }

            terminate() { this.terminated = true; }
        }

        try {
            const widget = new CaptchaWidget(element, { sitekey: testSitekey });
            widget._workersPool = new WorkersPool({
                workersReady: widget.onWorkersReady.bind(widget),
                workerError: widget.onWorkerError.bind(widget),
                workStarted: widget.onWorkStarted.bind(widget),
                workCompleted: widget.onWorkCompleted.bind(widget),
                progress: widget.onWorkProgress.bind(widget),
            }, false, SearchWorker);
            const errorEvent = new Promise((resolve, reject) => {
                const timeout = setTimeout(() => reject(new Error(`${mode} error event timed out`)), 1000);
                element.addEventListener('privatecaptcha:error', () => { clearTimeout(timeout); resolve(); }, { once: true });
            });
            widget.execute();
            await errorEvent;

            assert.strictEqual(widget._state, STATE_ERROR);
            assert.strictEqual(element.querySelector('private-captcha')._state, STATE_ERROR);
            assert.strictEqual(errored, 1);
            assert.strictEqual(errorCallbacks, 1);
            assert.strictEqual(widget.solution(), null);
            assert.strictEqual(element.querySelector('input[name="private-captcha-solution"]'), null);
            assert.ok(workers.every((worker) => worker.terminated));
            if (!isBlake) { assert.strictEqual(widget._workersPool._solutions.length, 1); }
            await new Promise((resolve) => setTimeout(resolve, 550));
            assert.strictEqual(finished, 0);
            assert.strictEqual(finishedCallbacks, 0);
            assert.strictEqual(submitted, 0);

            widget.reset();
            fail = false;
            const finishedEvent = new Promise((resolve, reject) => {
                const timeout = setTimeout(() => reject(new Error(`${mode} retry timed out`)), 2000);
                element.addEventListener('privatecaptcha:finish', () => { clearTimeout(timeout); resolve(); }, { once: true });
            });
            widget.execute();
            await finishedEvent;
            assert.strictEqual(widget._state, STATE_VERIFIED);
            assert.ok(widget.solution());
            assert.ok(element.querySelector('input[name="private-captcha-solution"]'));
            assert.strictEqual(errored, 1);
            assert.strictEqual(finished, 1);
            assert.strictEqual(finishedCallbacks, 1);
            assert.strictEqual(submitted, 1);
        } finally {
            globalThis.fetch = previousFetch;
        }
    });
}

test('CaptchaWidget handles synchronous solve dispatch failures without leaving a spinner', async () => {
    const { CaptchaWidget } = await import('../js/widget.js');
    const { WorkersPool } = await import('../js/workerspool.js');
    const { STATE_ERROR, STATE_READY } = await import('../js/html.js');
    document.body.innerHTML = '<form><div class="private-captcha"></div></form>';
    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, { sitekey: testSitekey });
    let errors = 0;
    let finished = 0;
    element.addEventListener('privatecaptcha:error', () => { errors++; });
    element.addEventListener('privatecaptcha:finish', () => { finished++; });
    const workers = [];
    class ThrowingWorker {
        constructor() { workers.push(this); this.terminated = false; }
        postMessage({ command }) { if (command === 'solve') { throw new Error('postMessage failed'); } }
        terminate() { this.terminated = true; }
    }
    widget._workersPool = new WorkersPool({ workerError: widget.onWorkerError.bind(widget) }, false, ThrowingWorker);
    const work = { ID: 42, challenge: 0, puzzleBuffer: new Uint8Array(128), solutionsCount: 8,
        difficulty: 136, isZero: () => false };
    widget._puzzle = work;
    widget._workersPool.init(work, false);
    widget.setState(STATE_READY);
    widget.execute();

    assert.strictEqual(errors, 1);
    assert.strictEqual(finished, 0);
    assert.strictEqual(widget._state, STATE_ERROR);
    assert.strictEqual(element.querySelector('private-captcha')._state, STATE_ERROR);
    assert.strictEqual(widget.solution(), null);
    assert.ok(workers.every((worker) => worker.terminated));
});

test('CaptchaWidget handles synchronous worker initialization failures as worker errors', async () => {
    const { CaptchaWidget } = await import('../js/widget.js');
    const { WorkersPool } = await import('../js/workerspool.js');
    const { ERROR_SOLVE_PUZZLE } = await import('../js/errors.js');
    const { STATE_ERROR } = await import('../js/html.js');
    const body = bytesFromHex(protocolFixtures.v2.body);
    body[27] = 8;
    body.fill(0, 28, 32);
    const previousFetch = globalThis.fetch;
    globalThis.fetch = async () => ({
        ok: true, text: async () => puzzlePayload(body), headers: { get: () => null },
    });
    document.body.innerHTML = '<form><div class="private-captcha"></div></form>';
    const element = document.querySelector('.private-captcha');
    const widget = new CaptchaWidget(element, { sitekey: testSitekey });
    let errors = 0;
    let finished = 0;
    element.addEventListener('privatecaptcha:error', () => { errors++; });
    element.addEventListener('privatecaptcha:finish', () => { finished++; });
    const workers = [];
    class ThrowingWorker {
        constructor() { workers.push(this); this.terminated = false; }
        postMessage() { throw new Error('worker initialization failed'); }
        terminate() { this.terminated = true; }
    }
    widget._workersPool = new WorkersPool({ workerError: widget.onWorkerError.bind(widget) }, false, ThrowingWorker);
    try {
        await widget.init(true);
        assert.strictEqual(errors, 1);
        assert.strictEqual(finished, 0);
        assert.strictEqual(widget._state, STATE_ERROR);
        assert.strictEqual(element.querySelector('private-captcha')._state, STATE_ERROR);
        assert.strictEqual(widget._errorCode, ERROR_SOLVE_PUZZLE);
        assert.strictEqual(widget.solution(), null);
        assert.strictEqual(element.querySelector('input[name="private-captcha-solution"]'), null);
        assert.ok(workers.every((worker) => worker.terminated));
    } finally {
        globalThis.fetch = previousFetch;
    }
});

test('getPuzzle preserves the challenge query when adding the sitekey', async () => {
    const { getPuzzle } = await import('../js/puzzle.js');
    const previousFetch = globalThis.fetch;
    let requestedURL;
    globalThis.fetch = async (url) => {
        requestedURL = url;
        return { ok: true, text: async () => 'puzzle.signature', headers: { get: () => null } };
    };
    try {
        await getPuzzle('/puzzle/152?challenge=blake2b', testSitekey, { attempts: 1 });
        assert.strictEqual(requestedURL, `/puzzle/152?challenge=blake2b&sitekey=${testSitekey}`);
    } finally {
        globalThis.fetch = previousFetch;
    }
});

test('getPuzzle per-call timeout triggers with 1 attempt', async (t) => {
    const http = await import('node:http');
    const { getPuzzle } = await import('../js/puzzle.js');

    // Create a slow server that takes 500ms to respond
    const server = http.createServer((req, res) => {
        setTimeout(() => {
            res.writeHead(200, { 'Content-Type': 'text/plain' });
            res.end('slow response');
        }, 500);
    });

    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const port = server.address().port;

    const originalFetch = globalThis.fetch;

    globalThis.fetch = async (url, options = {}) => {
        return new Promise((resolve, reject) => {
            const parsedUrl = new URL(url);
            const req = http.request({
                hostname: parsedUrl.hostname,
                port: parsedUrl.port,
                path: parsedUrl.pathname + parsedUrl.search,
                method: options.method || 'GET',
                headers: options.headers ? Object.fromEntries(options.headers) : {}
            }, (res) => {
                let data = '';
                res.on('data', (chunk) => data += chunk);
                res.on('end', () => {
                    resolve({
                        ok: res.statusCode >= 200 && res.statusCode < 300,
                        status: res.statusCode,
                        text: async () => data,
                        json: async () => JSON.parse(data)
                    });
                });
            });

            if (options.signal) {
                options.signal.addEventListener('abort', () => {
                    req.destroy();
                    reject(new Error('Aborted'));
                });
            }

            req.on('error', reject);
            req.end();
        });
    };

    try {
        const startTime = Date.now();
        await assert.rejects(
            async () => {
                // 1 attempt, per-call timeout 100ms, global timeout 5000ms
                await getPuzzle(`http://127.0.0.1:${port}/puzzle`, testSitekey, {
                    attempts: 1,
                    timeout: 100,
                    globalTimeout: 5000
                });
            },
            (err) => {
                // With 1 attempt and per-call timeout, it should fail after max retry attempts
                assert.ok(err.message.includes('maximum retry attempts') || err.message.includes('timed out'),
                    `Error message should indicate retry failure or timeout, got: ${err.message}`);
                return true;
            }
        );
        const elapsed = Date.now() - startTime;
        // Should timeout quickly (per-call timeout of 100ms + small overhead)
        assert.ok(elapsed < 500, `Should timeout quickly (took ${elapsed}ms)`);
    } finally {
        globalThis.fetch = originalFetch;
        server.close();
    }

    console.log('✓ getPuzzle per-call timeout test passed');
});

test('getPuzzle global timeout triggers with 2 attempts', async (t) => {
    const http = await import('node:http');
    const { getPuzzle } = await import('../js/puzzle.js');

    // Create a server that always returns 500 to trigger retry
    const server = http.createServer((req, res) => {
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end('{"error": "server error"}');
    });

    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const port = server.address().port;

    try {
        const startTime = Date.now();
        await assert.rejects(
            async () => {
                // 2 attempts, per-call timeout 5000ms, global timeout 200ms
                // Global timeout should trigger during wait between attempts
                await getPuzzle(`http://127.0.0.1:${port}/puzzle`, testSitekey, {
                    attempts: 2,
                    timeout: 5000,
                    globalTimeout: 200
                });
            },
            (err) => {
                // Global timeout should abort with timeout error
                assert.ok(err.message.includes('timed out') || err.message.includes('Fetch timed out'),
                    `Error message should indicate timeout, got: ${err.message}`);
                return true;
            }
        );
        const elapsed = Date.now() - startTime;
        // Should timeout around 200ms (global timeout) with some overhead allowance
        assert.ok(elapsed < 500, `Should timeout at global timeout (took ${elapsed}ms)`);
    } finally {
        server.close();
    }

    console.log('✓ getPuzzle global timeout test passed');
});

test('JS fallback finds a verifiable solution', async () => {
    const { findBlake2bSolution, thresholdFromDifficulty, readUInt32LE } = await import('../js/puzzle.utils.js');
    const puzzleBuffer = new Uint8Array(128);
    globalThis.crypto.getRandomValues(puzzleBuffer);
    const threshold = thresholdFromDifficulty(48);
    const solution = findBlake2bSolution(puzzleBuffer, threshold, 3, false, fallbackHasher);
    assert.strictEqual(solution.length, 8);
    assert.strictEqual(solution[0], 3);
    assert.ok(readUInt32LE(blake2b(puzzleBuffer, null, 32), 0) <= threshold);
});

test('scalar WASM hashes fixed 128-byte puzzles as BLAKE2b-256', async () => {
    const scalarSolverWasm = await readFile(new URL('../wasm/blake2b-solver-scalar.wasm', import.meta.url));
    assert.deepStrictEqual(WebAssembly.Module.imports(new WebAssembly.Module(scalarSolverWasm)), []);
    const { instance } = await WebAssembly.instantiate(scalarSolverWasm);
    const { memory, puzzle_ptr: puzzlePtr, digest_ptr: digestPtr, hash_puzzle: hashPuzzle } = instance.exports;
    const wasmMemory = new Uint8Array(memory.buffer);
    const puzzleOffset = puzzlePtr();
    const digestOffset = digestPtr();

    const puzzles = [
        new Uint8Array(128),
        new Uint8Array(128).fill(0xff),
        Uint8Array.from({ length: 128 }, (_, index) => index),
    ];

    let state = 0x6d2b79f5;
    for (let puzzleIndex = 0; puzzleIndex < 8; puzzleIndex++) {
        const puzzle = new Uint8Array(128);
        for (let byteIndex = 0; byteIndex < puzzle.length; byteIndex++) {
            state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
            puzzle[byteIndex] = state >>> 24;
        }
        puzzles.push(puzzle);
    }

    for (const puzzle of puzzles) {
        wasmMemory.set(puzzle, puzzleOffset);
        hashPuzzle();

        const expected = blake2b(puzzle, null, 32);
        const actual = wasmMemory.slice(digestOffset, digestOffset + 32);
        assert.deepStrictEqual(actual, expected);
    }
});

test('scalar WASM batch returns server-compatible solutions across nonce carries', async () => {
    const { readUInt32LE } = await import('../js/puzzle.utils.js');
    const wasm = await readFile(new URL('../wasm/blake2b-solver-scalar.wasm', import.meta.url));
    const { instance } = await WebAssembly.instantiate(wasm);
    const { memory, puzzle_ptr: puzzlePtr, prepare_puzzle: preparePuzzle,
        solve_batch: solveBatch, get_solution_nonce: solutionNonce } = instance.exports;
    assert.strictEqual(typeof preparePuzzle, 'function');
    assert.strictEqual(typeof solveBatch, 'function');

    const buffer = Uint8Array.from({ length: 128 }, (_, index) => index);
    new Uint8Array(memory.buffer).set(buffer, puzzlePtr());
    preparePuzzle();

    for (const { puzzleIndex, nonceStart, count } of [
        { puzzleIndex: 0, nonceStart: 0, count: 3 },
        { puzzleIndex: 17, nonceStart: 254, count: 4 },
        { puzzleIndex: 255, nonceStart: 65534, count: 4 },
        { puzzleIndex: 99, nonceStart: 0xfffffffe, count: 2 },
    ]) {
        const prefixes = [];
        for (let offset = 0; offset < count; offset++) {
            const nonce = nonceStart + offset;
            buffer[120] = puzzleIndex;
            buffer[124] = nonce >>> 24;
            buffer[125] = nonce >>> 16;
            buffer[126] = nonce >>> 8;
            buffer[127] = nonce;
            prefixes.push(readUInt32LE(blake2b(buffer, null, 32), 0));
        }
        const threshold = Math.min(...prefixes);
        const expectedOffset = prefixes.findIndex(prefix => prefix <= threshold);

        assert.strictEqual(solveBatch(puzzleIndex, threshold, nonceStart, 0), 0);
        assert.strictEqual(solveBatch(puzzleIndex, threshold, nonceStart, expectedOffset), 0);
        assert.strictEqual(solveBatch(puzzleIndex, threshold, nonceStart, count), 1);
        assert.strictEqual(solutionNonce() >>> 0, nonceStart + expectedOffset);

        const solution = buffer.slice(120);
        const nonce = solutionNonce() >>> 0;
        solution[0] = puzzleIndex;
        solution[4] = nonce >>> 24;
        solution[5] = nonce >>> 16;
        solution[6] = nonce >>> 8;
        solution[7] = nonce;
        buffer.set(solution, 120);
        const prefix = readUInt32LE(blake2b(buffer, null, 32), 0);
        assert.ok(prefix <= threshold);
        assert.deepStrictEqual(solution.slice(1, 4), Uint8Array.of(121, 122, 123));
    }
});

test('worker solver falls back to JS when scalar WASM fails while preserving solution bytes', async () => {
    const { createSolver } = await import('../js/blake2-solver.js');
    const { readUInt32LE } = await import('../js/puzzle.utils.js');
    const puzzle = Uint8Array.from({ length: 128 }, (_, index) => index * 13 & 255);
    const threshold = 0x7fffffff;
    const index = 23;
    let failedInstantiations = 0;
    const rejectedWASM = {
        instantiate: async () => {
            failedInstantiations++;
            throw new Error('CSP blocked WASM');
        },
    };

    for (const [runtime, expected] of [
        [{ instantiate: WebAssembly.instantiate }, 'scalar'],
        [rejectedWASM, 'js'],
        [null, 'js'],
    ]) {
        const solver = await createSolver(puzzle.slice(), runtime);
        assert.strictEqual(solver.kind, expected);
        assert.strictEqual(solver.wasm, expected !== 'js');
        const solution = await solver.solve(threshold, index, false);
        assert.strictEqual(solution.length, 8);
        assert.strictEqual(solution[0], index);
        assert.deepStrictEqual(solution.slice(1, 4), puzzle.slice(121, 124));
        const verified = puzzle.slice();
        verified.set(solution, 120);
        const prefix = readUInt32LE(blake2b(verified, null, 32), 0);
        assert.ok(prefix <= threshold);
    }
    assert.strictEqual(failedInstantiations, 1);
});
