# Lean Authoritative Sessions

## Status

This document defines how the new system behaves, what problems it solves, and which tradeoffs it accepts. It intentionally contains no implementation plan or task list.

## Goal

PostgreSQL is authoritative for authentication, identity, challenges, expiration, and revocation. A separate process-local cache keeps normal requests fast.

Our main goal is _increased correctness_, not full correctness. The design removes the most important cross-replica session failures while deliberately accepting bounded staleness and best-effort UI state.

The system intentionally avoids operation gates, recovery paths, lifecycle flags, registration staging, fallback codes, synchronous expiration writes, and generic transition command APIs.

## Primary Problems

### Challenge Replay

Today, multiple replicas can validate the same locally cached challenge and create multiple successful outcomes.

The new design stores challenge state and attempts in PostgreSQL. Sign-in verification locks the row, and successful consumption revokes the predecessor and creates one successor atomically.

Registration uses a leaner consume-first flow. It revokes the pending challenge and returns authoritative registration data before the existing account transaction runs. The authenticated successor is inserted afterward. Only one request can consume the challenge, but account creation and successor insertion are deliberately not atomic with that consumption.

**Tradeoff:** The five-attempt limit is cumulative per pending challenge SID,
not global per user. Starting a new captcha-backed flow creates a new SID and
attempt budget, so issuance abuse still requires separate rate limits.

### Remote Revocation

Today, deleting a session does not invalidate copies already cached by other replicas.

The new design stores terminal `revoked` state in PostgreSQL. Cached Authority is trusted for at most the fixed 10-minute validation lease, after which PostgreSQL must confirm it again.

**Tradeoff:** Revocation is not immediate. Another replica may accept the session until its lease expires, and already admitted requests are not cancelled.

### Stale Resurrection

Today, a delayed whole-session upsert can recreate a SID after logout or rotation deleted it.

The new design retains revoked rows until expiration and makes payload persistence update-only, version-checked, and restricted to live states. A stale payload writer cannot insert a missing row or update a revoked row.

**Tradeoff:** Revoked rows consume database space until bounded expiration cleanup runs.

## Design

The design has three layers:

- The browser cookie contains only a random SID.
- Each replica keeps a local session copy so most authenticated requests need no session query.
- The `backend.sessions` table stores shared authority: state, version, user ID, expiration, challenge, and failed attempts.

## Session Model

```go
type Authority struct {
	State          State
	Version        int32
	UserID         int32
	ChallengeKind  ChallengeKind
	ChallengeEmail string
	ExpiresAt      time.Time
	LeaseUntil     time.Time
}

type Session struct {
	ID        string
	authority atomic.Pointer[Authority]
	Payload   *Payload
}
```

`Authority` is immutable and stored behind an atomic pointer. Readers receive a value copy rather than a mutable pointer.

PostgreSQL is the only writer of Authority version and expiration. Go never increments or derives those values. After SQL succeeds, the cache may install a replacement Authority containing SQL-returned values. Ambiguous or stale results keep or evict the cache entry rather than inventing new Authority.

No installed Authority means a local anonymous session. It can hold UI state but cannot authenticate.

`Payload` contains only these continuation and presentation values:

- Registration/display name.
- Return URL.
- Notification ID.
- First-session state.
- Anonymous invite continuation ID.
- Ad-hoc notification state.

Payload never controls authentication, user identity, challenge state, expiration, or revocation.

Generic Payload accessors expose only Payload keys. Login step, user ID, identity email, verification code, verification timestamp, failed attempts, registration verification result, persistence flags, and tombstones are not Payload keys in the new API.

## PostgreSQL Types

```sql
CREATE TYPE backend.session_state AS ENUM (
    'pending',
    'authenticated',
    'revoked'
);

CREATE TYPE backend.session_challenge_kind AS ENUM (
    'sign_in',
    'registration',
    'email_change'
);
```


## PostgreSQL Table

```sql
CREATE TABLE backend.sessions (
    session_id TEXT PRIMARY KEY,
    state backend.session_state NOT NULL,
    version INT NOT NULL DEFAULT 1,
    user_id INT REFERENCES backend.users(id) ON DELETE CASCADE,
    data BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,

    challenge_kind backend.session_challenge_kind,
    challenge_code TEXT,
    challenge_email TEXT,
    challenge_expires_at TIMESTAMPTZ,
    failed_attempts INT NOT NULL DEFAULT 0,

    registration_requires_verification BOOL,
    registration_invite_id INT
);

CREATE INDEX sessions_expires_at_idx
    ON backend.sessions (expires_at);

CREATE INDEX sessions_user_id_idx
    ON backend.sessions (user_id);
```

