# Contributing

## Developer how-to

### Getting started

To spin up a local version of Private Captcha _for development_, clone this repository and run `make run-docker` in the root (it requires to have Docker installed). You can check [Makefile](../Makefile) for details of what it does exactly.

To run the server locally against already-running Docker databases:

```sh
make init # Initial frontend setup
make provision-local # Once, after starting empty database containers
make run-local
```

`run-local` builds and starts `bin/server` in `auto` mode, which runs migrations before serving requests. It reads `docker/pc.env`, rewrites `postgres:5432` to `localhost:15432`, disables required Postgres TLS for the test container, and connects to ClickHouse at `localhost:19000`. Override the ports with `TEST_PG_PORT` and `TEST_CH_PORT`, as with `test-local-light` and `test-local`.

### OpenAPI / Swagger

OpenAPI spec is [available](./openapi.yaml).

### Widget builds

`make build-widget-script` builds branded assets under `widget/static/js/` and
whitelabel assets under `widget/whitelabel/js/`. Both roots contain
`privatecaptcha.js` (Blake2b) and `privatecaptcha-ext.js` (extended), with source
maps. Whitelabel builds omit branding and size naturally to their content.
`make build-widget-library` always produces a branded library.

Both asset roots are embedded in the server. `widget.Static` serves only the
`static` root. SaaS consumers can select the other root using
`fs.Sub(widget.StaticFiles(), "whitelabel")` after checking customer eligibility.
The default/extended filenames are identical in both roots.

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
