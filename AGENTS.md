## General

- This project is a Golang monolith with internal parts in `pkg/` directory, main executable in `cmd/server`, JS widget code in `widget/` and Portal frontend code in `web/`. All dependencies get embedded into the final Golang binary.
- Instead of using `go`, `npm` or any other standard tooling, only use targets defined in the `Makefile` with appropriate names (e.g. `init-` for setup, `build-` for building and `test-` for testing)
- Add only the most important comments, prefer adding logs where necessary instead of comments
- If you're not sure how to run something, look for examples in `Makefile`, CI workflow `.github/workflows/ci.yaml` or dockerfiles in `docker/`
- If you change any external Go packages, run `make vendors`
- Fix errors before moving on. Never skip failures. Never declare done without a passing test. Go with simplest working solution first. No over-engineering.

### Databases

- we use Postgres and ClickHouse as databases
- we are using golang-migrate as a library for migrations and we run them ourselves via `pkg/db/init.go`
- base DB initialization scripts are in `pkg/db/migrations/init/`

#### Postgres

- Postgres migrations are in `pkg/db/migrations/postgres/` and queries are in `pkg/db/queries/postgres/`
- We use sqlc (config in `pkg/db/sqlc.yaml`) to codegen plain SQL into golang source code. After changing queries or migrations, run `make sqlc` in the root to regenerate the Go source code.
- you can verify the sqlc queries/migrations using `make vet-sqlc-local`
- we use generated Go code for Postgres via `pkg/db/business_impl.go`
- for `business_impl.go` methods naming convention for getters is to use `Retrieve` prefix instead of `Get` and to use `GetCached` prefix for cache-only data (sql queries in `pkg/db/queries/` can still use `Get`)

#### ClickHouse

- ClickHouse migrations are in `pkg/db/migrations/clickhouse/` and queries are written in Go code in `pkg/db/timeseries.go`
- we verify ClickHouse queries by writing integration tests for functionality that requires them
- we use ClickHouse database functionality through our own interface `TimeSeriesStore` (with in-memory stub implementation `MemoryTimeSeries`)

### Server

- Server (entrypoint in `cmd/server/main.go`) has logical parts of API, Portal and background worker (running maintenance jobs)
- handlers and routes for API part of the server are setup in `pkg/api/server.go` and `pkg/api/server_enterprise.go`
- handlers and routes for Portal part of the server are setup in `pkg/portal/server.go` and `pkg/portal/server_enterprise.go`
- maintenance jobs are defined in `pkg/maintenance/` package and scheduled in `cmd/server/main.go`

### Frontend

- All frontend code (HTML, CSS, JavaScript) for Portal is in `web/` directory
- We use htmx and Alpine.js libraries for frontend. For styles we use Tailwind CSS v3.4 (config is in `web/tailwind.config.js`)
- Frontend code is formatted using Golang templates (with our additional functions) with entrypoint in `pkg/portal/templates.go`. Our templates use a similar system to Hugo static site generator where custom pages always get used with "base" templates in `web/layouts/_default` for rendering, so we can reuse functionality.

## Environment setup

- Use `make init` to initialize everything for development

## Building instructions

- To build widget script for testing, run `make build-widget-script`
- To build portal/web JS code, run `make build-js` followed by `make copy-static-js`
- To build main server executable, run `make build-server` (or `make build-server-ee` if Enterprise Edition changes were made)

## Running instructions

- running locally requires `docker/pc.env` file to exist
- to launch usual version, run `make run-docker` in the repo root
- to launch enterprise version, first patch `cmd/server/main.go` file to pass `nil` instead of `quitFunc` into `maintenance.NewCheckLicenseJob` and then run `make run-docker-ee` in the repo root (don't commit this patch)
- open http://localhost:8080/portal/ URL in browser
- for login to the Portal use email `admin@privatecaptcha.local` (`PC_ADMIN_EMAIL` from `docker/pc.env`), wait for captcha to solve and on the next page enter two-factor code, which you can find by running `make find-docker-2fa` in the repo root (in parallel bash session)

## Testing instructions

- Test after writing. Never leave code untested.
- To run all Go unit tests, run `make test-unit`. Unit tests always run with "enterprise" tag. You can use `make test-unit` also as a "shortcut" to check if everything builds.
- To run JS widget tests, run `make test-widget-unit`
- To run a single Go integration test, run `make test-local-light TEST_NAME=<your-test-name>` (prefer running a single test for debugging)
- To run all Go integration tests, run `make test-local-light`
- Do not use underscores in Golang test names
- Put any new integration test for maintenance jobs to either Portal tests or API tests
- Prefer to not add any new DB methods for tests only, first try to reuse existing DB methods with some tests-only helpers (even if not optimal)
- To get unit tests code coverage, run `make test-unit-cover`
- To get integration tests code coverage, after running integration tests, open `coverage_integration/` directory in repository root
- Integration tests for Portal and API have global variables `store` (Postgres `db.BusinessStore`), `timeSeries` (ClickHouse, `common.TimeSeriesStore`) and `server` (respective server resource) that can be used instead of creating new resources.
- For exact HTTP routes to endpoints always check how they are setup in `server.go` and `server_enterprise.go`
- Portal render tests should be inside `pkg/portal/render_test.go`
- Always make sure all unit **and** integration tests pass before declaring done

## Output

- No sycophantic openers or closing fluff.
- No em dashes, smart quotes, or Unicode. ASCII only.
- Be concise. If unsure, say so. Never guess.

## Override Rule

- User instructions always override this file.
