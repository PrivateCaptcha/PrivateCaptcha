<a name="top"></a>
<img src="/web/static/img/pc-logo-dark.svg" height="50" />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/PrivateCaptcha/PrivateCaptcha)

![CI](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/ci.yaml/badge.svg) ![Go lint](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/golangci-lint.yml/badge.svg) ![JS lint](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/eslint.yml/badge.svg)

Private Captcha is an independent, privacy-first, self-hostable Proof-of-Work CAPTCHA service made in EU.

## About

Instead of asking users to solve complex puzzles or track their behavior, Private Captcha solves an invisible cryptographic task in the background. The system automatically adjusts the task difficulty, ensuring smooth access for real users while making it too costly for bots to attempt. Cryptographic task provides equal security regardless of bot's intelligence level, making it effective even as AI technology improves. Additionally, Private Captcha does not use cookies or track visitors and is fully GDPR-compliant that makes it easy to use for organizations that do business in EU.

### Features

- adaptive challenge difficulty (including various configuration options)
- optimized backend API endpoints (low resource requirements)
- lightweight widget ~17KB (including "invisible" version)
- usage statistics (backend)
- privacy-focused, no behavior tracking or PII processing

### Built with

- _Go_ for backend
- _Javascript_ (inevitably) for client widget, including WASM workers (where possible)
- _Postgres_ for "business" data (accounts, properties etc.)
- _ClickHouse_ for "operational" data (difficulty scaling, statistics etc.)

## Documentation

Please refer to the [official documentation](https://docs.privatecaptcha.com).

### Getting started

To spin up a local version of Private Captcha, clone this repository and run in the root `make run-docker` (it requires to have Docker installed). You can check [Makefile](./Makefile) for details of what it does exactly.

### Project structure

```
├── cmd/                              Main executable of the server and few helpers
├── docker/                           Development-only docker files
├── docs/                             Developer documentation snippets
├── Makefile
├── pkg/                              Backend part of the project (API and Portal)
├── web/                              Frontend part of the project (Portal)
└── widget/                           Client-side widget code
```

## License

This project is distributed under a PolyForm Noncommercial License (see [LICENSE](./LICENSE) for more information). This allows you to self-host community edition of Private Captcha for non-commercial use. Commercial licenses available for enterprise edition - please contact us at hello@privatecaptcha.com

[Back to top](#top)
