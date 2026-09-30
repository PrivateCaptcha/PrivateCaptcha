## Tooling

- Prefer Makefile targets instead of invoking `go`, `npm`, or other build/test tooling directly.
- Use `make init` for initial development setup.
- If external Go dependencies change, run `make vendors`.
- If Postgres queries or migrations change, run `make sqlc` and verify with `make vet-sqlc-local`.
- If unsure which command to use, check `Makefile`, `.github/workflows/ci.yaml`, and `docker/`.

## Code

- Prefer the simplest minimal working solution. Do not over-engineer.
- Add comments only when necessary. Prefer useful logging over explanatory comments where appropriate.
- Fix failures before continuing. Do not ignore or skip errors.

### Database

- Database migrations use `golang-migrate` library and are run by the application.
- Postgres access uses generated `sqlc` code.
- In `pkg/db/business_impl.go`, getter methods use:
  - `Retrieve...` for normal getters.
  - `GetCached...` for cache-only getters.
- SQL query names may still use `Get...`.
- ClickHouse access must go through `TimeSeriesStore`; use `MemoryTimeSeries` for in-memory implementations.
- Verify ClickHouse behavior with integration tests.

### Frontend

- Portal frontend uses htmx, Alpine.js, Tailwind CSS v3.4, and Go templates.
- Portal pages render through the base templates in `web/layouts/_default`.

## Build

- Widget: `make build-widget-script`
- Portal JS: `make build-js` then `make copy-static-js`
- Server: `make build-server`
- Enterprise server: `make build-server-ee`

## Tests

- Before declaring work complete, all unit and integration tests relevant to the change must pass.
- Run only tests relevant to the change. For example:
  - Widget-only changes: widget unit tests.
  - Portal business logic changes that do not require ClickHouse: Postgres-only integration tests.
- Do not use underscores in Go test names.
- Unit tests: `make test-unit`
- Widget tests: `make test-widget-unit`
- Postgres integration tests:
  `make test-local-light TEST_NAME=<test-name>`
- Postgres + ClickHouse integration tests:
  `make test-local TEST_NAME=<test-name>`
- Omit `TEST_NAME` to run the full corresponding integration suite.
- Docker is unavailable. Local integration tests require existing Postgres and ClickHouse containers.
- Maintenance-job integration tests belong in Portal or API integration tests.
- Do not add DB methods only for tests unless existing DB methods plus test helpers cannot reasonably be used.
- Portal and API integration tests already provide global `store`, `timeSeries`, and `server` resources. Reuse them.
- Verify HTTP route paths against `server.go` and `server_enterprise.go`.
- Portal render tests belong in `pkg/portal/render_test.go` under `TestRenderHTML`.
- Unit coverage: `make test-unit-cover`
- Integration coverage is written to `coverage_integration/`.

## Output

- No sycophantic openers or closing fluff.
- No em dashes, smart quotes, or Unicode. ASCII only.
- Be concise. If unsure, say so. Never guess.

## Override Rule

- User instructions always override this file.
