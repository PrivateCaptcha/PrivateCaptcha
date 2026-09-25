# Spec: Argon2id Proof-of-Work Challenge Support

Status: Approved

## Objective

Add hidden, production-quality Argon2id proof-of-work support alongside the existing Blake2b challenge.

The feature serves three groups:

- Site visitors solve either Blake2b or Argon2id challenges through the embedded widget.
- Operators opt individual properties into Argon2id by editing PostgreSQL directly.
- Backend operators receive bounded-memory Argon2id verification and reproducible command-line benchmark tooling.

Blake2b remains the default and preferred production challenge. Existing properties, newly created properties, cache-miss stubs, test puzzles, and Portal-owned puzzles remain Blake2b unless explicitly stated otherwise.

The Portal must not expose challenge selection in this work.

## Acceptance Criteria

- PostgreSQL has `backend.challenge_type` with `blake2b` and `argon2id`.
- `backend.properties.challenge` is non-null with default `blake2b`.
- Existing rows and newly inserted rows default to `blake2b`.
- Manual SQL can opt a property into `argon2id`.
- Blake2b puzzles continue using wire version 1 and remain byte-compatible.
- Argon2id puzzles use wire version 2 with a signed one-byte challenge field.
- Only exact puzzle versions 1 and 2 are accepted.
- Version 1 implicitly means Blake2b.
- Version 2 accepts only known challenge byte values.
- Argon2id uses RFC 9106 version `0x13`, `p=1`, `t=1`, `m=16 MiB`, and a 32-byte tag.
- Argon2id uses the normalized 128-byte v2 puzzle (canonical 48-byte body, zero padding, 8-byte solution nonce at offsets 120..127) as password and its first 16 bytes as salt.
- The JavaScript `thresholdFromDifficulty(puzzle.difficulty)` implementation remains unchanged.
- V2 Go verification uses the JavaScript `255.999` threshold rule exactly; v1 Go behavior remains unchanged.
- `pkg/difficulty/algorithm.go` remains unchanged.
- The selected Argon2id profile is reviewed against Blake2b solve time at logical difficulties 136, 152, and 168 and validated in a reference browser before production use.
- If calibrated Argon2id wire difficulty would be below 24, issuance falls back to a Blake2b v1 puzzle.
- Argon2id uses eight solutions, selected by the owner.
- Warm server validation for the selected solution count remains at or below 500 ms on the calibration machine.
- Submitted solution payloads must contain exactly the signed number of solutions before any expensive hashing occurs.
- Concurrent Argon2id verification is bounded by `PC_ARGON2_MEMORY_BUDGET_MIB`, defaulting to 512 MiB.
- Missing, invalid, or too-small memory-budget values fall back to 512 MiB and emit a warning.
- Argon2id verification returns HTTP 429 immediately when memory capacity is exhausted, without hashing a candidate.
- The widget uses the supplied scalar WASM solver and a noble-hashes pure-JS emergency fallback in one runtime JavaScript bundle. The supplied SIMD file remains unused.
- No WASM file, fallback script, or other runtime hashing asset is fetched separately.
- Argon2id uses one worker. Blake2b retains its existing worker behavior.
- `cmd/viewwidget` issues real Argon2id puzzles by default and supports `?challenge=blake2b`.
- `cmd/viewwidget` remains a human-only manual test tool and contains no calibration workflow.
- Command-line widget and Go benchmarks print independent measurements without selecting or rewriting a production profile.
- All Go unit tests, widget tests, PostgreSQL integration tests, full integration tests, builds, lint checks, golden vectors, and browser checks pass.

## Technical Design

### Database

Migration `000141`, after the difficulty migration `000140`, adds:

```sql
CREATE TYPE backend.challenge_type AS ENUM ('blake2b', 'argon2id');

ALTER TABLE backend.properties
ADD COLUMN challenge backend.challenge_type NOT NULL DEFAULT 'blake2b';
```

The down migration drops the column before dropping the enum.

`CreateProperty` continues omitting `challenge`, allowing PostgreSQL to apply the default. Portal update queries do not expose or mutate challenge selection.