There are no state-shape `CHECK` constraints and no duplicate full-row validator in Go. Correct row shapes are produced and consumed by the session queries and transition methods.

All generated session SQL calls are wrapped by `BusinessStoreImpl` in `business_impl.go`.

The raw random SID remains the PostgreSQL key.

## States

```text
local anonymous
    -> pending sign-in
    -> pending registration

pending sign-in
    -> failed attempt
    -> resend
    -> revoked predecessor + authenticated successor

pending registration
    -> failed attempt
    -> resend
    -> verification-required result
    -> revoked predecessor

successful registration result
    -> existing account transaction
    -> authenticated successor insertion

authenticated
    -> payload update
    -> queued expiration renewal
    -> email-change challenge
    -> revoked

revoked
    -> expiration cleanup only
```

`revoked` is terminal. Normal logout and challenge consumption do not delete rows.

Failed attempts are cumulative across pending challenge reissue and resend for
the same SID. Reissue and resend stop at the configured cap, and resend also
requires the current challenge to be unexpired. Exhausted browser flows clear
their cookie and must restart the captcha-backed flow with a new SID.

## Versioning

Version starts at `1`. Initial row insertion does not increment it. PostgreSQL is the only version writer.

Version increments once for:

- Payload persistence.
- Failed challenge attempt.
- Challenge issue on an existing row or resend.
- Successful challenge consumption, including the predecessor's transition to `revoked`.
- Email challenge issue or consumption.
- First standalone revocation of a live SID.

A successful challenge consumption increments its predecessor exactly once, not once for consumption and again for revocation. A successor SID starts again at version `1`.

Authoritative reads, validation lease calculation, expiration renewal, and repeated revocation do not increment version.

Cached version is a SQL precondition only for asynchronous Payload persistence. Security transitions lock and inspect PostgreSQL state directly. Cache publication compares versions locally but does not add a SQL precondition.

## Key Queries

### Authoritative Read

```sql
SELECT *
FROM backend.sessions
WHERE session_id = $1
  AND state <> 'revoked'
  AND expires_at > NOW();
```

Authenticated and sign-in reads also verify that the associated user remains enabled and not deleted.

### Payload Persistence

```sql
UPDATE backend.sessions
SET data = @data,
    version = version + 1
WHERE session_id = @session_id
  AND version = @expected_version
  AND state IN ('pending', 'authenticated')
  AND expires_at > NOW()
RETURNING version;
```

This query is update-only. It cannot recreate a deleted row or modify authority.

The implementation uses a set-based batch form of this predicate and returns each successfully updated SID and its PostgreSQL-written version.

### Expiration Renewal

```sql
UPDATE backend.sessions
SET expires_at = GREATEST(expires_at, NOW() + @ttl)
WHERE session_id = ANY(@session_ids)
  AND state = 'authenticated'
  AND expires_at > NOW()
RETURNING session_id, version, expires_at;
```

Requests only enqueue SIDs whose cached expiration entered the renewal window. A background worker renews the entire batch in one query. Expiration renewal does not change version.

### Revocation

The first revocation changes state and increments version. Repeated revocation returns the existing terminal result without another increment.

All live predicates use `> NOW()`. Cleanup uses:

```sql
DELETE FROM backend.sessions
WHERE expires_at <= NOW();
```

## Separate Session Cache

`SessionStore` owns a dedicated typed cache based on otter:

```text
SID -> Session {
    immutable Authority
    mutable Payload
}
```

The cache is separate from `BusinessStore.cache` but the DB methods we call from `BusinessStore`.

Benefits:

- Sessions cannot enter business-cache snapshots.
- Sessions do not compete with users, organizations, forms, or rules.
- No generic `CacheKey` conversion or `any` type assertions.
- Cache operations can conditionally replace or remove a specific Authority version.
- Live Authority residency and validation-lease trust are independent.
- Clearing the business cache cannot remove or restore sessions.

The cache is bounded to 10,000 entries and uses approximately the existing
15-minute sliding residency. SQL-returned revocations remain in this cache as
terminal markers. While a marker is resident, local reads, transitions, and
worker callbacks cannot replace or remove it with live Authority, regardless
of version. `Resolve` rejects a cached marker without reading PostgreSQL.

This is a bounded mitigation rather than a permanent per-SID generation. A
marker can be removed by residency or capacity eviction, and a revocation that
returns no row cannot create a versioned marker. Cross-replica staleness remains
bounded by the validation lease.

## Validation Lease

Authenticated Authority has a fixed 10-minute lease.

