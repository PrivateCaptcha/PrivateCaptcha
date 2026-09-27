<a name="top"></a>
<picture>
    <source media="(prefers-color-scheme: dark)" srcset="./web/static/img/pc-logo-light.png">
    <img alt="Private Captcha Logo" src="./web/static/img/pc-logo-dark.svg" height="50">
</picture>
---

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/PrivateCaptcha/PrivateCaptcha)

[![CI](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/ci.yaml/badge.svg)](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/ci.yaml) [![Go Lint](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/golangci-lint.yml) [![JS lint](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/widget.yml/badge.svg)](https://github.com/PrivateCaptcha/PrivateCaptcha/actions/workflows/widget.yml)

[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=PrivateCaptcha_PrivateCaptcha&metric=sqale_rating)](https://sonarcloud.io/dashboard?id=PrivateCaptcha_PrivateCaptcha)
[![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=PrivateCaptcha_PrivateCaptcha&metric=reliability_rating)](https://sonarcloud.io/dashboard?id=PrivateCaptcha_PrivateCaptcha)
[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=PrivateCaptcha_PrivateCaptcha&metric=security_rating)](https://sonarcloud.io/dashboard?id=PrivateCaptcha_PrivateCaptcha)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=PrivateCaptcha_PrivateCaptcha&metric=coverage)](https://sonarcloud.io/summary/overall?id=PrivateCaptcha_PrivateCaptcha&branch=main)

Private Captcha is an independent, privacy-first, self-hostable Proof-of-Work CAPTCHA service made in EU.

## About

### Project goals

- provide powerful means to fight bots, including AI scrapers, and spam even as AI improves
- make web a slightly better place by replacing existing frustrating CAPTCHAs
- stay focused on privacy and GDPR compliance as well as on-prem deployment
- provide stable, backward-compatible and reliable API and integrations
- be sustainable financially to fulfill previous goals long enough to make a difference

### Features

- [adaptive challenge difficulty](https://privatecaptcha.com/features/#difficulty) (including various configuration options, compute- and memory-hard proof-of-work)
- [custom difficulty rules](https://privatecaptcha.com/features/#rules) (based on traffic source, country, user-agent, IP address etc.)
- [form proxy](https://privatecaptcha.com/features/#form-proxy) (hide your webhook behind rate-limit and CAPTCHA check)
- optimized and rock-solid backend (low resource requirements, implemented in Go, backed by ClickHouse and Postgres)
- lightweight, [customizable](https://privatecaptcha.com/features/#widget) widget (including "invisible" version)
- [usage statistics](https://privatecaptcha.com/features/#stats) (weekly and monthly reports, dashboard domain- and organization-level usage statistics)
- [platform API](https://privatecaptcha.com/features/#platform-api) (import/export or manage organizations, domains, and forms via API)
- rich [integration ecosystem](https://docs.privatecaptcha.com/docs/integrations/) (most popular backend and client technologies, separate stacks like WordPress, Magento 2, TYPO3 and others)
- privacy-focused, no behavior tracking or PII processing

## Documentation

Please refer to the [official documentation](https://docs.privatecaptcha.com).

### Self-hosting

Self-hosting setup is in [another repository](https://github.com/PrivateCaptcha/self-hosting) and documentation - on main docs website.

## License

This project is distributed under a PolyForm Noncommercial License (see [LICENSE](./LICENSE) for more information). This allows you to self-host community edition of Private Captcha for non-commercial use. Commercial licenses available for enterprise edition - please contact us at hello@privatecaptcha.com

[Back to top](#top)