`make sqlc` generates `dbgen.ChallengeType` and a `Challenge` field on property rows. The sqlc rename table must produce idiomatic Go names, including `ChallengeTypeArgon2ID`.

The manual `UpdatePropertyRow` to `Property` copy in `pkg/db/business_impl.go` must preserve `Challenge`.

An empty generated challenge value from a persisted pre-upgrade gob cache is treated as Blake2b. Any other invalid non-empty value is rejected and logged.

Manual opt-in uses:

```sql
UPDATE backend.properties
SET challenge = 'argon2id'
WHERE id = $1;
```

Direct SQL changes may remain hidden by the existing property cache until expiry or process restart. Cache invalidation is not expanded in this work.

### Challenge Model

`pkg/puzzle` owns a wire-level `Challenge` byte independent of generated database types:

```go
type Challenge uint8

const (
	ChallengeBlake2b Challenge = iota
	ChallengeArgon2ID
)
```

The API layer explicitly maps PostgreSQL strings to wire values. PostgreSQL enum ordering is never cast or treated as the wire encoding.

`ComputePuzzle` remains the common signed envelope and gains `Challenge() Challenge`. Separate puzzle structs are not introduced unless implementation proves the shared envelope unsafe or substantially less clear.

Challenge-specific solving and verification are dispatched behind small internal functions or interfaces. Generic puzzle identity, signing, expiration, replay handling, and serialization remain shared.

### Wire Protocol

Version 1 remains exactly 47 bytes:

| Offset | Size | Field |
|---:|---:|---|
| 0 | 1 | version = 1 |
| 1 | 16 | property ID |
| 17 | 8 | puzzle ID, little-endian |
| 25 | 1 | difficulty |
| 26 | 1 | solution count |
| 27 | 4 | expiration Unix seconds, little-endian |
| 31 | 16 | random user data |

Version 1 always means Blake2b.

Version 2 is exactly 48 bytes:

| Offset | Size | Field |
|---:|---:|---|
| 0 | 1 | version = 2 |
| 1 | 1 | challenge |
| 2 | 16 | property ID |
| 18 | 8 | puzzle ID, little-endian |
| 26 | 1 | calibrated wire difficulty |
| 27 | 1 | solution count |
| 28 | 4 | expiration Unix seconds, little-endian |
| 32 | 16 | random user data |

The challenge byte is covered by the existing puzzle HMAC. The dotted base64 transport, signature version, solution metadata format, and HMAC-SHA1 construction remain unchanged.

Parsers reject unknown versions, unknown challenges, short records, trailing bytes, and non-canonical lengths. Version 2 parameters are implicit and fixed; changing Argon2id `m`, `p`, `t`, tag size, or transcript in the future requires a new puzzle version.

### Issuance

`Verifier.PuzzleForRequest` uses the database challenge only for authenticated, non-stub property puzzles.

Issuance behavior is:

| Requested property challenge | Condition | Issued puzzle |
|---|---|---|
| Blake2b | Any | Blake2b v1 |
| Argon2id | Calibrated difficulty >= 24 | Argon2id v2 |
| Argon2id | Calibrated difficulty < 24 | Blake2b v1 |

Cache-miss stubs, API test puzzles, Portal echo puzzles, and other paths without a loaded property remain Blake2b v1.

Rules may change logical difficulty but do not change the property's requested challenge.

Logs for Argon issuance include logical difficulty, wire difficulty, selected profile, and whether low-end Blake fallback occurred.

### Argon2id Transcript

For each candidate:

```text
password    = 128 bytes: exact canonical 48-byte v2 puzzle body, 72 zero bytes, exact 8-byte solution nonce
salt        = password[0:16]
parallelism = 1
passes      = 1
memorySize  = 16384 KiB
tagLength   = 32
version     = 0x13
result      = little-endian uint32(tag[0:4])
valid       = result <= thresholdFromDifficulty(wireDifficulty)
```

The signature is not part of the Argon2id password or salt.

The generated solver continues using an 8-byte solution format. It places the solution index in byte 0, keeps bytes 1..3 zero, and uses bytes 4..7 for a big-endian 32-bit search counter. Verification accepts unique 8-byte solutions and does not trust client lane labels.

Nonce exhaustion returns an explicit error and never returns an all-zero placeholder as a valid solution.

