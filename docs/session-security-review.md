# Session Security Review

## Status

This report records the formal post-cleanup security review required by the
session refactor. It reviews commit `2dbc37b0e729d4ac02ae95e3af937577556f1ae3`,
including the legacy removal committed in `2c5b6161`, and does not change the
implementation.

Review date: 2026-09-01.

The review covered challenge replay, revocation, stale resurrection, cache
trust and validation leases, expiration renewal, cookies, concurrency, and
failure handling. It also explicitly reviewed the approved registration,
invite, email-change, and account-deletion scope deviations.

This was a source and test review, not a dependency vulnerability scan or a
production penetration test.

## Summary

No critical finding was identified. Nine actionable findings are deferred to
the final hardening task:

| ID | Severity | Finding |
| --- | --- | --- |
| SSR-1 | High | Raw bearer SIDs and challenge secrets enter logs and audit records. |
| SSR-2 | Medium | Delayed reads or transition results can republish locally revoked Authority. |
| SSR-3 | Medium | Authenticated successor creation is not serialized with user disable or deletion. |
| SSR-4 | Low | Transaction-start timing can extend challenge or session validity across waits. |
| SSR-5 | Low | Logout is a state-changing GET endpoint and permits cross-site forced logout. |
| SSR-6 | Low | Terminal sessions retain obsolete challenge secrets and email metadata. |
| SSR-7 | Low | OTP generation falls back to a non-cryptographic PRNG when system entropy fails. |
| SSR-8 | Medium | Resend resets failed attempts and permits repeated attempt-cap renewal. |
| SSR-9 | Medium | Email-change consumption can renew cache trust after user disable or deletion. |

The approved cross-entity failure modes remain deliberate tradeoffs rather
than new findings. Several assurance gaps are also recorded for Task 12.

## Findings

### SSR-1: Session And Challenge Secrets In Logs And Audit Records

Severity: High.

The raw SID is the browser's bearer credential, but `SessionIDAttr` emits it
without redaction (`pkg/common/log.go:115-119`). Private and pending-challenge
requests add the SID to the logging context (`pkg/portal/server.go:508-517`,
`pkg/portal/twofactor.go:56-63`), so all downstream context-aware logs can
contain it. Payload and renewal queue-drop warnings also log raw SIDs
(`pkg/db/session_payload.go:110-116`, `pkg/db/session.go:360-362`).

PostgreSQL trace logging serializes query arguments, truncating only their
combined length (`pkg/db/postgres.go:38-53`). Session issue and consume calls
include predecessor SIDs, generated successor SIDs, and challenge codes among
those arguments (`pkg/db/session.go:108-153`, `pkg/db/session.go:174-236`).

The same request context is also copied into durable audit events. `RecordEvent`
places the raw SID in each event (`pkg/db/audit.go:142-172`), and persistence
writes it to `backend.audit_logs.session_id` (`pkg/db/audit.go:85-96`,
`pkg/db/queries/postgres/auditlog.sql:1-3`). Portal audit models expose the
stored value (`pkg/portal/audit.go:349-355`). This is a separate credential sink
that would remain even if structured application logs were redacted.

Anyone who can read these logs can replay a still-live authenticated SID as a
cookie. Query traces can additionally expose pending challenge codes and a
newly generated successor SID. Log access is a prerequisite, but disclosure
of either credential can become direct account access.

Task 12 must treat SIDs and challenge codes as secrets across structured logs,
SQL tracing, durable audit records, and audit presentation. Tests must prove
that raw predecessor SIDs, successor SIDs, and challenge codes are absent from
these sinks while preserving any required correlation through a non-replayable
representation.

### SSR-2: Authority Can Reappear After Local Revocation

Severity: Medium.

Cache publication rejects lower versions while a newer cache entry exists, but
revocation and successful predecessor consumption invalidate the entry without
leaving a terminal generation marker (`pkg/db/session.go:148-199`,
`pkg/db/session.go:254-273`). A delayed operation can then see an empty cache
and publish an older live result.

One concrete sequence is:

1. A cold `Resolve` reads authenticated version N from PostgreSQL.
2. Before publication, logout records version N+1 and invalidates the local
   cache.
3. The delayed read reaches the cache computation with no originally observed
   entry and installs version N (`pkg/db/session_cache.go:41-84`).
4. `sessionFromStored` gives that stale authenticated result a fresh validation
   lease (`pkg/db/session_cache.go:95-118`).

An authenticated email-change transition can follow the same pattern through
`publishTransitionSession` (`pkg/db/session.go:65-85`). This differs from the
approved cross-replica validation lease: the affected process has already
observed the terminal event and then forgets it.

Task 12 must decide on terminal cache markers or per-SID generations and cover
cold reads and every authenticated transition-publication path with
deterministic revoke-before-publication tests.

