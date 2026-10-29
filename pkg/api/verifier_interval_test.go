package api

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

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
// a CreatedAt in the future, producing a negative maxSolveTime that flags every
// legitimate submission as solution-timing. With the fix, the inferred CreatedAt is
// pulled back to MaxValidityPeriod for any unexpired puzzle, so legitimate solves
// verify cleanly while genuinely excessive solve times are still caught.
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

	t.Run("ReducedIntervalStillFlagsExcessiveSolveTime", func(t *testing.T) {
		t.Parallel()
		// 25h reported elapsed exceeds even the conservative (24h-based) maxSolveTime,
		// proving the fix does not simply disable the timing heuristic.
		result, tnow := verifyReducedIntervalCase(t, 5*time.Minute, issueValidity, uint32(25*time.Hour/time.Millisecond))
		if result.Error != puzzle.SolutionTimingError {
			t.Fatalf("excessive solve after interval reduction: error = %v, want %v", result.Error, puzzle.SolutionTimingError)
		}
		if result.CreatedAt.After(tnow) {
			t.Fatalf("inferred CreatedAt %v is in the future (now %v)", result.CreatedAt, tnow)
		}
		// solution-timing is still a client-side success, mirroring Solution() semantics.
		if !result.Success() {
			t.Fatalf("solution-timing result should remain a client success: %+v", result)
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

// TestVerifyHTTPValidityIntervalReducedMidWindow issues a puzzle through the real
// /puzzle HTTP handler, lowers the property's validity interval via the DB update
// path (mirroring the owner's dashboard action), evicts the sitekey cache so /verify
// re-reads the updated property, then solves and posts to /verify and /siteverify.
// Asserts HTTP 200, success, code 0 (no solution-timing), and a non-future
// challenge_ts/timestamp. Requires PostgreSQL (the global store/server).
func TestVerifyHTTPValidityIntervalReducedMidWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	// Issue with the default 6h interval so the puzzle's remaining validity exceeds
	// the lowered (5m) interval at verify time, reproducing the bug's trigger.
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "interval-http.example.com"), org)
	if err != nil {
		t.Fatal(err)
	}
	sitekey := db.UUIDToSiteKey(property.ExternalID)

	// Issue a distinct puzzle per verification endpoint BEFORE lowering the validity
	// interval, so each subtest verifies a separate (non-replayed) puzzle that was
	// issued under the original 6h window.
	proofs := make(map[string]string)
	for _, name := range []string{"Verify", "SiteVerify"} {
		puzzleStr, solutionsStr, perr := solutionsSuite(ctx, sitekey, property.Domain)
		if perr != nil {
			t.Fatalf("issue %s puzzle: %v", name, perr)
		}
		proofs[name] = solutionsStr + "." + puzzleStr
	}

	// Lower the property's validity interval to 5m after issuance, mirroring the
	// owner's dashboard action, and evict the cached property so /verify re-reads it.
	if _, _, err := store.Impl().UpdateProperty(ctx, org, user, &dbgen.UpdatePropertyParams{
		ID:                        property.ID,
		Name:                      property.Name,
		Level:                     property.Level,
		Growth:                    property.Growth,
		ValidityInterval:          5 * time.Minute,
		MaxReplayCount:            property.MaxReplayCount,
		AllowSubdomains:           property.AllowSubdomains,
		AllowLocalhost:            property.AllowLocalhost,
		EdgeTokenValidityInterval: property.EdgeTokenValidityInterval,
	}); err != nil {
		t.Fatalf("UpdateProperty: %v", err)
	}
	cache.Delete(ctx, db.PropertyBySitekeyCacheKey(sitekey))

	keyParams := db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0)
	keyParams.Scope = dbgen.ApiKeyScopePuzzle
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}
	secret := db.UUIDToSecret(apikey.ExternalID)

	tnow := time.Now().UTC()
	verifyWrapper := func(response, secret, sitekey string, _ ...map[string][]string) (*http.Response, error) {
		return verifySuite(response, secret, sitekey)
	}
	for _, tc := range []struct {
		name    string
		do      func(string, string, string, ...map[string][]string) (*http.Response, error)
		recaptc bool
	}{
		{"Verify", verifyWrapper, false},
		{"SiteVerify", siteVerifySuite, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := tc.do(proofs[tc.name], secret, sitekey)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if tc.recaptc {
				var rr VerifyResponseRecaptchaV2
				if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
					t.Fatal(err)
				}
				if !rr.Success || len(rr.ErrorCodes) != 0 {
					t.Fatalf("reduced-interval siteverify must succeed with no error codes, got success=%v error_codes=%v",
						rr.Success, rr.ErrorCodes)
				}
				if ts := time.Time(rr.ChallengeTS); ts.After(tnow.Add(time.Second)) {
					t.Fatalf("siteverify challenge_ts %v is in the future (now %v)", ts, tnow)
				}
			} else {
				var result VerificationResponse
				if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
					t.Fatal(err)
				}
				if !result.Success || result.Code != puzzle.VerifyNoError {
					t.Fatalf("reduced-interval verify must succeed with code 0, got success=%v code=%v (%s)",
						result.Success, result.Code, result.Code.String())
				}
				if result.Timestamp == nil {
					t.Fatalf("verify response missing timestamp: %+v", result)
				}
				if ts := time.Time(*result.Timestamp); ts.After(tnow.Add(time.Second)) {
					t.Fatalf("verify response timestamp %v is in the future (now %v)", ts, tnow)
				}
			}
		})
	}
}
