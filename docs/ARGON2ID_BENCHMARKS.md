# Argon2id Benchmarking

The browser Argon2id profile is now fixed at `m=16 MiB`, `t=1`, `p=1`, version `0x13`, and a 32-byte tag. These benchmarks help choose **wire difficulty and solution count** while checking client time, browser memory, and Go verification cost. They print measurements; they do not select or change production settings.

## Transcript And Solver

For each candidate, the password is exactly 128 bytes: the signed canonical 48-byte v2 puzzle body, 72 zero bytes, and the 8-byte solution nonce at offsets 120..127. The salt is the first 16 bytes of that password. Argon2id produces a 32-byte tag; its first four bytes are interpreted as a little-endian 32-bit value and compared with the wire-difficulty threshold. Go, the scalar WASM solver, and the Noble fallback must agree on this transcript.

The WASM solver sets the solution index in nonce byte 0 and searches a big-endian 32-bit counter in bytes 4..7; bytes 1..3 remain zero. Go verifies the actual submitted eight bytes, so the counter layout controls the search order, not the accepted nonce format. Each solution lane starts its own search.

The scalar and SIMD binaries live in `widget/wasm/argon2id-solver-{scalar,simd}.wasm`. The runtime currently embeds only scalar WASM; SIMD is reserved for later use. There is no runtime WASM fetch. The separate `widget-wasm/` builder is not part of this repository's committed source; the scalar binary is cross-checked against full Go reference tags in widget tests.

## Run The Widget Benchmark

```sh
make bench-widget
make bench-widget BENCH_ARGS='--help'

# Compare wire difficulties with the selected eight solution lanes at 16 MiB.
make bench-widget BENCH_ARGS='--algorithm=argon2id --argon-difficulties=8,24,40 --solutions=8 --runs=7 --warmup-ms=0 --window-ms=1 --json'

# Measure the Blake2b baseline separately.
make bench-widget BENCH_ARGS='--algorithm=blake2b --blake-difficulties=136,152,168 --solutions=24 --runs=7 --warmup-ms=0 --window-ms=1 --json'

# Quick smoke run while changing the benchmark.
make bench-widget BENCH_ARGS='--algorithm=argon2id --argon-difficulties=24 --solutions=8 --runs=1 --warmup-ms=0 --window-ms=1'
```

| Option | Default | Meaning |
|---|---|---|
| `--algorithm` | `all` | `all`, `blake2b`, or `argon2id` |
| `--runs` | `3` | Number of recorded timing windows per row |
| `--warmup-ms` | `250` | Unrecorded warm-up window per row |
| `--window-ms` | `250` | Minimum duration of each recorded window |
| `--solutions` | `1` | Comma-separated solution counts |
| `--blake-difficulties` | `136,152,168` | Blake2b logical difficulties |
| `--argon-difficulties` | `0,8,16` | Argon2id wire difficulties |
| `--json` | off | Structured JSON instead of a table |

`m`, `t`, and `p` are not command-line options: these binaries only implement the fixed profile. For machine-readable results:

```sh
make bench-widget BENCH_ARGS='--algorithm=argon2id --argon-difficulties=24,40 --solutions=8 --runs=5 --json'
```

## Read The Rows

| Operation | Measurement | Decision |
|---|---|---|
| `hash` | One provider hash of a candidate, without searching. `medianHashes` is one. | Baseline per-candidate cost and implementation regressions. |
| `solve` | Repeatedly tests nonces until all requested solution lanes pass the wire threshold. WASM uses `solve_batch` without JS calls per candidate; Noble hashes in JavaScript. | Visitor work at a particular difficulty and solution count. |

Blake2b `hash` uses the `blakejs` reference; Blake2b `solve` uses the current production solver and includes solver startup for each puzzle. Argon2id `hash` uses the selected provider directly; `solve` reuses that provider and measures the shared search function. The current provider is `argon2id-scalar`, falling back to `argon2id-noble` if scalar WASM cannot load.

The command-line Blake solve runs 24 lanes sequentially; the real widget runs them on four workers. Calibrate the Argon offset against repeated real-browser solves, not Node solve medians alone. The browser-calibrated offset is 128; issuance requires Argon wire difficulty at least 24, so logical difficulties below 152 fall back to Blake v1. Wire difficulty 8 is included above only to reproduce the initial browser comparison at logical 136.

`medianMillis` is the median time per hash or full solve across windows. `minimumMillis` and `maximumMillis` show the spread. `medianHashes` estimates candidate attempts per operation from the returned nonce, and `hashesPerSecond` uses paired timing and attempt samples. Search outcomes vary by puzzle; increasing difficulty by 8 roughly doubles expected attempts. More solutions increase total visitor and verifier work.

## Memory

Each supplied WASM instance owns fixed 16 MiB working memory plus 128 KiB of puzzle, digest, and stack space (16,908,288 bytes total). It reuses that memory between candidates and does not allocate a new buffer per hash. The Noble fallback does not retain this WASM buffer and may instead allocate JavaScript-managed working memory per candidate. One Argon worker avoids multiplying the WASM memory reservation per puzzle.

`rssDeltaMiB` and `externalDeltaMiB` are coarse Node process changes after provider initialization, not exact browser peak memory. Garbage collection and lazy page commitment can affect them; negative RSS deltas are possible. The fixed WASM size is the more useful lower bound per instance. Validate the eventual profile on representative browsers and low-memory devices.

## Go Verification

```sh
make bench-puzzle
make bench-puzzle BENCH_TIME=3x
```

`BenchmarkArgon2IDVerification` uses the production verifier at 16 MiB, with one to eight solutions. It reports elapsed time, allocated bytes, and allocations for the entire sequential verification operation. The Go verifier hashes the same 128-byte password/16-byte salt/32-byte tag as the widget.

Choose difficulty and solution count against explicit client latency and server verification budgets. Repeat runs on the same idle machine, record the source revision, Node/Go versions, provider, commands, and raw output. Node measurements are not real-browser evidence: they exclude worker startup, browser scheduling, device diversity, thermal throttling, and the network flow. Validate a candidate in the production widget and a real browser before enabling v2 issuance.