Mitigation implemented on 2026-09-02: SQL-returned revocations now remain in
the existing session cache as terminal markers. Local reads, transitions, and
worker callbacks cannot replace or remove a resident marker, including with a
higher live version. This closes the deterministic local races covered by the
tests, but does not fully eliminate the finding: markers still share the
cache's 10,000-entry capacity and 15-minute sliding residency, so they can be
evicted, and a revocation that returns no row has no versioned marker to retain.
Cross-replica validation-lease behavior is unchanged.

### SSR-3: Successor Creation Races User Disable Or Deletion

Severity: Medium.

Sign-in challenge consumption validates only session state. It does not join,
validate, or lock the user row before inserting an authenticated successor
(`pkg/db/queries/postgres/sessions.sql:134-164`). A valid pending challenge can
therefore create a fresh authenticated session after its user is disabled or
soft-deleted.

The delete-then-revoke flow introduces a related race. A user-wide revocation
statement can start while sign-in consumption holds the predecessor row lock.
After waiting, its statement snapshot can update the predecessor without
seeing the successor inserted by the consuming statement. The successor can
then survive the revocation pass. Registration successor creation checks the
user row (`pkg/db/queries/postgres/sessions.sql:188-198`) but does not serialize
that check with concurrent disable, soft deletion, or the following revocation
statement.

Later authoritative reads reject disabled and deleted users
(`pkg/db/queries/postgres/sessions.sql:1-35`), but a newly published successor
receives a validation lease before that read. Task 12 must review every
authenticated-successor path, not only sign-in, and add cross-store disable and
soft-delete race tests.

Mitigated on 2026-09-02 by requiring an enabled, non-deleted user in sign-in
consumption; registration successor creation already applies the same checks.
The rare concurrent update window is accepted without row locking.

### SSR-4: Transaction-Start Timing Can Extend Validity

Severity: Low.

Sign-in, registration, and email-change consumption use PostgreSQL `NOW()` in
their expiration predicates (`pkg/db/queries/postgres/sessions.sql:134-179`,
`pkg/db/queries/postgres/sessions.sql:200-216`). `NOW()` is stable from the
transaction start. These calls currently run in implicit autocommit
transactions, so transaction start and statement start coincide. A request
that starts immediately before expiration can wait for a row lock and still
consume the challenge after the wall-clock deadline. The configured lock
timeout permits a wait of up to ten seconds
(`pkg/db/postgres.go:23-28`, `pkg/db/postgres.go:114-120`).

The same timing question applies to session renewal. Its live-row predicate and
new expiration both use transaction-start `NOW()`
(`pkg/db/queries/postgres/sessions.sql:238-244`), so a row that expires while
the update waits can still be extended. A cold `Resolve` also captures
`readTime` before PostgreSQL and uses it for final publication checks
(`pkg/db/session_cache.go:35-92`). If the database read began before expiration
but returns afterward, that request can accept Authority using the earlier
time. The next request rechecks against the current clock, limiting this path,
while a renewal can extend the row for the normal session TTL.

These cases require the operation to begin while authority is still live, so
they are classified Low rather than as arbitrary resurrection. Existing tests
cover already-expired rows, not expiration while waiting. Task 12 must define
the required expiration point for challenge consumption, renewal, and cold
resolution, then add deterministic boundary tests.

### SSR-5: Logout Uses A State-Changing GET

Severity: Low.

Logout is registered as a public GET route (`pkg/portal/server.go:339-345`) and
performs synchronous shared revocation (`pkg/portal/utils.go:150-163`). A
SameSite=Lax session cookie can accompany a top-level cross-site GET, so an
attacker can navigate a victim to the logout URL and revoke the victim's
session. The impact is forced logout and loss of in-progress work, not account
access.

Task 12 must either approve this behavior explicitly or move logout to a
CSRF-protected unsafe method with route-level coverage.

Resolved on 2026-09-02 by moving logout to an authenticated, CSRF-protected
POST route. The signed-in header submits HTMX POST requests, while GET and
requests without valid CSRF cannot revoke the session.

### SSR-6: Terminal Rows Retain Challenge Data

Severity: Low.

Successful sign-in and registration consumption revoke their predecessors but
do not clear challenge kind, code, email, or expiration
(`pkg/db/queries/postgres/sessions.sql:134-186`). Standalone revocation also
changes only state and version (`pkg/db/queries/postgres/sessions.sql:246-264`),
so revoking an authenticated session with an email-change challenge retains
that challenge metadata. State checks prevent replay, but obsolete secrets and
email PII remain until expiration cleanup.

Task 12 must decide whether every terminal transition should clear challenge
columns immediately and add raw-column assertions. The existing successful
email-change SQL already clears all challenge columns
(`pkg/db/queries/postgres/sessions.sql:200-219`); its missing raw-column test is
an assurance gap, not evidence that this path retains data.

