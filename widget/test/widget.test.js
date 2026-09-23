import test from 'node:test';
import assert from 'node:assert';
import { readFile } from 'node:fs/promises';
import { Window } from 'happy-dom';
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
    const { findSolution, thresholdFromDifficulty, readUInt32LE } = await import('../js/puzzle.utils.js');
    const puzzleBuffer = new Uint8Array(128);
    globalThis.crypto.getRandomValues(puzzleBuffer);
    const threshold = thresholdFromDifficulty(48);
    const solution = findSolution(puzzleBuffer, threshold, 3, false, fallbackHasher);
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
