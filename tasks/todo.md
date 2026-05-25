# Todo: Form Submission Usage Metrics

## Phase 1: Storage Foundation
- [x] Add ClickHouse migrations for `form_submit_logs` raw Null table and 1h/1d/1mo aggregate chain.
- [x] Add form metrics types and `TimeSeriesStore` methods.
- [x] Implement `TimeSeriesDB` form write/read/delete methods.
- [x] Implement `MemoryTimeSeries` form write/read/delete methods.
- [x] Add unit tests for in-memory form metrics.

## Checkpoint 1
- [x] Run `make test-unit`.
- [x] Review ClickHouse schema and deletion keys.

## Phase 2: API Write Path
- [x] Add API server form metrics channel/cancel lifecycle.
- [x] Emit one final form submission metric from `submitForm`.
- [x] Add form proxy tests for success, final failure, retry overcount prevention, invalid captcha, and unsafe URL.

## Checkpoint 2
- [x] Run `make test-unit`.

## Phase 3: Portal Read Path
- [ ] Add form stats TimeSeries query usage in Portal handler.
- [ ] Add form stats route and form authorization helper if needed.
- [ ] Add form stats response type and regenerate easyjson if needed.
- [ ] Add `cmd/viewportal` stub form stats handler.
- [ ] Add integration test equivalent to `TestGetPropertyStats`.

## Checkpoint 3
- [ ] Run `make generate-easyjson` if response structs changed.
- [ ] Run `make test-unit`.
- [ ] Run `make test-local-light TEST_NAME=TestGetFormStats`.

## Phase 4: GC And Deletion
- [ ] Add sqlc queries for soft-deleted forms and hard-delete forms.
- [ ] Run `make sqlc`.
- [ ] Add BusinessStoreImpl methods for retrieving/deleting soft-deleted forms.
- [ ] Add form purge step to `GarbageCollectDataJob`.
- [ ] Include form metrics tables in org/user deletion and add `DeleteFormsData`.
- [ ] Add unit/integration coverage for form GC.

## Final Checkpoint
- [ ] Run `make vet-sqlc-local`.
- [ ] Run `make test-unit`.
- [ ] Run `make test-local-light`.
- [ ] Human review before implementation is marked complete.
