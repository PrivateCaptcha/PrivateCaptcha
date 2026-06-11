'use strict';

const ErrorTracker = {
    config: {
        endpoint: '',
        maxErrors: 10,
        timeWindow: 60000, // 1 minute
    },

    errorCount: 0,
    lastReset: Date.now(),

    init(customConfig = {}) {
        this.config = { ...this.config, ...customConfig };
        this.setupHandlers();
    },

    setupHandlers() {
        window.onerror = (msg, url, line, col, error) => {
            // If error object exists, use it directly
            // Otherwise, create a proper Error object (not a plain object)
            const errorObj = error || new Error(msg);

            // Add location info if we don't have an error object
            if (!error) {
                errorObj.filename = url;
                errorObj.lineno = line;
                errorObj.colno = col;
            }

            this.trackError(errorObj);
        };

        window.addEventListener('unhandledrejection', (event) => {
            // Handle cases where reason might not be an Error object
            const error = event.reason instanceof Error
                ? event.reason
                : new Error(String(event.reason));
            this.trackError(error);
        });
    },

    trackError(error) {
        const now = Date.now();

        if (now - this.lastReset > this.config.timeWindow) {
            this.errorCount = 0;
            this.lastReset = now;
        }

        if (this.errorCount >= this.config.maxErrors) {
            console.debug('Throttling error reporting');
            return;
        }

        this.errorCount++;

        const errorData = {
            message: error.message || String(error),
            stack: error.stack || new Error().stack, // Fallback to current stack
            url: window.location.href,
            userAgent: navigator.userAgent,
            timestamp: new Date().toISOString(),
            // Add these for better debugging
            filename: error.filename || error.fileName,
            line: error.lineno || error.lineNumber,
            column: error.colno || error.columnNumber,
        };

        fetch(this.config.endpoint, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(errorData),
            // Add keepalive to ensure errors are sent even during page unload
            keepalive: true,
        }).catch(console.error);
    }
};

const demoWidget = {
    _clearTimeout: null,

    resetCaptcha() {
        let autoWidget = window.privateCaptcha.autoWidget;
        if (autoWidget) {
            autoWidget.reset();
        }
    },

    onDifficultyChange(endpoint) {
        if (this._clearTimeout) { clearTimeout(this._clearTimeout); }

        let autoWidget = window.privateCaptcha.autoWidget;
        if (autoWidget) {
            autoWidget.reset({ puzzleEndpoint: endpoint });
        }
    },

    onCaptchaReset() {
        this._clearTimeout = setTimeout(this.resetCaptcha, 2000 /*millis*/);
    },
};

function loadScript(url, callback) {
    const scripts = document.getElementsByTagName('script');
    for (let i = 0; i < scripts.length; i++) {
        if (scripts[i].src === url) {
            if (callback) {
                setTimeout(callback, 0);
            }
            return;
        }
    }

    let script = document.createElement('script');
    script.type = 'text/javascript';
    script.src = url;
    // several events for cross browser compatibility.
    script.onreadystatechange = callback;
    script.onload = callback;
    // Fire the loading
    document.head.appendChild(script);
}

const portalUnsavedChanges = {
    initialized: false,
    forms: new Map(),
    states: new Map(),

    init() {
        if (this.initialized) {
            return;
        }

        this.initialized = true;

        window.addEventListener('beforeunload', (event) => {
            if (!this.hasDirtyForms()) {
                return;
            }

            event.preventDefault();
            event.returnValue = true;
        });

        document.addEventListener('htmx:beforeRequest', (event) => {
            const trigger = event.detail && event.detail.elt;
            if (!(trigger instanceof Element)) {
                return;
            }

            const scoped = trigger.closest('[data-unsaved-changes-scope]');
            if (!scoped) {
                return;
            }

            const scope = scoped.dataset.unsavedChangesScope;
            if (!scope || !this.isScopeDirty(scope)) {
                return;
            }

            const form = trigger.closest('form[data-unsaved-changes-key]');
            if (form && (form.dataset.unsavedChangesScope === scope)) {
                return;
            }

            if (!window.confirm('You have unsaved changes. Leave without saving?')) {
                event.preventDefault();
                return;
            }

            this.clearScope(scope);
        });
    },

    registerForm(form) {
        if (!(form instanceof HTMLFormElement)) {
            return;
        }

        this.init();
        this.pruneForms();

        const key = form.dataset.unsavedChangesKey;
        const scope = form.dataset.unsavedChangesScope;
        if (!key || !scope) {
            return;
        }

        const currentSnapshot = this.snapshotForm(form);
        const initialState = form.dataset.unsavedChangesInitial || 'clean';
        const previous = this.states.get(key);

        let baseline = currentSnapshot;
        let dirty = false;
        if (initialState === 'dirty') {
            if (previous) {
                baseline = previous.baseline;
                dirty = currentSnapshot !== baseline;
            } else {
                dirty = true;
            }
        }

        this.states.set(key, { baseline, dirty, scope });
        this.forms.set(form, key);

        const syncFormState = () => {
            const state = this.states.get(key);
            if (!state || !document.body.contains(form)) {
                return;
            }

            state.dirty = this.snapshotForm(form) !== state.baseline;
        };

        form._syncFormState = syncFormState; // Store for cleanup
        form.addEventListener('input', syncFormState);
        form.addEventListener('change', syncFormState);
    },

    snapshotForm(form) {
        const values = [];

        for (const element of form.elements) {
            if (!(element instanceof HTMLElement) || element.disabled || !element.name) {
                continue;
            }

            if (element instanceof HTMLInputElement) {
                if ((element.type === 'checkbox') || (element.type === 'radio')) {
                    values.push(`${element.name}:${element.checked}`);
                } else {
                    values.push(`${element.name}:${element.value}`);
                }
                continue;
            }

            if ((element instanceof HTMLTextAreaElement) || (element instanceof HTMLSelectElement)) {
                if (element.multiple && element instanceof HTMLSelectElement) {
                    const selected = Array.from(element.selectedOptions).map(opt => opt.value).join(',');
                    values.push(`${element.name}:${selected}`);
                } else {
                    values.push(`${element.name}:${element.value}`);
                }
            }
        }

        return values.join('|');
    },

    pruneForms() {
        for (const [form, key] of this.forms.entries()) {
            if (!document.body.contains(form)) {
                this.forms.delete(form);

                if (!this.hasFormForKey(key)) {
                    const state = this.states.get(key);
                    if (state && !state.dirty) {
                        this.states.delete(key);
                    }
                }
            }
        }
    },

    hasFormForKey(key) {
        for (const registeredKey of this.forms.values()) {
            if (registeredKey === key) {
                return true;
            }
        }

        return false;
    },

    hasDirtyForms() {
        this.pruneForms();

        for (const state of this.states.values()) {
            if (state.dirty) {
                return true;
            }
        }

        return false;
    },

    isScopeDirty(scope) {
        this.pruneForms();

        for (const state of this.states.values()) {
            if ((state.scope === scope) && state.dirty) {
                return true;
            }
        }

        return false;
    },

    clearScope(scope) {
        for (const [form, key] of this.forms.entries()) {
            const state = this.states.get(key);
            if (state && (state.scope === scope)) {
                // Remove listeners before deleting mapping
                form.removeEventListener('input', form._syncFormState);
                form.removeEventListener('change', form._syncFormState);
                this.forms.delete(form);
            }
        }

        for (const [key, state] of this.states.entries()) {
            if (state.scope === scope) {
                this.states.delete(key);
            }
        }
    },
};

window.portalUnsavedChanges = portalUnsavedChanges;
