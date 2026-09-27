# Contributing

## Developer how-to

### Getting started

To spin up a local version of Private Captcha _for development_, clone this repository and run `make run-docker` in the root (it requires to have Docker installed). You can check [Makefile](../Makefile) for details of what it does exactly.

### OpenAPI / Swagger

OpenAPI spec is [available](./openapi.yaml).

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

### Built with

- _Go_ for backend (API and Portal)
- _JavaScript_ (inevitably) for client widget, including WASM workers (where possible)
- _Postgres_ for "business" data (accounts, properties etc.)
- _ClickHouse_ for "operational" data (difficulty scaling, statistics etc.)
- TailwindCSS for Portal (backend)
