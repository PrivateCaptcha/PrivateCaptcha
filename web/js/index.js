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
            this.trackError(error || { message: msg, url, line, col });
        };

        window.addEventListener('unhandledrejection', (event) => {
            this.trackError(event.reason);
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
            message: error.message,
            stack: error.stack,
            url: window.location.href,
            userAgent: navigator.userAgent,
            timestamp: new Date().toISOString(),
        };

        fetch(this.config.endpoint, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(errorData),
        }).catch(console.error);
    }
};
