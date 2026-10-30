import test from 'node:test';
import assert from 'node:assert';
import { getPuzzle } from '../js/puzzle.js';

for (const { navigator, expected } of [
    { navigator: { userAgent: 'Chrome/132.0.0.0', webdriver: false }, expected: '' },
    { navigator: { userAgent: 'Chrome/132.0.0.0', webdriver: true }, expected: 'automation' },
    { navigator: { userAgent: 'HeadlessChrome/132.0.0.0', webdriver: true }, expected: 'automation' },
]) {
    test(`getPuzzle sends client hints ${JSON.stringify(expected)} for ${JSON.stringify(navigator)}`, async (t) => {
        t.mock.getter(globalThis, 'navigator', () => navigator);
        let headers;
        t.mock.method(globalThis, 'fetch', async (_url, options) => {
            headers = new Headers(options.headers);
            return { ok: true, text: async () => 'puzzle.signature', headers: new Headers() };
        });

        const result = await getPuzzle('/puzzle', 'sitekey', { attempts: 1 });
        assert.strictEqual(headers.get('X-PC-Client-Hints'), expected);
        assert.strictEqual(headers.get('X-PC-Captcha-Version'), '1');
        assert.deepStrictEqual(result, { data: 'puzzle.signature', notice: null });
    });
}

test('getPuzzle sends an empty hints header when browser detection throws', async (t) => {
    t.mock.getter(globalThis, 'navigator', () => { throw new Error('navigator unavailable'); });
    let headers;
    t.mock.method(globalThis, 'fetch', async (_url, options) => {
        headers = new Headers(options.headers);
        return { ok: true, text: async () => 'puzzle.signature', headers: new Headers() };
    });

    assert.deepStrictEqual(await getPuzzle('/puzzle', 'sitekey', { attempts: 1 }), {
        data: 'puzzle.signature', notice: null,
    });
    assert.strictEqual(headers.get('X-PC-Client-Hints'), '');
});

test('getPuzzle preserves the client hints header on retries', async (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    t.mock.getter(globalThis, 'navigator', () => ({ webdriver: true }));
    t.mock.method(console, 'warn', () => {});
    const hints = [];
    t.mock.method(globalThis, 'fetch', async (_url, options) => {
        hints.push(new Headers(options.headers).get('X-PC-Client-Hints'));
        return {
            ok: hints.length > 1, status: hints.length > 1 ? 200 : 503,
            text: async () => 'puzzle.signature', headers: new Headers(),
        };
    });

    const result = getPuzzle('/puzzle', 'sitekey', { attempts: 2 });
    await Promise.resolve();
    t.mock.timers.tick(1600);
    assert.deepStrictEqual(await result, { data: 'puzzle.signature', notice: null });
    assert.deepStrictEqual(hints, ['automation', 'automation']);
});