### Benchmarking

`make bench-widget` benchmarks the Blake2b solver, a direct Blake2b JS hash, and the shared Argon2id provider/search functions in Node. The later production-dispatch task must import these same Argon modules. `make bench-puzzle` benchmarks the same Go implementation used by verification. Neither command recommends parameters or changes source files.

The fixed Argon2id hash profile and remaining calibration axes are:

| Parameter | Values |
|---|---|
| `m` | 16 MiB |
| solution count | 8 |
| `p` | 1 |
| `t` | 1 |
| tag length | 32 bytes (first 4 bytes used for difficulty) |

The widget benchmark reports independent Blake2b and Argon2id hash and solve rows. Difficulty lists, solution counts, run count, warm-up duration, and timing-window duration are command-line options. It supplies:

```text
Node runtime
provider implementation and WASM status
configured Argon2id memory
difficulty and solution count
median/minimum/maximum duration
median hashes and hashes per second
approximate process RSS and external-memory deltas
```

The owner selected the immutable v2 profile:

| Value | Selected |
|---|---:|
| Argon2id memory KiB | 16384 |
| Argon2id solution count | 8 |
| Argon2id difficulty offset | 128 |
| Minimum issued Argon wire difficulty | 24 |
| Lowest Argon logical difficulty | 152 (lower values issue Blake2b v1) |
| Go memory budget MiB | 512 |

The fixed parameters are defined in `pkg/puzzle/argon2id.go` and `widget/js/argon2-provider.js`. The client follows the signed puzzle's solution count; the server enforces the issued v2 profile. The Docker frontend stage builds using only `web/` and `widget/`.

The offset was revised from the initial Node-based choice of 114 to 128 before production rollout after browser validation. A pre-rollout security review set the minimum issued wire difficulty to 24: at wire difficulty 0, eight arbitrary unique nonces pass with approximately 99.93% probability. The verifier still checks the signed challenge and wire difficulty of every received v2 puzzle, including previously issued low-difficulty puzzles. Changing the v2 hash parameters, solution count, offset, or transcript after production rollout requires a new wire version.

Calibration on Apple M4 (darwin/arm64), Node v22.15.1, Go benchmark on the same machine, source revision `bc07cc9` plus the viewer and profile changes in T19. `make bench-widget BENCH_ARGS='--algorithm=blake2b --blake-difficulties=136,152,168 --solutions=24 --runs=7 --warmup-ms=0 --window-ms=1 --json'` and `make bench-widget BENCH_ARGS='--algorithm=argon2id --argon-difficulties=8,24,40 --solutions=8 --runs=7 --warmup-ms=0 --window-ms=1 --json'` measured the shared production search cores. Blake2b used `blake2b-scalar` WASM and Argon2id used `argon2id-scalar` WASM, with no SIMD attempt. Node solve medians (ms) were:

| Logical difficulty | Blake2b v1 (24 solutions) | Argon2id v2 (8 solutions) | Argon wire difficulty |
|---:|---:|---:|---:|
| 136 (calibration only; issuance falls back to Blake2b v1) | 350.813 | 115.330 | 8 |
| 152 | 1181.744 | 361.281 | 24 |
| 168 | 4776.740 | 1208.029 | 40 |

Node Blake solve rows test the lanes sequentially, while the browser runs them on four workers. Node timings therefore cannot calibrate the cross-algorithm visitor experience. In Chrome at `http://localhost:8080/`, the production widget with scalar WASM and one Argon worker measured the following solve medians (worker-pool elapsed time, excluding network retries and the final animation):

| Logical difficulty | Blake2b v1 (4 workers, 24 solutions) | Argon2id v2 (1 worker, 8 solutions) | Argon wire difficulty | Difference |
|---:|---:|---:|---:|---:|
| 136 (calibration only; issuance falls back to Blake2b v1) | 91 ms (6 solves) | 104 ms (5 solves) | 8 | +14% |
| 152 | 385 ms (5 solves) | 423 ms (5 solves) | 24 | +10% |
| 168 | 1,540 ms (6 solves) | 1,568 ms (5 solves) | 40 | +2% |

