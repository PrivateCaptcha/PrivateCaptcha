import test from 'node:test';
import assert from 'node:assert/strict';
import { encode } from 'base64-arraybuffer';

async function createActivationWidget(t, options = {}, trustedEvents = true) {
    const { CaptchaWidget } = await import('../js/widget.js');
    const { WorkersPool } = await import('../js/workerspool.js');
    if (trustedEvents) {
        Object.defineProperty(window.Event.prototype, 'isTrusted', {
            configurable: true,
            get: () => false,
        });
        t.after(() => { delete window.Event.prototype.isTrusted; });
    }
    t.mock.getter(globalThis, 'navigator', () => ({ webdriver: false }));
    document.body.innerHTML = '<form><input id="field"><div class="private-captcha"></div></form>';
    const element = document.querySelector('.private-captcha');
    const hints = [];
    const body = new Uint8Array(47);
    body[0] = 1;
    body[26] = 1;
    t.mock.method(globalThis, 'fetch', async (_url, options) => {
        hints.push(new Headers(options.headers).get('X-PC-Client-Hints'));
        return { ok: true, text: async () => `${encode(body.buffer)}.signature`, headers: new Headers() };
    });
    t.mock.method(WorkersPool.prototype, 'init', () => {});
    const initialized = new Promise((resolve) => element.addEventListener('privatecaptcha:init', resolve, { once: true }));
    const widget = new CaptchaWidget(element, { sitekey: 'test', startMode: 'click', ...options });
    const captcha = element.querySelector('private-captcha');
    return { widget, hints, element, captcha, body, field: document.querySelector('#field'), initialized };
}

function activationEvent(type, trusted) {
    const event = new window.Event(type, { bubbles: true, composed: true });
    if (typeof trusted === 'boolean') {
        Object.defineProperty(event, 'isTrusted', { value: trusted });
    }
    return event;
}

test('activation hints flag only explicitly untrusted focus in visible widget modes', async (t) => {
    for (const displayMode of ['widget', 'popup', 'hidden']) {
        for (const trusted of [true, false, undefined]) {
            await t.test(`${displayMode} trusted=${trusted}`, async (t) => {
                const { field, hints, initialized } = await createActivationWidget(t, { displayMode }, trusted !== undefined);
                field.dispatchEvent(activationEvent('focusin', trusted));
                await initialized;
                assert.deepEqual(hints, [displayMode !== 'hidden' && trusted === false ? 'automation' : '']);
            });
        }
    }
});

test('activation hints use the original checkbox event rather than the CustomEvent trust', async (t) => {
    for (const trusted of [true, false, undefined]) {
        await t.test(`trusted=${trusted}`, async (t) => {
            const { captcha, hints, initialized } = await createActivationWidget(t, {}, trusted !== undefined);
            const checkbox = captcha.shadowRoot.querySelector('input');
            checkbox.checked = true;
            checkbox.dispatchEvent(activationEvent('change', trusted));
            await initialized;
            assert.deepEqual(hints, [trusted === false ? 'automation' : '']);
        });
    }
});

test('activation hints retain click trust when the subsequent change event is trusted', async (t) => {
    for (const trusted of [true, false]) {
        await t.test(`click trusted=${trusted}`, async (t) => {
            const { captcha, hints, initialized } = await createActivationWidget(t);
            const checkbox = captcha.shadowRoot.querySelector('input');
            checkbox.dispatchEvent(activationEvent('click', trusted));
            checkbox.checked = true;
            checkbox.dispatchEvent(activationEvent('change', true));
            await initialized;
            assert.deepEqual(hints, [trusted ? '' : 'automation']);
        });
    }
});

test('activation hints do not accept forged trust in a checked CustomEvent detail', async (t) => {
    const { element, hints, initialized } = await createActivationWidget(t);
    element.dispatchEvent(new window.CustomEvent('privatecaptcha:checked', {
        detail: { isTrusted: true, originalEvent: { isTrusted: true } },
    }));
    await initialized;
    assert.deepEqual(hints, ['automation']);
});

test('activation hints ignore focus on the captcha itself and focus after loading starts', async (t) => {
    const { widget, captcha, field, hints } = await createActivationWidget(t);
    captcha.dispatchEvent(activationEvent('focusin', false));
    assert.deepEqual(hints, []);
    const loading = widget.init(false);
    field.dispatchEvent(activationEvent('focusin', false));
    await loading;
    assert.deepEqual(hints, ['']);
});