Mitigated on 2026-09-02 by clearing the OTP code during successful sign-in and
registration consumption, standalone revocation, and user-wide revocation.
Successful email-change consumption already clears all challenge columns.
Challenge kind, email metadata, and challenge expiration remain on other
terminal rows until expiration cleanup to keep transition queries simple.

### SSR-7: OTP Generation Falls Back To A Non-Cryptographic PRNG

Severity: Low.

Challenge codes normally use `crypto/rand`, but entropy failure is logged and
then falls back to the package-level `math/rand/v2` generator
(`pkg/portal/utils.go:17-28`). That API does not provide a cryptographic
unpredictability guarantee. The result is used for sign-in, registration,
resend, and email-change challenges.

System entropy failure is uncommon and is not directly controlled by a remote
attacker, which limits practical exposure. If it occurs, however, the system
continues issuing authentication secrets with a weaker security contract
instead of failing closed. Task 12 must decide whether all challenge issuance
must require cryptographic randomness and add an injected entropy-failure test.

### SSR-8: Resend Renews The Attempt Budget

Severity: Medium.

Each wrong code increments shared `failed_attempts`, and consume operations
stop at the configured cap. `ResendPendingChallenge` replaces the code and
expiration but also resets `failed_attempts` to zero without requiring the old
challenge to be unexpired or below its attempt cap
(`pkg/db/queries/postgres/sessions.sql:111-121`). A client that initiated the
challenge possesses its cookie and email-derived CSRF token and can alternate
guess batches with resends until the three-hour session expires.

The public chain's generic per-IP limiter bounds ordinary abuse, and every
resend generates a new random code and sends visible email, but there is no
challenge-specific resend or cumulative per-SID attempt policy
(`pkg/portal/server.go:303-316`, `pkg/portal/server.go:350-359`,
`pkg/portal/twofactor.go:179-214`). Distributed clients can avoid a purely
per-IP bound. This makes the effective limit apply to each issued code rather
than to the challenge SID described by the architecture.

Task 12 must define whether attempts are cumulative per SID, per user, or per
issued code and add a dedicated issuance/resend abuse policy with tests, or
explicitly approve the resettable cap as residual risk.

Resolved on 2026-09-02 with a cumulative per-SID policy. Pending challenge
reissue and resend preserve `failed_attempts` and stop at the configured cap;
resend also requires the current challenge to be unexpired. Exhausted browser
flows clear their cookie and must start a new captcha-backed flow with a new
SID. The remaining per-user issuance risk continues to rely on the existing
captcha and request rate limits.

### SSR-9: Email Transition Can Renew Trust For An Inactive User

Severity: Medium.

Email-change consumption checks authenticated session and challenge state but
does not join or revalidate the user row
(`pkg/db/queries/postgres/sessions.sql:200-219`). Both successful and wrong-code
results are published as authenticated Authority (`pkg/db/session.go:233-251`),
and `sessionFromStored` assigns every authenticated result a new validation
lease (`pkg/db/session_cache.go:95-118`).

A settings request can retrieve an active user, then race with user disable or
soft deletion before challenge consumption. The transition still returns and
publishes authenticated Authority with up to another ten minutes of local
trust. This exceeds the accepted fixed-lease staleness because the transition
actively renews trust without revalidating user state. It is separate from the
approved behavior where a successful challenge can be consumed before the
business email update fails.

Task 12 must decide whether email-change consumption revalidates active user
state or preserves the existing lease when it has not done so. Deterministic
successful- and wrong-code races must cover both disable and soft deletion.

Mitigated on 2026-09-02 by revalidating that the session user is enabled and
not deleted during email-change consumption. The rare concurrent update window
is accepted without row locking.

## Approved Tradeoffs

The following reviewed behaviors match the approved plan and are not new
security findings.

### Registration CSRF After Consumption

Registration deliberately reaches PostgreSQL before CSRF validation so that
consumption can return the authoritative email (`pkg/portal/csrf.go:64-85`,
`pkg/portal/twofactor.go:93-109`). Invalid CSRF can therefore reject a request
after a correct code consumed the challenge, or after a wrong code incremented
attempts. The consumed challenge cannot create an account or be replayed.

The correct-code/invalid-CSRF behavior is covered by
`pkg/portal/register_test.go:94-133`. The corresponding wrong-code attempt
behavior remains an explicit Task 12 assurance case.

### Consume-First Registration

Registration consumption, account creation, and successor insertion are three
separate persistence boundaries (`pkg/portal/twofactor.go:93-131`). Account
creation failure leaves a consumed challenge. Successor insertion failure
leaves a completed account without an authenticated session, and normal
sign-in remains available. No recovery state is part of the approved design.