These measurements used `?level=136,152,168&challenge=blake2b` for the Blake baseline and, while offset 114 was still compiled, `?level=122,138,154` to issue the equivalent Argon wire difficulties for the candidate offset 128. Before the minimum-wire guard, the rebuilt viewer issued Argon wire difficulties 8, 24, and 40 at logical 136, 152, and 168; the final issuance policy falls back to Blake v1 at 136 and issues Argon v2 at 152 and 168. Individual solves vary with random puzzle bodies. `make bench-puzzle BENCH_TIME=5x` measured 55.986 ms/op at eight solutions (16 MiB, 134232857 B/op total allocations), below the 500 ms server ceiling. The provider-inclusive bundle size remains a final release-gate measurement.

The benchmark records whichever solver or provider loads in Node. Blake2b direct hash rows use the JS reference, while solve rows use the production solver. Pure-JS fallback results must be identified by provider name and must not be treated as equivalent to WASM results.

Node results do not measure browser worker scheduling, timer privacy, startup, thermal throttling, or mobile memory pressure. A final profile must also pass the real browser flow. The complete procedure and limitations are in `docs/ARGON2ID_BENCHMARKS.md`.

`pkg/difficulty/algorithm.go` is not modified. Its output is the logical difficulty. The selected profile converts logical difficulty to wire difficulty only when issuing Argon2id.

### Backend Solving And Verification

Go uses `golang.org/x/crypto/argon2.IDKey`.

V1 Blake2b verification retains its existing threshold behavior. V2 Argon2id uses a Go threshold implementation matching the current JavaScript `255.999` formula.

Verification order is:

1. Parse canonical payload lengths and exact solution count.
2. Validate puzzle version and challenge.
3. Validate puzzle signature and property ownership rules.
4. Check context cancellation.
5. Try to acquire weighted Argon memory capacity when required; return HTTP 429 if unavailable.
6. Check solution uniqueness.
7. Verify each candidate, checking context between candidates.
8. Release capacity.

One Argon call cannot be interrupted after it starts. The context is checked before each call.

The process-wide weighted semaphore capacity is read from `PC_ARGON2_MEMORY_BUDGET_MIB`. Missing, malformed, zero, negative, overflowing, or profile-incompatible values fall back to 512 MiB with a warning. Capacity is measured against the selected `m`.
After config reload, the verifier reads the updated budget and replaces its semaphore once active verifications have finished.

Oversized or over-counted solution payloads invoke zero Blake2b or Argon2id work.

### Widget

The widget parser stores the puzzle version, challenge, exact serialized puzzle bytes, and full 16-byte `userData`. It keeps the four-byte `AccountID` offset explicit without dropping those bytes from `userData`.

V1 maps to Blake2b. V2 reads the challenge byte immediately after version. Unknown versions and challenges fail before worker startup.

The Argon provider exposes one-shot hashing and WASM-batched searching:

```js
provider.hash(password, salt, output);
await provider.solve(canonicalBody, threshold, solutionIndex);
```

The Blake2b worker uses `createSolver` on the existing normalized 128-byte puzzle buffer. The Argon2id adapter constructs the normalized 128-byte v2 password and hashes it with its first 16 bytes as salt. Its output buffer holds the four bytes used for the difficulty comparison.

The supplied provider is loaded once per Argon worker and retains its 16 MiB plus 128 KiB WASM linear memory. It tries scalar WASM. The WASM solver hashes search batches without crossing into JavaScript for each candidate.

If scalar WASM fails, the worker uses `@noble/hashes` Argon2id. The noble fallback may allocate a fresh `m`-sized array per candidate; this allocation and severe speed degradation are accepted only for the emergency fallback. OOM and provider failures terminate cleanly with the existing widget error flow.

Argon2id uses one worker to avoid multiplying its WASM memory buffer. Blake2b keeps its current pool behavior.

A v2 Argon puzzle never silently changes to Blake2b in the client. Low-end fallback is decided by the server and represented explicitly as a v1 Blake puzzle.

The production widget remains one runtime JavaScript request. Scalar WASM, the pure-JS fallback, and worker source are embedded. Existing source-map output may remain because it is not a runtime dependency.

### Viewwidget

