# Argon2id Final Review

Date: 2026-09-24

The owner prioritized the remaining medium findings in the order below.

## Resolved blockers

1. **Docker frontend build:** The Docker frontend stage copies only `web/` and `widget/`. Fixed Argon2id parameters are now in the Go puzzle code and the standalone widget provider, with no cross-directory profile import. A Docker build was not run locally.
2. **Low-end proof-of-work bypass:** At wire difficulty 0, about 99.93% of eight arbitrary distinct nonces pass. With the owner's approval, commit `437b3b3` requires wire difficulty at least 24 for Argon2id issuance, falling back to Blake2b v1 below logical difficulty 152. Signed v2 verification still uses the received wire difficulty for already-issued puzzles. Boundary tests and a browser solve/submit at wire difficulty 24 pass.
3. **Shared worker failure flow:** Both Blake2b and Argon2id now stop workers, discard pending lanes, render an error, clear any saved proof, and fire `privatecaptcha:error` once on initialization or solve failure. They no longer complete with placeholder lanes or fire `privatecaptcha:finish` on failure. Widget tests cover partial solutions, WASM and Noble search exceptions, popup mode, synchronous dispatch errors, and reset followed by a successful retry. The old `failWork` behavior originated in `765556e0` alongside browser measurement tests that expected both error and completion; the measurement core was removed in `ce5fc911` while that behavior remained.

## Prioritized follow-ups

1. **Medium - measure peak resident memory under concurrent verification.** `pkg/api/verifier.go` reserves 16 MiB of semaphore capacity per Argon2id verification, returning HTTP 429 when full; `pkg/puzzle/argon2id.go` calls `argon2.IDKey` for each of eight candidates. `make bench-puzzle BENCH_TIME=5x` reports about 134 MiB total allocations for an eight-solution verification. At a 512 MiB semaphore capacity, 32 sets can execute concurrently. Total allocated bytes are not peak live memory, so this does not prove an overrun, but the capacity alone is not an RSS ceiling. Measure peak RSS and GC behavior at 32 concurrent verifications and reduce concurrency or reserve headroom if the process exceeds its memory target.

No other critical finding was identified in the signed parser, PostgreSQL challenge mapping, widget bundle, or rollout path. Go/widget unit tests, Go race tests, PostgreSQL-only and full integration tests, production builds, and selected benchmarks pass. The final Chrome viewer flow passed at desktop and mobile; `SPEC.md` records the selected profile, cutover, bundle size, and browser evidence.
