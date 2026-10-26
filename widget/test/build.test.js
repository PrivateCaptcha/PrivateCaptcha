import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { transform } from 'esbuild';
import { Window } from 'happy-dom';

for (const root of ['static', 'whitelabel']) {
    test(`${root} Blake2b widget omits Argon2id WASM and noble fallback`, async () => {
        const [blake2b, extended, wasm] = await Promise.all([
            readFile(`./${root}/js/privatecaptcha.js`, 'utf8'),
            readFile(`./${root}/js/privatecaptcha-ext.js`, 'utf8'),
            readFile('./wasm/argon2id-solver-scalar.wasm'),
        ]);

        const argonWasm = wasm.toString('base64');
        const nobleFallback = '"m" (memory) must be at least 8*p bytes';
        assert.ok(extended.includes(argonWasm));
        assert.ok(extended.includes(nobleFallback));
        assert.ok(!blake2b.includes(argonWasm));
        assert.ok(!blake2b.includes(nobleFallback));
        assert.ok(blake2b.length < extended.length);
        assert.ok(/x-pc-captcha-version",\s*(?:false\s*\?\s*"2"\s*:\s*"1"|"1")/.test(blake2b));
        assert.ok(/x-pc-captcha-version",\s*(?:true\s*\?\s*"2"\s*:\s*"1"|"2")/.test(extended));
    });
}

const builds = [
    { file: 'static/js/privatecaptcha.js', branded: true },
    { file: 'static/js/privatecaptcha-ext.js', branded: true },
    { file: 'whitelabel/js/privatecaptcha.js', branded: false },
    { file: 'whitelabel/js/privatecaptcha-ext.js', branded: false },
    { file: 'lib/index.js', branded: true, library: true },
];

for (const { file, branded, library } of builds) {
    test(`${file} has isolated branding dependencies`, async () => {
        const source = await readFile(file, 'utf8');
        const map = JSON.parse(await readFile(`${file}.map`, 'utf8'));
        const includes = (name) => map.sources.some(path => path.endsWith(`/js/${name}`));
        assert.equal(includes('branding.js'), branded);
        assert.equal(includes('branding.css'), branded);
        if (branded) {
            assert.equal(includes('branding-whitelabel.js'), false);
        } else {
            for (const marker of ['pc-spacer', 'pc-info', 'pc-logo', '--logo-height', '20em']) {
                assert.ok(!source.includes(marker), `whitelabel bundle contains ${marker}`);
            }
        }
    });

    test(`${file} preserves its branding through state changes and reset`, async (t) => {
        const window = new Window({
            url: 'https://localhost',
            settings: { enableJavaScriptEvaluation: true },
        });
        t.after(() => window.happyDOM.close());
        window.document.body.innerHTML = '<form><div class="private-captcha" data-sitekey="test" data-start-mode="click" data-whitelabel="true"></div></form>';
        window.__privateCaptchaWhitelabel = true;
        let source = await readFile(file, 'utf8');
        if (library) {
            source = (await transform(source, { format: 'iife', globalName: 'widgetLibrary' })).code;
        }
        window.eval(source);
        const host = window.document.querySelector('.private-captcha');
        let widget;
        if (library) {
            widget = new window.widgetLibrary.CaptchaWidget(host, { whitelabel: true });
        } else {
            window.privateCaptcha.setup();
            widget = window.privateCaptcha.autoWidget;
        }
        const captcha = host.querySelector('private-captcha');
        const checkBranding = () => {
            for (const selector of ['.pc-spacer', '.pc-info', '.pc-logo', 'a.pc-link']) {
                assert.equal(captcha.shadowRoot.querySelector(selector) !== null, branded, selector);
            }
            assert.ok(captcha.shadowRoot.querySelector('.pc-interactive-area label'));
        };
        checkBranding();
        for (const state of ['loading', 'ready', 'inprogress', 'verified', 'invalid', 'error']) {
            widget.setProgressState(state);
            checkBranding();
            if (state === 'inprogress') {
                widget.setProgress(50);
                assert.ok(captcha.shadowRoot.querySelector('progress-ring'));
            }
        }
        widget.reset({ whitelabel: true });
        checkBranding();
        assert.ok(captcha.shadowRoot.querySelector('#pc-checkbox'));
    });
}