`cmd/viewwidget` is a manual end-to-end viewer, not a benchmark tool.

Normal puzzle routes issue initialized, non-stub Argon2id puzzles by default so submitted work is actually verified.

`?challenge=blake2b` switches normal puzzle issuance to Blake2b v1.

Echo and zero-puzzle behavior remains Blake2b unless required to make normal Argon testing correct.

The viewer parses and verifies both protocol versions using production puzzle code.

### Benchmark Tools

Benchmark code lives with widget tests and uses their existing esbuild pipeline to embed WASM. It runs directly from the command line and has no HTTP server, browser page, selector, dedicated worker, or benchmark-specific esbuild configuration.

The Go verification benchmark lives in `pkg/puzzle`. Parameter selection remains an explicit owner decision based on recorded measurements and final browser validation.

## Tech Stack

- Go 1.27.
- PostgreSQL migrations through golang-migrate.
- sqlc-generated PostgreSQL models and queries.
- `golang.org/x/crypto/argon2` for backend Argon2id.
- Existing `golang.org/x/sync/semaphore` for weighted memory admission.
- JavaScript ES modules bundled by esbuild into the existing IIFE widget.
- Existing inlined Web Worker architecture.
- `argon2id@1.0.1` for the supplied scalar WASM.
- `@noble/hashes@2.4.0` for pure-JS emergency fallback.
- RFC 9106 cross-language vectors.
- No Portal UI framework changes.

## Commands

Existing commands:

| Purpose | Command |
|---|---|
| Initialize dependencies and assets | `make init` |
| Regenerate Go vendors | `make vendors` |
| Regenerate sqlc output | `make sqlc` |
| Validate SQL locally | `make vet-sqlc-local` |
| Build widget | `make build-widget-script` |
| Build viewwidget | `make build-view-widget` |
| Build Enterprise server | `make build-server-ee` |
| Run viewwidget | `make view-widget` |
| Format Go | `make format` |
| Lint Go | `make lint` |
| Run Go unit tests | `make test-unit` |
| Run widget unit tests | `make test-widget-unit` |
| Run PostgreSQL integration tests | `make test-local-light` |
| Run full integration tests | `make test-local` |
| Run existing Go benchmarks | `make bench-unit` |
| Build all standard binaries | `make build` |

Required new commands:

| Purpose | Command |
|---|---|
| Run widget hash/search benchmarks | `make bench-widget` |
| Run targeted Go verification benchmarks | `make bench-puzzle` |

No direct `go`, `npm`, or other standard tooling commands are used outside Make targets.

## Project Structure

| Path | Responsibility |
|---|---|
| `pkg/db/migrations/postgres/000141_*` | Challenge enum and property column |
| `pkg/db/sqlc.yaml` | Generated enum naming |
| `pkg/db/generated/` | Regenerated sqlc output |
| `pkg/db/business_impl.go` | Preserve challenge through property copies |
| `pkg/puzzle/` | Challenge model, v1/v2 codecs, profiles, solving, verification, benchmarks |
| `pkg/api/verifier.go` | Property-driven issuance and resource admission |
| `pkg/common/config.go` | Argon memory-budget config key |
| `pkg/config/env.go` | `PC_ARGON2_MEMORY_BUDGET_MIB` mapping |
| `widget/js/` | Parser, providers, worker dispatch, solver |
| `widget/test/` | Codec, vectors, fallback, worker, and bundle tests |
| `cmd/viewwidget/` | Manual Argon-first widget testing |
| `docs/ARGON2ID_BENCHMARKS.md` | Benchmark commands, output, and limitations |
| `Makefile` | Widget and Go benchmark targets |
| `SPEC.md` | Approved requirements and measured profile values |

Tests stay next to their existing packages. Shared cross-language fixtures use a single checked-in testdata representation consumed by Go and JavaScript where practical.

## Code Style

Use existing Go and JavaScript conventions. Keep challenge mapping explicit and reject unknown values.

```go
type Challenge uint8

const (
	ChallengeBlake2b Challenge = iota
	ChallengeArgon2ID
)

func calibrateChallenge(logical uint8, requested Challenge) (Challenge, uint8) {
	if requested != ChallengeArgon2ID {
		return ChallengeBlake2b, logical
	}

	wire := int(logical) + argon2DifficultyOffset
	if wire < 0 {
		return ChallengeBlake2b, logical
	}

	return ChallengeArgon2ID, uint8(wire)
}
```