```text
LeaseUntil = min(read time + 10 minutes, ExpiresAt)
```

Inside the lease, normal private requests use only memory.

After lease expiry:

- Read PostgreSQL.
- Reject missing, expired, revoked, or invalid user authority.
- Fail closed on PostgreSQL errors.
- Replace Authority and preserve local Payload when the database version is unchanged.
- Replace Authority and Payload from PostgreSQL when the database version is greater.
- Ignore a lower-version result so a delayed read cannot regress the cache.
- Keep a resident revoked marker instead of publishing any live result, regardless of version.
- Conditionally evict only the cache entry observed before a missing or invalid result.

Concurrent lease misses may issue duplicate reads. This is accepted instead of adding singleflight or a per-SID operation gate.

Remote logout or user disable can therefore remain locally accepted for at most the validation lease.

## Payload Worker

There is one payload worker per process.

`Payload.Set` and `Payload.Delete` always update memory. Persisted pending and authenticated sessions enqueue the SID best effort. Anonymous Payload is memory-only and never enqueued; its current snapshot is stored synchronously when the session becomes pending.

The worker:

- Deduplicates SIDs.
- Reads the current cache entry.
- Serializes the latest Payload.
- Executes one expected-version Payload update for the entire batch.
- Installs the SQL-returned version on exact success when the cached prior version still matches.
- Conditionally evicts the matching stale version on conflict.
- Never updates or evicts a resident revoked marker.
- Does not reread, merge, or retry version conflicts.

Payload loss during overflow, shutdown, eviction, serialization failure, or conflict is accepted because Payload contains no authority.

Enqueue is nonblocking. There is no process-wide queued-SID set that can suppress a mutation arriving while a previous batch is in flight.

## Expiration Renewal Worker

There is one expiration renewal worker per process, separate from the Payload worker.

After successful private authentication, a request whose cached expiration entered the renewal window:

- Enqueues the SID without blocking.
- Optimistically refreshes the browser cookie using the configured session TTL.
- Does not mutate cached Authority expiration or validation lease.

The worker:

- Deduplicates SIDs.
- Executes one expiration-extension query for the entire batch.
- Extends only live authenticated rows using `GREATEST` semantics.
- Does not increment version.
- Installs SQL-returned expiration only when the returned version still matches cached Authority.
- Preserves the existing validation lease when publishing expiration.
- Evicts the cache entry when the returned version differs or the SID is not returned.
- Never updates or evicts a resident revoked marker.

If persistence fails, the optimistic cookie may outlive the PostgreSQL row. The cookie cannot extend PostgreSQL authority, and a later authoritative read fails closed.

## Registration

Registration screening is resolved synchronously before pending-session issue.

If an anonymous session contains an invite ID, issue copies it into `registration_invite_id` without another invite lookup. Registration consumption returns that authoritative invite ID together with authoritative email and registration name. The existing background onboarding job and `LinkOrgInviteToUser` SQL remain responsible for final invite linkage and validation.

No invite proof or generation state is added. If the anonymous invite ID is absent or lost before issue, registration succeeds without linkage. Late Referer-based repair after issue is not authoritative.

Registration completion is deliberately split:

1. Consume and revoke the pending challenge, returning authoritative registration data.
2. Validate CSRF using the returned authoritative email.
3. Run the existing account transaction without session SQL.
4. Insert the authenticated successor in a second session call.

Invalid code may increment attempts before CSRF validation. Invalid CSRF may reject a request after its challenge was consumed or incremented. Account failure leaves the challenge consumed. Successor insertion failure leaves an account without an authenticated session, and the user can use normal sign-in. No recovery state or fallback code is added.

## Email Change

Email challenge issue uses the authenticated PostgreSQL session and current user email, not Payload identity.

- Wrong code increments shared attempts and version.
- Correct code consumes and clears the session challenge.
- The existing user-email update runs separately after successful challenge consumption.
- User update failure leaves the email unchanged and the challenge consumed.
- Name-only updates do not mutate email challenge state.

## Logout And User Disable

Logout synchronously transitions the presented SID to `revoked` before clearing the cookie or reporting success.
The Portal exposes logout only as an authenticated, CSRF-protected POST endpoint.

Repeated logout is idempotent.

Account deletion first commits the existing soft-delete transaction and then revokes user-bound sessions in a separate batch query. The current cookie is cleared directly after successful revocation instead of calling logout and issuing a duplicate single-SID query.

If soft deletion succeeds but revocation fails, the response reports an error and retains the cookie. Cached replicas may continue trusting previously validated Authority until the 10-minute lease expires. An authoritative read rejects the deleted user.

