import test from 'node:test';
import assert from 'node:assert/strict';
import { encode } from 'base64-arraybuffer';

async function createDetectionWidget(t, options = {}, automation = true) {
    const { CaptchaWidget } = await import('../js/widget.js');
    document.body.innerHTML = '<form><div class="private-captcha" data-custom-detection-callback="detectCustom"></div></form>';
    const element = document.querySelector('.private-captcha');
    const hints = [];
    const body = new Uint8Array(47);
    body[0] = 1;
    body[26] = 1;
    t.mock.getter(globalThis, 'navigator', () => ({ webdriver: automation }));
    t.mock.method(globalThis, 'fetch', async (_url, options) => {
        hints.push(new Headers(options.headers).get('X-PC-Client-Hints'));
        return { ok: true, text: async () => `${encode(body.buffer)}.signature`, headers: new Headers() };
    });
    const widget = new CaptchaWidget(element, { sitekey: 'test', startMode: 'click', ...options });
    t.mock.method(widget._workersPool, 'init', () => widget.onWorkersReady(false));
    return { widget, hints, element };
}

test('custom detection appends only the custom token for truthy sync and async results', async (t) => {
    for (const value of [true, 'detected', 1, {}, false, '', 0, null, undefined]) {
        for (const asyncResult of [false, true]) {
            await t.test(`${JSON.stringify(value)} async=${asyncResult}`, async (t) => {
                let receivedWidget;
                const { widget, hints } = await createDetectionWidget(t, {
                    customDetectionCallback: (instance) => {
                        receivedWidget = instance;
                        return asyncResult ? Promise.resolve(value) : value;
                    },
                });
                await widget.init(false);
                assert.ok(receivedWidget === widget, 'callback receives the widget');
                assert.deepEqual(hints, [value ? 'automation,custom' : 'automation']);
                assert.equal(widget._state, 'ready');
            });
        }
    }
});

test('custom detection resolves the HTML callback and prefers a direct function', async (t) => {
    window.detectCustom = () => true;
    t.after(() => { delete window.detectCustom; });
    const { widget, hints } = await createDetectionWidget(t);
    await widget.init(false);
    widget.reset({ customDetectionCallback: () => false });
    await widget.init(false);
    assert.deepEqual(hints, ['automation,custom', 'automation']);
});

test('custom detection failures do not fail puzzle loading', async (t) => {
    for (const [name, callback] of [
        ['throw', () => { throw new Error('detector failed'); }],
        ['reject', () => Promise.reject(new Error('detector rejected'))],
        ['invalid callback', 42],
        ['missing global', 'missingDetector'],
        ['throwing then getter', () => ({ get then() { throw new Error('invalid thenable'); } })],
    ]) {
        await t.test(name, async (t) => {
            t.mock.method(console, 'warn', () => {});
            const { widget, hints } = await createDetectionWidget(t, { customDetectionCallback: callback });
            let errors = 0;
            widget.element().addEventListener('privatecaptcha:error', () => { errors++; });
            await widget.init(false);
            assert.deepEqual(hints, ['automation']);
            assert.equal(widget._state, 'ready');
            assert.equal(errors, 0);
        });
    }
});

test('custom detection tolerates a throwing global callback getter', async (t) => {
    Object.defineProperty(window, 'detectCustom', {
        configurable: true,
        get() { throw new Error('callback unavailable'); },
    });
    t.after(() => { delete window.detectCustom; });
    t.mock.method(console, 'warn', () => {});
    const { widget, hints } = await createDetectionWidget(t);
    await widget.init(false);
    assert.deepEqual(hints, ['automation']);
    assert.equal(widget._state, 'ready');
});

for (const rejectLate of [false, true]) {
    test(`custom detection times out at 500ms and ignores a late ${rejectLate ? 'rejection' : 'result'}`, async (t) => {
        t.mock.timers.enable({ apis: ['setTimeout'] });
        t.mock.method(console, 'warn', () => {});
        let finish;
        let started;
        const detectionStarted = new Promise((resolve) => { started = resolve; });
        const { widget, hints } = await createDetectionWidget(t, {
            customDetectionCallback: () => new Promise((resolve, reject) => {
                finish = rejectLate ? reject : resolve;
                started();
            }),
        });
        const loading = widget.init(false);
        await detectionStarted;
        t.mock.timers.tick(499);
        assert.deepEqual(hints, [], 'request waits for detection');
        t.mock.timers.tick(1);
        await loading;
        assert.deepEqual(hints, ['automation']);
        assert.equal(widget._state, 'ready');
        finish(rejectLate ? new Error('late rejection') : true);
        await new Promise((resolve) => setImmediate(resolve));
        assert.deepEqual(hints, ['automation'], 'late result cannot change or repeat the request');
    });
}

test('custom detection awaits an async result and sends custom without automation', async (t) => {
    let finish;
    let started;
    const detectionStarted = new Promise((resolve) => { started = resolve; });
    const { widget, hints } = await createDetectionWidget(t, {
        customDetectionCallback: () => new Promise((resolve) => { finish = resolve; started(); }),
    }, false);
    const loading = widget.init(false);
    await detectionStarted;
    assert.deepEqual(hints, []);
    finish(true);
    await loading;
    assert.deepEqual(hints, ['custom']);
});

for (const reinit of [false, true]) {
    test(`custom detection discards a stale result after reset, reinit=${reinit}`, async (t) => {
        let finish;
        let started;
        const detectionStarted = new Promise((resolve) => { started = resolve; });
        const { widget, hints } = await createDetectionWidget(t, {
            customDetectionCallback: () => new Promise((resolve) => { finish = resolve; started(); }),
        });
        const loading = widget.init(false);
        await detectionStarted;
        widget.reset({ customDetectionCallback: () => false });
        if (reinit) { await widget.init(false); }
        finish(true);
        await loading;
        assert.deepEqual(hints, reinit ? ['automation'] : []);
        assert.equal(widget._state, reinit ? 'ready' : 'empty');
    });
}

test('custom detection runs once per load and keeps the same hint on HTTP retries', async (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    t.mock.method(console, 'warn', () => {});
    let detections = 0;
    const { widget, hints } = await createDetectionWidget(t, { customDetectionCallback: () => ++detections === 1 });
    let firstAttempt;
    const attempted = new Promise((resolve) => { firstAttempt = resolve; });
    globalThis.fetch.mock.mockImplementationOnce(async (_url, options) => {
        hints.push(new Headers(options.headers).get('X-PC-Client-Hints'));
        firstAttempt();
        return { ok: false, status: 503 };
    });
    const loading = widget.init(false);
    await attempted;
    await Promise.resolve();
    t.mock.timers.tick(1600);
    await loading;
    assert.equal(detections, 1);
    assert.deepEqual(hints, ['automation,custom', 'automation,custom']);
    widget.expire();
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(detections, 2);
    assert.deepEqual(hints, ['automation,custom', 'automation,custom', 'automation']);
});