Additional conventions:

- Use `Argon2ID` in authored Go identifiers.
- Use `argon2id` in PostgreSQL, query strings, URLs, and JavaScript challenge values.
- Use explicit little-endian encoding.
- Keep profile constants together and immutable for protocol v2.
- Prefer small challenge-specific functions over a broad generic crypto framework.
- Do not add comments unless the protocol or calibration rule is not self-explanatory.
- New Go test names contain no underscores.
- Generated files are changed only through `make sqlc` or `make vendors`.

## Testing Strategy

### Unit Tests

- V1 byte-for-byte serialization remains unchanged.
- V2 serialization has the exact 48-byte layout.
- V1 implicitly maps to Blake2b.
- Unknown versions, challenges, lengths, and trailing bytes fail.
- Database-to-wire mappings cover both values, empty legacy cache data, and invalid values.
- Logical-to-wire profile mapping and low-end Blake fallback are deterministic.
- V1 and v2 threshold behavior is version-specific.
- Argon2id Go solving and verification succeed for selected profile constants.
- Exact under-count and over-count payloads fail before hash invocation.
- Cancellation while waiting for memory capacity fails closed.
- Capacity is always released.
- Invalid memory-budget configuration falls back to 512 MiB with a warning.
- Solver exhaustion returns an error.

### Cross-Language Tests

- Go, scalar WASM, and noble JS produce the same 32-byte tag.
- Fixtures cover multiple salts and nonces at the fixed memory size.
- V2 threshold boundary fixtures match JavaScript exactly.
- Go and JavaScript parse the same v1 and v2 puzzle fixtures.
- Submitted solution bytes match across Go and JavaScript.

### Widget Tests

- V1 and v2 parsing.
- Correct 16-byte `userData`.
- Unknown version/challenge rejection.
- Scalar WASM success.
- Scalar WASM failure followed by noble JS.
- Noble OOM/provider failure reports a widget error.
- Argon uses one worker.
- Blake retains current workers.
- Production output contains no runtime `.wasm` or extra hashing script dependency.

### Benchmarks

- Per-call Go Argon2id at 16 MiB.
- Full Go verification for solution counts 1 through 8.
- Node timings for shared widget Blake2b and Argon2id providers at configurable difficulties and solution counts.
- Provider, WASM, hash throughput, and approximate Node process-memory reporting.
- Browser end-to-end validation of the manually selected profile before production use.
- Go allocation and memory reporting for bounded verification planning.

### Integration Tests

- Migration defaults existing and new properties to Blake2b.
- Manual database change to Argon2id causes non-stub API issuance of v2.
- Blake properties issue v1.
- Stubs issue v1.
- Low calibrated difficulty falls back to v1.
- Argon solution verification succeeds through the API.
- Tampered challenge bytes fail signature verification before Argon work.
- Over-counted payloads invoke no Argon work.
- Property updates preserve challenge.

### Browser Verification

Use the real built widget in Chrome DevTools:

- Default viewwidget flow solves and verifies Argon2id.
- `?challenge=blake2b` solves and verifies Blake2b.
- No console errors occur.
- No WASM or fallback JavaScript network request occurs.
- Worker/provider initialization completes.
- Widget remains functional at desktop and mobile viewport sizes.

### Final Quality Gates

Run all listed builds, lint checks, unit tests, integration tests, benchmarks, and browser checks before declaring completion.

After planned verification, run security and code reviews. Add the final task named `Review and prioritize deferred code/security findings` to the task list. Medium or large newly discovered changes are reported there and remain blocked on user prioritization. Small findings may be fixed normally. Critical blockers stop work immediately for user direction.

## Boundaries

### Always Do