test('activation hints allow visible programmatic execute and load mode', async (t) => {
    for (const options of [{}, { startMode: 'load' }]) {
        await t.test(JSON.stringify(options), async (t) => {
            const { widget, hints, initialized } = await createActivationWidget(t, options);
            if (options.startMode !== 'load') { widget.execute(); }
            await initialized;
            assert.deepEqual(hints, ['']);
        });
    }
});

test('activation hints survive public boolean tampering and reset without leaking to another widget', async (t) => {
    const { widget, field, hints, initialized } = await createActivationWidget(t, {
        customDetectionCallback: (instance) => {
            for (const key of Object.keys(instance)) {
                if (typeof instance[key] === 'boolean') { instance[key] = false; }
            }
            return true;
        },
    });
    field.dispatchEvent(activationEvent('focusin', false));
    await initialized;
    widget.reset({ customDetectionCallback: null });
    await widget.init(false);
    assert.deepEqual(hints, ['automation,custom', 'automation']);

    const { CaptchaWidget } = await import('../js/widget.js');
    const otherElement = document.createElement('div');
    widget.element().parentElement.appendChild(otherElement);
    const other = new CaptchaWidget(otherElement, { sitekey: 'test', startMode: 'click' });
    await other.init(false);
    assert.deepEqual(hints, ['automation,custom', 'automation', '']);
});

test('activation hints detect hidden documents for every activation path and display mode', async (t) => {
    for (const displayMode of ['widget', 'popup', 'hidden']) {
        for (const activation of ['focus', 'checkbox', 'execute', 'init', 'load']) {
            await t.test(`${displayMode} ${activation}`, async (t) => {
                t.mock.getter(document, 'visibilityState', () => 'hidden');
                const { widget, field, captcha, hints, initialized } = await createActivationWidget(t, {
                    displayMode,
                    startMode: activation === 'load' ? 'load' : 'click',
                }, false);
                switch (activation) {
                    case 'focus':
                        field.dispatchEvent(activationEvent('focusin'));
                        break;
                    case 'checkbox':
                        const checkbox = captcha.shadowRoot.querySelector('input');
                        checkbox.checked = true;
                        checkbox.dispatchEvent(activationEvent('change'));
                        break;
                    case 'execute':
                        widget.execute();
                        break;
                    case 'init':
                        await widget.init(false);
                        break;
                }
                await initialized;
                assert.deepEqual(hints, ['automation']);
            });
        }
    }
});

test('activation hints sample visibility at activation rather than widget construction', async (t) => {
    for (const initial of ['hidden', 'visible']) {
        await t.test(initial, async (t) => {
            let visibility = initial;
            t.mock.getter(document, 'visibilityState', () => visibility);
            const { widget, hints } = await createActivationWidget(t);
            visibility = initial === 'hidden' ? 'visible' : 'hidden';
            await widget.init(false);
            assert.deepEqual(hints, [visibility === 'hidden' ? 'automation' : '']);
        });
    }
});

test('activation hints preserve activation visibility across async custom detection', async (t) => {
    for (const initial of ['hidden', 'visible', undefined]) {
        await t.test(`initial=${initial}`, async (t) => {
            let visibility = initial;
            t.mock.getter(document, 'visibilityState', () => visibility);
            const { widget, hints } = await createActivationWidget(t, {
                customDetectionCallback: async (instance) => {
                    visibility = initial === 'hidden' ? 'visible' : 'hidden';
                    for (const key of Object.keys(instance)) {
                        if (typeof instance[key] === 'boolean') { instance[key] = false; }
                    }
                    await Promise.resolve();
                    return false;
                },
            });
            await widget.init(false);
            assert.deepEqual(hints, [initial === 'hidden' ? 'automation' : '']);
        });
    }
});

test('activation hints retain hidden activation evidence across reset only for that widget', async (t) => {
    let visibility = 'hidden';
    t.mock.getter(document, 'visibilityState', () => visibility);
    const { widget, hints } = await createActivationWidget(t);
    await widget.init(false);
    visibility = 'visible';
    widget.reset();
    await widget.init(false);
    assert.deepEqual(hints, ['automation', 'automation']);

    const { CaptchaWidget } = await import('../js/widget.js');
    const otherElement = document.createElement('div');
    widget.element().parentElement.appendChild(otherElement);
    const other = new CaptchaWidget(otherElement, { sitekey: 'test', startMode: 'click' });
    await other.init(false);
    assert.deepEqual(hints, ['automation', 'automation', '']);
});