## Expiration

Initial policy remains:

| Setting | Value |
| --- | --- |
| Session TTL | 3 hours |
| Challenge TTL | 15 minutes |
| Validation lease | 10 minutes |
| Renewal window | 2 hours 30 minutes |

Expiration renewal is asynchronous and runs only when the cached deadline enters the renewal window.

Private reads schedule renewal after successful authentication. The existing private-write middleware order remains CSRF followed by private authentication, so writes schedule renewal only after successful CSRF and authentication.

Scheduling performs no PostgreSQL call. It enqueues the SID and optimistically refreshes the browser cookie without changing cached Authority. The renewal worker later installs SQL-returned expiration while preserving Version and LeaseUntil, or evicts the entry when it cannot safely publish the result.

## Manager API

Session Manager has a clear division between local scheduling, cache-backed resolution, and methods that always block on PostgreSQL.

The intended explicit operations are:

- Resolve a session using cache first and PostgreSQL on cache miss or validation lease expiry.
- Issue a sign-in challenge.
- Issue a registration challenge.
- Resend a pending challenge.
- Verify and consume a sign-in challenge while creating its successor atomically.
- Consume a registration challenge and return registration data.
- Insert the registration authenticated successor after account creation.
- Issue an email-change challenge.
- Consume an email-change challenge.
- Revoke one SID idempotently.
- Revoke user-bound sessions in one batch query.
- Enqueue Payload persistence locally.
- Enqueue expiration renewal locally.

Concrete Go names may be refined without changing these operations. There is no generic command API, synchronous `Touch`, `skipCache` flag, recovery method, or rollback-renew method.

## Session Roundtrip Budget

The budget counts new session-authority PostgreSQL calls. Existing business queries and transactions are excluded.

- Warm private request inside a valid lease: zero.
- Private request after lease expiry: one read.
- Payload mutation: zero request calls.
- Expiration renewal: zero request calls.
- Sign-in or registration challenge issue: one session call, plus one miss-classification read when rejected.
- Successful sign-in consume or resend: one warm session call or at most two cold session calls.
- Rejected sign-in or registration consume or resend: one additional miss-classification read.
- Successful registration completion: two session calls.
- Email challenge issue or consumption: one transition plus a possible cold read.
- Logout: one revocation call.
- Cold account deletion plus user-session revocation: at most two session calls.

Each background worker persists its whole batch per query rather than issuing one query per SID.

## Process Lifecycle And Cutover

- Both background workers start before HTTP traffic is accepted.
- HTTP requests stop before worker channels are closed.
- Best-effort Payload or renewal events may be lost during shutdown, but shutdown must not panic or send on a closed channel.
- The session cache is never loaded from disk.
- Legacy sessions are invalidated at cutover.
- Mixed old and new session binaries are unsupported.
- Deployment applies the additive schema, drains and stops all old replicas, and then starts only new replicas.
- Rolling rollback to an old session binary is unsupported after new traffic is accepted.

Local-only operations enqueue Payload persistence and expiration renewal. Cache-backed resolution reads PostgreSQL only on cache miss or validation lease expiry. Every security transition method always accesses PostgreSQL.

## Key Tradeoffs

- Remote revocation is lease-bounded, not immediate.
- UI Payload may be lost or overwritten.
- Concurrent cache misses may duplicate PostgreSQL reads.
- Security transitions require synchronous PostgreSQL access.
- Terminal revoked rows remain until expiration cleanup.
- Registration consumption, account creation, and successor insertion are separate operations.
- Registration validates CSRF after consumption returns authoritative email.
- Invite ID is copied into the authoritative session row without an additional validation query.
- Email challenge consumption and user update are separate operations.
- Account soft deletion and user-session revocation are separate operations.
- Expiration renewal and Payload persistence use separate background workers.
- Lost successful responses are not recovered.
- Legacy sessions are invalidated.
- Mixed old and new session binaries are unsupported.

## Non-Goals

- Immediate cross-replica invalidation.
- PostgreSQL read on every private request.
- Redis or pub/sub.
- Exact concurrent Payload merging.
- Durable anonymous sessions.
- Logout of every user device.
- Recovery from ambiguous commits.
- Legacy cookie or cache compatibility.
- Cross-entity atomicity for registration, email update, or account deletion.

## Delivery Constraint

Every major implementation change is committed and reviewed before the next begins. Dead legacy session code is removed after the new runtime is complete.

A formal security review runs only after legacy removal. Findings are recorded without immediate implementation and are addressed in the final hardening phase. Contract violations discovered during normal implementation or code review still block the current change and are fixed before commit.