- Keep Blake2b the database and product default.
- Preserve v1 Blake2b wire compatibility.
- Sign the challenge byte through the puzzle body.
- Verify integrity before expensive Argon work.
- Enforce exact lengths, versions, challenges, and solution counts.
- Bound server memory.
- Keep browser hashing off the main thread.
- Keep all widget runtime code in one JavaScript bundle.
- Use shared cross-language fixtures.
- Record the selected profile, benchmark commands, environment, and browser validation in this spec.
- Run all required Make targets.
- Preserve unrelated user changes.
- Put deferred review findings in the final user-controlled task.

### Ask First

- No manually reviewed benchmark profile satisfies all constraints.
- A change to the v2 wire layout or Argon transcript becomes necessary.
- A dependency must replace the supplied WASM solvers or noble-hashes.
- Browser support must be narrowed.
- A critical security issue blocks safe implementation.
- Calibration would require modifying `pkg/difficulty/algorithm.go`.
- Production profile parameters would need to change without a new protocol version.
- Portal exposure or API-based challenge configuration is proposed.

### Never Do

- Expose challenge selection in the Portal during this work.
- Change `pkg/difficulty/algorithm.go`.
- Let clients select Argon parameters.
- Fetch WASM or fallback scripts at runtime.
- Treat unknown wire values as Blake2b.
- Silently downgrade a received v2 Argon puzzle in the widget.
- Issue Argon2id for cache-miss stubs.
- Remove or weaken tests to obtain a passing build.
- Implement medium or large review findings before the final user prioritization task.
- Change signature algorithms, analytics schemas, or unrelated protocol fields.

## Success Criteria

The work is complete when a manually opted-in property issues signed v2 Argon2id puzzles, the production widget solves them through scalar WASM or the documented JS fallback, and the backend safely verifies them under a bounded memory budget.

At the selected logical difficulty, normal Argon2id browser solve time must remain within +/-20 percent of current Blake2b at levels 136, 152, and 168, except where the explicitly selected low-end Blake fallback applies.

Blake2b remains the default, v1 remains compatible, viewwidget supports manual switching, benchmarking is reproducible, and every required quality gate passes.

## Bundle Baseline And Remaining Measurements

The owner has selected the browser-calibrated profile above. The final production widget is 89,707 bytes raw and 31,283 bytes gzip level 9, respectively +21,774 and +10,595 bytes over the Blake2b baseline.

On 2026-09-23, the production viewer was tested in Chrome at desktop 1440x900 and mobile 390x844. `/`, `/popup.html`, and `/dark.html` each completed a green submission with default Argon2id v2 and with `?challenge=blake2b` v1 (12 submissions). Default logical difficulty 168 issued Argon wire 40 with 8 solutions and one scalar WASM worker; Blake kept difficulty 168, 24 solutions, and four WASM workers. The result pages had no console errors; the Network panel showed no separate WASM or fallback script request. Inlined blob workers were loaded from the single widget script. The manual viewer intentionally returns 500 on two out of three puzzle requests to test retry handling, so corresponding retry messages are expected on some puzzle pages.

After the minimum-wire change on 2026-09-24, Chrome confirmed that `/?level=136` issues Blake v1 with four workers while `/?level=152` issues Argon v2 at wire 24 with eight solutions and one scalar WASM worker. The wire-24 submission reached a green result with no unexpected result-page console errors or separate hashing asset request.

The final Make gate passed formatting, Go/widget lint, sqlc generation and local vet, vendor regeneration, Go/widget unit and Go race tests, PostgreSQL-only and full integration tests, production builds, and both benchmarks. sqlc emitted only import-order drift in two generated files; the hook-required goimports ordering was retained, leaving no generated content diff. Vendor and widget lockfile content did not change.

### Blake2b Widget Baseline

Measured from source revision `fcd4c882` using `make build-widget-script STAGE=prod` followed by `make widget-size`:

| Artifact | Size |
|---|---:|
| Production widget, raw | 67,933 bytes |
| Production widget, gzip level 9 without name/timestamp | 20,688 bytes |

| Value | Resolution |
|---|---|
| Argon2id `m` | Fixed at 16 MiB by the supplied WASM binaries |
| Argon2id solution count | 8 |
| Difficulty offset | 128 |
| Minimum issued Argon wire difficulty | 24 |
| Low-end Blake cutover | Logical difficulty below 152 |
| Final widget size delta | +21,774 bytes raw; +10,595 bytes gzip level 9 |
