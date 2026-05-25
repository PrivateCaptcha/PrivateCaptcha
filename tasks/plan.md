# Implementation Plan: Form Submission Usage Metrics

## Overview
Add ClickHouse-backed usage metrics for forms proxy submissions. Metrics will record one final outcome per form submission, not per retry, and will expose form-level stats through Portal. The implementation follows existing request/verify log patterns while adding form-specific retention, deletion, and in-memory behavior.

## Architecture Decisions
- Use a new ClickHouse chain: `form_submit_logs` Null table -> `form_submit_logs_1h` -> `form_submit_logs_1d` -> `form_submit_logs_1mo`.
- Store `user_id`, `org_id`, `property_id`, `form_id`, `timestamp`, `success_count`, and `failure_count` so form stats, org/user deletion, and property/form deletion are all possible.
- User deletion should also delete form metrics.
- Add a new `common.FormSubmitRecord` and `RetrieveFormStatsByPeriod`, rather than overloading `RetrievePropertyStatsByPeriod`.
- Record form metrics only after captcha succeeds and form URL safety checks pass. Request creation errors should not emit metrics. HTTP attempts should emit exactly one final success/failure record after retries finish.
- Add a dedicated API server metrics channel analogous to `VerifyLogChan`, separate from the existing async form submission queue.

## Dependency Graph
ClickHouse migrations -> common store contract -> real/memory TimeSeries implementations -> API write path -> Portal read path -> maintenance deletion path.

Postgres form queries -> sqlc generation -> BusinessStoreImpl helpers -> Portal authorization and GC.

## Phase 1: Form Metrics Storage Contract

### Task 1: Add ClickHouse form submission log chain
**Description:** Add migrations for form submission metrics with raw Null ingestion and 1h, 1d, and 1mo aggregation tables.

**Acceptance criteria:**
- [x] New up/down migrations create and drop `form_submit_logs`, `form_submit_logs_1h`, `form_submit_logs_1d`, and `form_submit_logs_1mo`.
- [x] Aggregated tables include `success_count` and `failure_count`.
- [x] Tables include enough keys for form stats and deletion by form, property, org, and user.

**Verification:**
- [x] Run `make test-unit`.
- [ ] Run `make test-local-light TEST_NAME=TestGetFormStats` after later stats task exists.
- [x] Inspect migrations for down-order correctness.

**Dependencies:** None.

**Files likely touched:**
- `pkg/db/migrations/clickhouse/000011_create_form_submit_logs.up.sql`
- `pkg/db/migrations/clickhouse/000011_create_form_submit_logs.down.sql`
- `pkg/db/migrations/clickhouse/000012_aggregate_form_submit_logs_1h.up.sql`
- `pkg/db/migrations/clickhouse/000012_aggregate_form_submit_logs_1h.down.sql`
- `pkg/db/migrations/clickhouse/000013_aggregate_form_submit_logs_1d.up.sql`
- `pkg/db/migrations/clickhouse/000013_aggregate_form_submit_logs_1d.down.sql`
- `pkg/db/migrations/clickhouse/000014_aggregate_form_submit_logs_1mo.up.sql`
- `pkg/db/migrations/clickhouse/000014_aggregate_form_submit_logs_1mo.down.sql`

**Estimated scope:** M

### Task 2: Add TimeSeries form write/read/delete support
**Description:** Add form submission record types and implement write, stats retrieval, and deletion methods for both ClickHouse and in-memory stores.

**Acceptance criteria:**
- [x] `common.TimeSeriesStore` includes `WriteFormSubmitBatch`, `RetrieveFormStatsByPeriod`, and `DeleteFormsData`.
- [x] `TimeSeriesDB` writes form metrics and retrieves period stats with Today/Week/Month/Year behavior matching property stats.
- [x] `DeletePropertiesData`, `DeleteOrganizationsData`, and `DeleteUsersData` remove form metrics where applicable.
- [x] `MemoryTimeSeries` mirrors the new behavior and is covered by unit tests.

**Verification:**
- [x] Run `make test-unit`.
- [x] Unit tests in `pkg/db/timeseries_test.go` cover write, retrieve, delete form data, org delete, and user delete.

**Dependencies:** Task 1.

**Files likely touched:**
- `pkg/common/store.go`
- `pkg/common/...` for `FormSubmitRecord` type location if existing record types are elsewhere
- `pkg/db/timeseries.go`
- `pkg/db/timeseries_test.go`

**Estimated scope:** M

## Checkpoint: Storage Foundation
- [x] `make test-unit` passes.
- [x] The in-memory implementation proves the API contract before integration work.
- [x] Review ClickHouse table keys and retention before building callers.

## Phase 2: Write Path

### Task 3: Record final form submission outcomes from API server
**Description:** Add a form metrics channel and emit exactly one success/failure metric per final form submission attempt.

**Acceptance criteria:**
- [ ] `api.Server` has `FormSubmitLogChan` and `FormSubmitLogCancel` analogous to `VerifyLogChan`.
- [ ] `Server.Init` starts a batch processor using `TimeSeries.WriteFormSubmitBatch`.
- [ ] `Server.Shutdown` cancels and closes the new channel safely.
- [ ] `submitForm` records one final metric after retries finish.
- [ ] Failed captcha, missing form, disabled form, unsafe URL, and request construction errors do not emit form metrics.
- [ ] HTTP 2xx emits success; exhausted attempts with HTTP non-2xx or `client.Do` errors emit failure once.