Manager tests prove transition failures do not replace the cookie, but focused
full-handler tests for account-transaction failure and post-account successor
failure remain Task 12 assurance work.

### Invite Copy Without Issue-Time Validation

Registration issue copies the anonymous Payload invite ID into the session row
without another invite query (`pkg/session/manager.go:99-114`). Consumption
returns the stored ID, and mutable Payload changes cannot replace it. Final
membership linkage still requires the exact invite ID, an unlinked invite, and
a case-insensitive email match in `LinkOrgInviteToUser` SQL. A stale or
mismatched copied ID therefore fails closed at linkage. Direct reissue replacing
the copied ID is a continuation-semantics question, not an authorization bypass
under the reviewed final validation.

### Delete Then Revoke

Account soft deletion commits before the separate user-wide session revocation
(`pkg/portal/settings.go:439-488`). If revocation fails, the response reports an
error and retains the cookie. A later authoritative read rejects the deleted
user. This accepted partial failure is covered by
`pkg/portal/settings_test.go:818-874`. SSR-3 is narrower: it concerns a new
successor racing and escaping the revocation statement.

### Other Accepted Residual Risks

- Remote revocation remains accepted for the fixed validation lease on replicas
  that have not observed the terminal event.
- Email challenge consumption and the user-email update remain separate; user
  update failure leaves the challenge consumed.
- An optimistic renewal cookie may outlive PostgreSQL expiration when its queue
  event is dropped or persistence fails. The cookie cannot extend authority.
- Payload loss and Payload mutation races remain non-authoritative continuation
  loss under the approved best-effort contract. They do not change identity,
  authentication, challenge, expiration, or revocation state. Task 12 may
  reassess that reliability tradeoff but must not silently redefine it as an
  authority invariant.
- A sign-in successor SID collision remains a rollback-safe SQL error. The
  predecessor update and successor insert are one statement.
- Empty or NULL challenge fields require an out-of-contract database writer.
  Issue methods reject empty codes, and NULL challenge data fails closed.
- Lost successful responses are not recovered.

The mutate-first transition design also uses a race-tolerant follow-up
`InspectSessionChallenge` query when the mutation returns no row
(`pkg/db/business_impl.go:289-327`). Concurrent resend, consumption, or
revocation can change the typed outcome observed by that follow-up read. This
classification race is approved for uncommon terminal outcomes so that normal
success and wrong-code paths remain atomic and fast. Task 12 may reassess it
without reintroducing a monolithic transition query.

## Assurance Gaps

Task 12 should decide or test the following without treating each item as a
confirmed vulnerability:

- Add full-handler tests for registration account-creation failure and
  successor-insertion failure after consumption.
- Prove an invalid registration code can increment attempts before invalid CSRF
  rejection, matching the approved order.
- Add an email-change user-update failure test after successful challenge
  consumption.
- Assert raw email-change challenge code and expiration columns are NULL after
  successful consumption.
- Inject transition infrastructure errors and prove their original identity is
  preserved through every wrapper.
- The sign-in concurrency test already queries both candidate successor SIDs
  and proves exactly one row persists (`pkg/portal/session_consistency_test.go:146-157`);
  that previously planned check is complete.

## Reviewed Without A Finding

- Sign-in challenge replay is prevented by one conditional predecessor update
  and atomic successor insert. Cross-store concurrency proves one winner.
- Registration replay is prevented by the terminal predecessor state.
- Email-change replay is prevented by atomically clearing challenge columns.
- Failed attempts and resend state are shared through PostgreSQL and remain
  cumulative for each pending SID.
- Payload persistence is update-only, version-checked, and restricted to live
  unexpired rows; it cannot recreate revoked or deleted authority.
- Authority is immutable to callers, and generic Payload access excludes
  authentication fields.
- The dedicated session cache is not persisted and is isolated from business
  cache save, load, and clear operations.
- Renewal targets authenticated rows that are live at statement start, does not
  increment version, and publishes only SQL-returned expiration for the
  matching cached version. SSR-4 records the lock-wait expiration boundary.
- Cookie SIDs use `crypto/rand`; cookies are HttpOnly, SameSite=Lax, host-only,
  path-scoped, and Secure when configured or served through HTTPS.
- Security-transition and logout failures fail closed and do not rotate or
  clear the browser cookie before shared state succeeds. Optimistic renewal is
  the documented exception: it refreshes the cookie before best-effort
  PostgreSQL renewal, but the cookie cannot extend database authority.
- Session SQL is generated and parameterized.
- Worker enqueue and shutdown paths are protected against send/close races.

## Task 12 Disposition

All confirmed findings above are represented in the local Task 12 work list.
Task 12 must use one approved finding per test-first commit and stop for review
after each commit. No finding was implemented during this review.
