package api

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestMinSolveTimeAtMediumUpdate(t *testing.T) {
	p := puzzle.NewComputePuzzle(123, [puzzle.PropertyIDSize]byte{}, 168)
	solutions, err := (&puzzle.ComputeSolver{}).Solve(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := solutions.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(raw[3:7], 1)
	signed, err := p.Serialize(t.Context(), puzzle.NewSalt([]byte("test-salt")), nil)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	encoded.WriteString(base64.StdEncoding.EncodeToString(raw))
	encoded.WriteByte('.')
	if err := signed.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	payload, err := puzzle.ParseVerifyPayload[puzzle.ComputePuzzle](t.Context(), encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	value := "500us"
	env := config.NewEnvConfig(func(name string) string {
		if name == "PC_MIN_SOLVE_TIME_AT_MEDIUM" {
			return value
		}
		return ""
	})
	cfg := config.NewBaseConfig(testsConfigStore())
	item := env.Get(common.MinSolveTimeAtMediumKey)
	cfg.Add(item)
	verifier := NewVerifier(cfg, nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if verifier.MinSolveTimeAtMediumKey != item {
		t.Fatal("verifier did not retain its minimum solve time config item")
	}
	if _, result, err := verifier.verifyPayload(t.Context(), payload, nil, false); err != nil || result != puzzle.VerifyNoError {
		t.Fatalf("initial minimum: result = %v, err = %v", result, err)
	}
	for _, tc := range []struct {
		value string
		want  puzzle.VerifyError
	}{
		{
			value: "500us",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "1.5ms",
			want:  puzzle.SolutionTimingError,
		},
		{
			value: "1ms",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "invalid",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "",
			want:  puzzle.VerifyNoError,
		},
	} {
		value = tc.value
		env.Update(t.Context())
		if err := verifier.Update(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, result, err := verifier.verifyPayload(t.Context(), payload, nil, false); err != nil || result != tc.want {
			t.Fatalf("minimum %q: result = %v, err = %v, want %v", value, result, err, tc.want)
		}
	}
	verifier.MinSolveTimeAtMediumKey = nil
	verifier.updateMinSolveTimeAtMedium(t.Context())
	if _, result, err := verifier.verifyPayload(t.Context(), payload, nil, false); err != nil || result != puzzle.VerifyNoError {
		t.Fatalf("nil timing config: result = %v, err = %v", result, err)
	}
}

func TestVerifySolutionTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	previous := server.Verifier.MinSolveTimeAtMediumKey
	server.Verifier.MinSolveTimeAtMediumKey = config.NewStaticValue(common.MinSolveTimeAtMediumKey, "100ms")
	server.Update(t.Context())
	t.Cleanup(func() {
		server.Verifier.MinSolveTimeAtMediumKey = previous
		server.Update(t.Context())
	})
	for _, compat := range []bool{false, true} {
		for _, elapsed := range []uint32{0, 1, 3600000, ^uint32(0)} {
			t.Run(fmt.Sprintf("Compat%tMillis%d", compat, elapsed), func(t *testing.T) {
				payload, secret, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
				if err != nil {
					t.Fatal(err)
				}
				parts := strings.SplitN(payload, ".", 2)
				solutions, err := base64.StdEncoding.DecodeString(parts[0])
				if err != nil {
					t.Fatal(err)
				}
				binary.LittleEndian.PutUint32(solutions[3:7], elapsed)
				payload = base64.StdEncoding.EncodeToString(solutions) + "." + parts[1]

				for _, expected := range []puzzle.VerifyError{puzzle.SolutionTimingError, puzzle.VerifiedBeforeError} {
					var response *http.Response
					if compat {
						response, err = siteVerifySuite(payload, secret, sitekey)
					} else {
						response, err = verifySuite(payload, secret, sitekey)
					}
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					if response.StatusCode != http.StatusOK {
						t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
					}
					wantSuccess := expected == puzzle.SolutionTimingError
					if compat {
						var result VerifyResponseRecaptchaV2
						if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
							t.Fatal(err)
						}
						if result.Success != wantSuccess || len(result.ErrorCodes) != 1 || result.ErrorCodes[0] != expected.String() {
							t.Fatalf("siteverify response = %+v, want success = %t, code = %v", result, wantSuccess, expected)
						}
					} else {
						var result VerificationResponse
						if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
							t.Fatal(err)
						}
						if result.Success != wantSuccess || result.Code != expected {
							t.Fatalf("verify response = %+v, want success = %t, code = %v", result, wantSuccess, expected)
						}
						if wantSuccess && (result.Timestamp == nil || result.Origin == "") {
							t.Fatalf("timing diagnostic lost puzzle context: %+v", result)
						}
					}
				}
			})
		}
	}
}

func TestVerifyMinSolveTimeAtMediumReload(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	previous := server.Verifier.MinSolveTimeAtMediumKey
	t.Cleanup(func() {
		server.Verifier.MinSolveTimeAtMediumKey = previous
		server.Update(t.Context())
	})
	for _, tc := range []struct {
		value string
		want  puzzle.VerifyError
	}{
		{
			value: "100ms",
			want:  puzzle.SolutionTimingError,
		},
		{
			value: "10ms",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "",
			want:  puzzle.VerifyNoError,
		},
		{
			value: "invalid",
			want:  puzzle.VerifyNoError,
		},
	} {
		t.Run(tc.value, func(t *testing.T) {
			server.Verifier.MinSolveTimeAtMediumKey = config.NewStaticValue(common.MinSolveTimeAtMediumKey, tc.value)
			server.Update(t.Context())
			payload, secret, sitekey, err := setupVerifySuite(t.Context(), t.Name(), dbgen.ApiKeyScopePuzzle)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.SplitN(payload, ".", 2)
			solutions, err := base64.StdEncoding.DecodeString(parts[0])
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint32(solutions[3:7], 30)
			payload = base64.StdEncoding.EncodeToString(solutions) + "." + parts[1]
			response, err := verifySuite(payload, secret, sitekey)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result VerificationResponse
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || !result.Success || result.Code != tc.want {
				t.Fatalf("minimum %q: status = %d, result = %+v, want %v", tc.value, response.StatusCode, result, tc.want)
			}
		})
	}
}

// verifyReducedIntervalCase issues a puzzle with issueValidity, then verifies it
// against a property whose current ValidityInterval is propertyValidity, with the
// client-reported elapsedMillis. The verify time is placed issueValidity before the
// signed expiration plus a small solve delay, so the puzzle's remaining validity is
// (issueValidity - delay). No PostgreSQL is required: the property is served from a
// static cache and the puzzle cache is in-memory.
func verifyReducedIntervalCase(t *testing.T, propertyValidity, issueValidity time.Duration, elapsedMillis uint32) (*puzzle.VerifyResult, time.Time) {
	t.Helper()
	ctx := t.Context()

	cache := db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{})
	property := &dbgen.Property{
		ID:               1,
		ExternalID:       pgtype.UUID{Valid: true, Bytes: [puzzle.PropertyIDSize]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}},
		OrgID:            pgtype.Int4{Valid: true, Int32: 2},
		OrgOwnerID:       pgtype.Int4{Valid: true, Int32: 3},
		Domain:           "interval.example.com",
		ValidityInterval: propertyValidity,
		Enabled:          true,
		Growth:           dbgen.DifficultyGrowthMedium,
		Level:            pgtype.Int2{Valid: true, Int16: int16(common.DifficultyLevelMedium)},
	}
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	if err := cache.Set(ctx, db.PropertyBySitekeyCacheKey(sitekey), property); err != nil {
		t.Fatal(err)
	}
	store := db.NewBusinessEx(nil, cache)

	verifier := NewVerifier(testsConfigStore(), store, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if err := verifier.Update(ctx); err != nil {
		t.Fatal(err)
	}

	p := puzzle.NewComputePuzzle(puzzle.NextPuzzleID(), property.ExternalID.Bytes, 0)
	if err := p.Init(issueValidity); err != nil {
		t.Fatal(err)
	}
	signed, err := p.Serialize(ctx, verifier.Salt.Value(), nil)
	if err != nil {
		t.Fatal(err)
	}
	solutions, err := (&puzzle.ComputeSolver{}).Solve(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	encoded.WriteString(solutions.String())
	encoded.WriteByte('.')
	if err := signed.Write(&encoded); err != nil {
		t.Fatal(err)
	}

	parts := strings.SplitN(encoded.String(), ".", 2)
	solutionsBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(solutionsBytes[3:7], elapsedMillis)
	payloadStr := base64.StdEncoding.EncodeToString(solutionsBytes) + "." + parts[1]
	payload, err := verifier.ParseSolutionPayload(ctx, []byte(payloadStr))
	if err != nil {
		t.Fatal(err)
	}

	// Reconstruct the (truncated) issuance time from the signed expiration and the
	// validity used at issuance, then verify a few seconds later to model the time
	// the client spends solving before submitting.
	issuedAt := p.Expiration().Add(-issueValidity)
	tnow := issuedAt.Add(3 * time.Second)
	result, _, err := verifier.Verify(ctx, payload, argonOwner{property.OrgOwnerID.Int32}, tnow)
	if err != nil {
		t.Fatalf("Verify returned an error: %v", err)
	}
	return result, tnow
}

// TestVerifyValidityIntervalReducedBetweenIssueAndVerify reproduces the bug where a
// property owner lowering the validity interval mid-window makes the verifier infer
// a CreatedAt in the future. The fix skips the solution-timing heuristic whenever the
// inferred CreatedAt is unreliable (in the future), so legitimate solves verify
// cleanly, and pins the recorded CreatedAt to the conservative MaxValidityPeriod
// bound so the API never exposes a future challenge_ts.
func TestVerifyValidityIntervalReducedBetweenIssueAndVerify(t *testing.T) {
	t.Parallel()
	const issueValidity = 6 * time.Hour

	t.Run("ReducedIntervalLegitimateSolve", func(t *testing.T) {
		t.Parallel()
		result, tnow := verifyReducedIntervalCase(t, 5*time.Minute, issueValidity, 100)
		if result.Error != puzzle.VerifyNoError {
			t.Fatalf("legitimate solve after interval reduction: error = %v (%d), want %v",
				result.Error, result.Error, puzzle.VerifyNoError)
		}
		if result.CreatedAt.IsZero() {
			t.Fatal("CreatedAt is zero; the verify record would be dropped from analytics")
		}
		if result.CreatedAt.After(tnow) {
			t.Fatalf("inferred CreatedAt %v is in the future (now %v); API would expose a future challenge_ts",
				result.CreatedAt, tnow)
		}
		if !result.Valid() {
			t.Fatalf("verification result is not reportable: %+v", result)
		}
		// The /verify response surfaces result.CreatedAt as the timestamp; it must
		// not drift into the future for solves that should be accepted.
		response := newVerificationResponse(result, false)
		if !response.Success || response.Timestamp == nil || time.Time(*response.Timestamp).After(tnow) {
			t.Fatalf("verification response exposes a missing/future timestamp: %+v", response)
		}
	})

	t.Run("UnchangedIntervalLegitimateSolve", func(t *testing.T) {
		t.Parallel()
		// Control: with the interval unchanged, the inference and maxSolveTime must
		// behave exactly as before (no fallback), so a sub-age solve verifies cleanly.
		result, tnow := verifyReducedIntervalCase(t, issueValidity, issueValidity, 100)
		if result.Error != puzzle.VerifyNoError {
			t.Fatalf("legitimate solve with unchanged interval: error = %v, want %v", result.Error, puzzle.VerifyNoError)
		}
		if result.CreatedAt.IsZero() || result.CreatedAt.After(tnow) {
			t.Fatalf("CreatedAt = %v, now %v", result.CreatedAt, tnow)
		}
	})

	t.Run("UnchangedIntervalFlagsSolveExceedingAge", func(t *testing.T) {
		t.Parallel()
		// Control: with the interval unchanged, a client-reported elapsed that
		// exceeds the puzzle's inferred age is still rejected by the heuristic.
		result, _ := verifyReducedIntervalCase(t, issueValidity, issueValidity, 10_000 /* 10s > 3s age */)
		if result.Error != puzzle.SolutionTimingError {
			t.Fatalf("solve exceeding puzzle age with unchanged interval: error = %v, want %v",
				result.Error, puzzle.SolutionTimingError)
		}
	})
}

// TestVerifyCreatedAtNeverInFutureAcrossIntervals sweeps the property's current
// validity interval across every discrete ValidityDurations option while the puzzle
// was issued with a larger one, and asserts the inferred CreatedAt never lands in the
// future for a legitimate, sub-age solve.
func TestVerifyCreatedAtNeverInFutureAcrossIntervals(t *testing.T) {
	t.Parallel()
	const issueValidity = 6 * time.Hour
	for _, propertyValidity := range puzzle.ValidityDurations {
		propertyValidity := propertyValidity
		// Remaining validity is issueValidity - 3s, which exceeds every interval
		// below 6h, so each of these cases forces the fallback path.
		if propertyValidity >= issueValidity {
			continue
		}
		t.Run(propertyValidity.String(), func(t *testing.T) {
			t.Parallel()
			result, tnow := verifyReducedIntervalCase(t, propertyValidity, issueValidity, 100)
			if result.Error != puzzle.VerifyNoError {
				t.Fatalf("error = %v, want %v", result.Error, puzzle.VerifyNoError)
			}
			if result.CreatedAt.IsZero() {
				t.Fatal("CreatedAt is zero")
			}
			if result.CreatedAt.After(tnow) {
				t.Fatalf("CreatedAt %v is in the future (now %v) for property interval %v",
					result.CreatedAt, tnow, propertyValidity)
			}
		})
	}
}