test('activation hints preserve the original visibility when the expiry timer refreshes', async (t) => {
    for (const startMode of ['auto', 'click', 'load']) {
        for (const initial of ['visible', 'hidden']) {
            await t.test(`${startMode} ${initial}`, async (t) => {
                t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now: 1800000000000 });
                let visibility = initial;
                t.mock.getter(document, 'visibilityState', () => visibility);
                const { widget, hints, body, initialized } = await createActivationWidget(t);
                new DataView(body.buffer).setUint32(27, Date.now() / 1000 + 20, true);
                widget.reset({ startMode });
                if (startMode !== 'load') { await widget.init(false); }
                await initialized;
                const expected = initial === 'hidden' ? 'automation' : '';
                assert.deepEqual(hints, [expected]);

                let resets = 0;
                widget.element().addEventListener('privatecaptcha:reset', () => { resets++; });
                visibility = initial === 'visible' ? 'hidden' : 'visible';
                t.mock.timers.tick(15000);
                await new Promise((resolve) => setImmediate(resolve));
                assert.deepEqual(hints, [expected, expected]);
                assert.equal(resets, 1);
            });
        }
    }
});

test('activation hints sample visibility again on explicit reset and error retry', async (t) => {
    for (const retry of ['reset', 'error']) {
        await t.test(retry, async (t) => {
            let visibility = 'visible';
            t.mock.getter(document, 'visibilityState', () => visibility);
            const { widget, hints } = await createActivationWidget(t);
            await widget.init(false);
            visibility = 'hidden';
            if (retry === 'reset') {
                widget.reset();
            } else {
                t.mock.method(console, 'error', () => {});
                widget.onWorkerError(new Error('retry'));
            }
            await widget.init(false);
            assert.deepEqual(hints, ['', 'automation']);
        });
    }
});

test('activation hints check hidden visibility when expire is called before the first activation', async (t) => {
    t.mock.getter(document, 'visibilityState', () => 'hidden');
    const { widget, hints, initialized } = await createActivationWidget(t);
    widget.expire();
    await initialized;
    assert.deepEqual(hints, ['automation']);
});

test('activation hints use configured eligibility despite public options tampering', async (t) => {
    for (const displayMode of ['widget', 'popup']) {
        for (const replace of [false, true]) {
            await t.test(`${displayMode} replace=${replace}`, async (t) => {
                const { widget, field, hints, initialized } = await createActivationWidget(t, { displayMode });
                if (replace) {
                    widget._options = { ...widget._options, displayMode: 'hidden' };
                } else {
                    widget._options.displayMode = 'hidden';
                }
                field.dispatchEvent(activationEvent('focusin', false));
                await initialized;
                assert.deepEqual(hints, ['automation']);
            });
        }
    }
});

test('activation hints follow supported display-mode changes on reset', async (t) => {
    for (const [initial, updated] of [['widget', 'hidden'], ['hidden', 'widget']]) {
        await t.test(`${initial} to ${updated}`, async (t) => {
            const { widget, field, hints, initialized } = await createActivationWidget(t, { displayMode: initial });
            widget.reset({ displayMode: updated });
            field.dispatchEvent(activationEvent('focusin', false));
            await initialized;
            assert.deepEqual(hints, [updated === 'hidden' ? '' : 'automation']);
        });
    }
});

test('fabricated objects cannot activate through public handler properties', async (t) => {
    for (const name of ['onFocusIn', 'onChecked', 'onCheckboxClicked']) {
        await t.test(name, async (t) => {
            const { widget, captcha, field, hints, initialized } = await createActivationWidget(t);
            const target = name === 'onCheckboxClicked' ? captcha : widget;
            if (typeof target[name] === 'function') {
                target[name]({
                    isTrusted: true,
                    target: name === 'onCheckboxClicked' ? { checked: true } : field,
                    preventDefault() {},
                    stopPropagation() {},
                });
            }
            await new Promise((resolve) => setImmediate(resolve));
            assert.deepEqual(hints, [], 'fabricated handler input must not load a puzzle');

            field.dispatchEvent(activationEvent('focusin', false));
            await initialized;
            assert.deepEqual(hints, ['automation']);
        });
    }
});

test('replacing the public checkbox handler cannot bypass event detection', async (t) => {
    const { widget, captcha, hints, initialized } = await createActivationWidget(t);
    captcha.onCheckboxClicked = () => widget.execute();
    const checkbox = captcha.shadowRoot.querySelector('input');
    checkbox.checked = true;
    checkbox.dispatchEvent(activationEvent('change', false));
    await initialized;
    assert.deepEqual(hints, ['automation']);
});