**Verification:**
- [ ] Run `make test-unit`.
- [ ] Add or extend `pkg/api/form_proxy_test.go` tests for success, downstream failure with retries, invalid captcha, and unsafe URL metric behavior.

**Dependencies:** Task 2.

**Files likely touched:**
- `pkg/api/server.go`
- `pkg/api/form_proxy.go`
- `pkg/api/server_test.go`
- `pkg/api/form_proxy_test.go`
- `cmd/server/main.go`

**Estimated scope:** M

## Checkpoint: End-to-End Write Path
- [ ] API unit/integration tests prove successful form proxy submission writes one success metric.
- [ ] Retry tests prove multiple retries still produce one final metric.
- [ ] `make test-unit` passes.

## Phase 3: Read Path

### Task 4: Add Portal form stats endpoint
**Description:** Add a Portal JSON endpoint for form stats, analogous to `getPropertyStats`.

**Acceptance criteria:**
- [ ] New route serves form stats, with path: `/org/{org}/form/{form}/stats/{period}`.
- [ ] Handler authenticates the session, validates org access, validates the form belongs to the org, handles ETag/private cache headers, and returns success/failure series.
- [ ] New response type uses meaningful JSON keys, for example `success` and `failure`.
- [ ] `cmd/viewportal/main.go` includes a `stubFormStatsHandler`.
- [ ] Integration test mirrors `TestGetPropertyStats`: create account/form, insert form metrics, query all periods, assert totals.

**Verification:**
- [ ] Run `make generate-easyjson` if new easyjson response structs are added.
- [ ] Run `make test-unit`.
- [ ] Run `make test-local-light TEST_NAME=TestGetFormStats`.

**Dependencies:** Task 2.

**Files likely touched:**
- `pkg/portal/server.go`
- `pkg/portal/form.go` or `pkg/portal/property.go`
- `pkg/portal/response.go`
- `pkg/portal/response_easyjson.go`
- `pkg/portal/property_test.go` or new `pkg/portal/form_stats_test.go`
- `cmd/viewportal/main.go`
- `pkg/common/endpoints.go` only if a new endpoint constant is needed

**Estimated scope:** M

## Checkpoint: Read Path
- [ ] Form stats endpoint is protected and rejects bad org/form IDs.
- [ ] Integration test proves ClickHouse writes are queryable through Portal.
- [ ] `make test-local-light TEST_NAME=TestGetFormStats` passes.

## Phase 4: Retention And Deletion

### Task 5: Add soft-deleted form GC path
**Description:** Add Postgres queries and business methods for soft-deleted forms so `GarbageCollectDataJob` can purge form time-series data before hard-deleting form rows.

**Acceptance criteria:**
- [ ] `forms.sql` includes `GetSoftDeletedForms` and `DeleteForms`.
- [ ] `BusinessStoreImpl` exposes `RetrieveSoftDeletedForms` and `DeleteForms`.
- [ ] `GarbageCollectDataJob` retrieves soft-deleted forms, calls `TimeSeries.DeleteFormsData`, then hard-deletes forms.
- [ ] `DeleteOrganizationsData` includes all form metrics tables.
- [ ] `DeleteUsersData` includes form metrics tables for account deletion consistency.

**Verification:**
- [ ] Run `make sqlc`.
- [ ] Run `make vet-sqlc-local`.
- [ ] Run `make test-unit`.
- [ ] Add/extend GC tests to cover form metric deletion and hard delete after soft delete.

**Dependencies:** Task 2.

**Files likely touched:**
- `pkg/db/queries/postgres/forms.sql`
- `pkg/db/generated/*.go`
- `pkg/db/querier_stub.go`
- `pkg/db/business_impl.go`
- `pkg/db/business_impl_test.go`
- `pkg/maintenance/data.go`
- `pkg/api/gc_test.go` or `pkg/portal/jobs_test.go`

**Estimated scope:** M

## Checkpoint: Complete Feature
- [ ] `make test-unit` passes.
- [ ] `make vet-sqlc-local` passes.
- [ ] `make test-local-light TEST_NAME=TestGetFormStats` passes.
- [ ] Run broader `make test-local-light` before declaring done.
- [ ] Human review confirms endpoint path, response JSON names, and deletion behavior.

## Risks and Mitigations
| Risk | Impact | Mitigation |
|------|--------|------------|
| Form rows currently have `deleted_at` but no visible soft-delete path | Medium | Add retrieval/hard-delete GC support now; do not add user-facing soft-delete unless explicitly required. |
| ClickHouse monthly table may need `form_id` unlike request monthly table | High | Include `form_id` in `form_submit_logs_1mo` so yearly form stats work. |
| Retried submissions could overcount failures | High | Make `submitForm` return/emit one final result after retry loop, not inside each attempt. |
| New response structs require generated easyjson | Medium | Run `make generate-easyjson` after editing `pkg/portal/response.go`. |
| Integration tests may need ClickHouse MV processing delay | Medium | Follow existing `TestGetPropertyStats` pattern with a small wait after writes. |
