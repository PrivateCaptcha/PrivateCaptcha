# Difficulty simulator

## What the chart shows

The gnuplot output normally has two panels. With `--debug`, it has three panels:

1. **Traffic inputs and bucket state**
   - `property background req/step`: generated background traffic for the property.
   - `selected user req/step`: generated traffic for the requester being scored.
   - `property level p`: current variable leaky-bucket level for the property.
   - `user level u`: current constant leaky-bucket level for the selected requester.
   - `property leakRate λ`: expected property requests per property bucket interval.
   - `P8`: `sqrt((minExpectedRPS*bucketSize)^2 + (K*λ)^2)`.

2. **Debug: normalized pressure and float difficulty components** — shown only with `--debug`.
   - `property_ratio p/P8`: the most important diagnostic for property pressure.
   - `user_ratio u/U8`: normalized selected-user pressure.
   - `property_excess_buckets p/λ`: accumulated property excess measured in normal buckets.
   - `instant rate ratio`: current property requests per step compared with `leakRate` scaled to one simulation step.
   - `user_delta_float`, `property_delta_float`, `total_delta_float`: difficulty contributions before rounding to `uint8`.

3. **Difficulty output**
   - `V1 legacy`: old combined-level formula, using `userLevel + propertyLevel`.
   - `V2 candidate`: new split user/property formula using dynamic `P8`.
   - `selected --formula`: whichever formula you selected with `--formula=v1` or `--formula=v2`.
   - With `--debug`, this panel also plots `V2 float before rounding`.

The simulator defaults to `--base=100` so changes are visually easier to see.

## Troubleshooting

`property_ratio = property_level / property_ref` is the first thing to inspect when property difficulty barely moves. If that ratio is small, the formula is receiving a weak property signal.

`property_ref_from_min_rps` shows the smooth low-traffic component:

```text
minExpectedRPS * propertyBucketSizeSeconds
```

`property_ref_from_leak` shows the dynamic component:

```text
property-k * property_leak_rate
```

`property_delta` and `difficulty_float` show the pre-rounding values. If these move but `v2_difficulty` does not, the effect is being hidden by byte rounding.

`property_leak_rate_before_add` and `property_leak_rate_after_add` help detect whether `leakRate` is chasing the spike. If `property_ref` rises during the spike, the denominator is dampening the numerator.

## Debug mode

Use `--debug` when a knob appears to have little effect:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug'
```

Useful reading order:

```text
1. property_ratio = property_level / property_ref
2. property_delta = actual property-only difficulty delta before rounding
3. difficulty_float = final V2 difficulty before uint8 rounding
4. property_ref_from_min_rps vs property_ref_from_leak
5. property_leak_rate_before_add vs property_leak_rate_after_add
```

If `property_ratio` stays below about `1`, then with defaults `property-weight=0.25` and `growth=normal`, the property term can only add roughly `0..2` difficulty. That is expected from the formula.

If `property_ref_from_min_rps` is much larger than `property_ref_from_leak`, then low-traffic protection dominates. Decrease `--min-expected-rps`, decrease `--property-bucket`, or increase simulated traffic to make the leak-rate-derived component more visible.

If `property_ref` rises during the spike, the dynamic denominator is adapting upward and suppressing difficulty growth.

## Scenarios

### Baseline: user and property burst together

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst'
```

This is the main sanity check: property pressure should raise the floor, while user pressure should dominate the selected requester's final difficulty.

### User-only pressure

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=user-burst --property=false --user=true'
```

Use this to tune `--user-ref`, `--user-weight`, and `--growth` without property noise.

### Property-only pressure

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true'
```

Use this to tune `--property-k`, `--min-expected-rps`, `--property-bucket`, and `--property-weight`. The property-only curve should usually be mild: it raises the floor but should not punish normal users too heavily.

### Property pressure with a normal selected user

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=true --property=true --user-rate=0.15 --user-contributes-property=true'
```

This approximates a normal user arriving during a property-wide spike.

### Compare old V1 vs new V2

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst --formula=v2'
```

Both V1 and V2 are always plotted. `--formula` only controls the highlighted `selected --formula` line.

## Testing K, the leak-rate component of P8

`K` is passed as `--property-k` or `--k`.

Small `K` makes property pressure more sensitive:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-k=1'
```

Default candidate:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-k=4'
```

Conservative property response:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-k=8'
```

Very conservative property response:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-k=16'
```

Interpretation:

```text
K = 4 means the leak-rate-derived component of P8 is four normal property buckets worth of accumulated excess.
When property_level p ≈ P8, the property term contributes:
  +8 * growth * propertyWeight
```

With defaults `growth=normal` and `property-weight=0.25`, `p≈P8` adds about `+2` difficulty.

## Testing minimum expected RPS

`--min-expected-rps` controls the low-traffic component of `P8`:

```text
Pmin = minExpectedRPS * propertyBucketSizeSeconds
```

More sensitive low-traffic behavior:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --min-expected-rps=0.1'
```

Default:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --min-expected-rps=0.5'
```

Conservative low-traffic behavior:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --min-expected-rps=2.0'
```

## Testing property bucket interval

The bucket interval strongly affects both the leaky-bucket behavior and the low-traffic `P8` component.

Default 5-minute property bucket:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-bucket=5m'
```

More responsive 1-minute property bucket:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-bucket=1m'
```

Very responsive 30-second property bucket:

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-bucket=30s'
```

Remember that changing `--property-bucket` also changes:

```text
Pmin = minExpectedRPS * propertyBucketSizeSeconds
```

So shorter buckets make the low-traffic protection smaller in absolute request-count terms.

## Testing property weight

`property-weight` controls how much normalized property pressure affects each requester.

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-k=4 --property-weight=0.125'
```

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-k=4 --property-weight=0.25'
```

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=property-spike --user=false --property=true --debug --property-k=4 --property-weight=0.5'
```

## Testing growth modes

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst --debug --growth=slow'
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst --debug --growth=normal'
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst --debug --growth=fast'
```

## Testing cross weight

`cross-weight` adds an interaction term so suspicious users during property-wide spikes ramp faster.

```bash
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst --debug --cross-weight=0'
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst --debug --cross-weight=0.05'
make run-difficulty-sim SIMULATION_ARGS='--scenario=both-burst --debug --cross-weight=0.15'
```
