package api

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestChallengeMapping(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selected  dbgen.ChallengeType
		logical   uint8
		challenge puzzle.Challenge
		wire      uint8
		fallback  bool
		invalid   bool
	}{
		{name: "Blake", selected: dbgen.ChallengeTypeBlake2b, logical: 136, challenge: puzzle.ChallengeBlake2b, wire: 136},
		{name: "LegacyCache", selected: "", logical: 152, challenge: puzzle.ChallengeBlake2b, wire: 152},
		{name: "LowArgon", selected: dbgen.ChallengeTypeArgon2ID, logical: 151, challenge: puzzle.ChallengeBlake2b, wire: 151, fallback: true},
		{name: "ArgonZero", selected: dbgen.ChallengeTypeArgon2ID, logical: 128, challenge: puzzle.ChallengeBlake2b, wire: 128, fallback: true},
		{name: "ArgonMinimum", selected: dbgen.ChallengeTypeArgon2ID, logical: 152, challenge: puzzle.ChallengeArgon2ID, wire: 24},
		{name: "ArgonMedium", selected: dbgen.ChallengeTypeArgon2ID, logical: 160, challenge: puzzle.ChallengeArgon2ID, wire: 32},
		{name: "ArgonHigh", selected: dbgen.ChallengeTypeArgon2ID, logical: 168, challenge: puzzle.ChallengeArgon2ID, wire: 40},
		{name: "Unknown", selected: "unknown", logical: 152, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			challenge, wire, fallback, err := selectPropertyChallenge(tc.selected, tc.logical)
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v, want invalid = %t", err, tc.invalid)
			}
			if !tc.invalid && (challenge != tc.challenge || wire != tc.wire || fallback != tc.fallback) {
				t.Fatalf("mapping = (%d,%d,%t), want (%d,%d,%t)", challenge, wire, fallback, tc.challenge, tc.wire, tc.fallback)
			}
		})
	}
}

type fixedPuzzleDifficulty uint8

func (a fixedPuzzleDifficulty) Difficulty(*leakybucket.AddResult, *leakybucket.AddResult, difficulty.Property, float64) uint8 {
	return uint8(a)
}

func TestPuzzleForRequestChallenge(t *testing.T) {
	property := &dbgen.Property{
		ID:               1,
		ExternalID:       pgtype.UUID{Valid: true, Bytes: [16]byte{1}},
		OrgID:            pgtype.Int4{Valid: true, Int32: 2},
		OrgOwnerID:       pgtype.Int4{Valid: true, Int32: 3},
		Level:            pgtype.Int2{Valid: true, Int16: int16(common.DifficultyLevelMedium)},
		Growth:           dbgen.DifficultyGrowthMedium,
		ValidityInterval: time.Hour,
		Challenge:        dbgen.ChallengeTypeArgon2ID,
	}
	verifier := NewVerifier(testsConfigStore(), nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		logical   uint8
		challenge puzzle.Challenge
		wire      uint8
	}{
		{name: "Argon", logical: 152, challenge: puzzle.ChallengeArgon2ID, wire: 24},
		{name: "Cutover", logical: 151, challenge: puzzle.ChallengeBlake2b, wire: 151},
	} {
		t.Run(tc.name, func(t *testing.T) {
			levels := difficulty.NewLevelsEx(db.NewMemoryTimeSeries(), fixedPuzzleDifficulty(tc.logical), 1, time.Minute)
			ctx := context.WithValue(t.Context(), common.PropertyContextKey, property)
			ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.1"))
			req := httptest.NewRequest("GET", "/puzzle", nil).WithContext(ctx)
			issued, selected, err := verifier.PuzzleForRequest(req, levels, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if selected != property || issued.Challenge() != tc.challenge || issued.Difficulty() != tc.wire {
				t.Fatalf("issued challenge = %d difficulty = %d, want %d %d", issued.Challenge(), issued.Difficulty(), tc.challenge, tc.wire)
			}
			count := 24
			if tc.challenge == puzzle.ChallengeArgon2ID {
				count = puzzle.Argon2IDSolutionsCount
			}
			if issued.SolutionsCount() != count {
				t.Fatalf("solutions = %d, want %d", issued.SolutionsCount(), count)
			}
		})
	}
	property.Challenge = "unsupported"
	ctx := context.WithValue(t.Context(), common.PropertyContextKey, property)
	ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.1"))
	levels := difficulty.NewLevelsEx(db.NewMemoryTimeSeries(), fixedPuzzleDifficulty(152), 1, time.Minute)
	if issued, _, err := verifier.PuzzleForRequest(httptest.NewRequest("GET", "/puzzle", nil).WithContext(ctx), levels, nil, nil); err == nil || issued != nil {
		t.Fatalf("invalid property challenge issued %v with error %v", issued, err)
	}
	stubCtx := context.WithValue(t.Context(), common.SitekeyContextKey, db.UUIDToSiteKey(property.ExternalID))
	stub, selected, err := verifier.PuzzleForRequest(httptest.NewRequest("GET", "/puzzle", nil).WithContext(stubCtx), nil, nil, nil)
	if err != nil || selected != nil || stub.Challenge() != puzzle.ChallengeBlake2b || stub.SolutionsCount() != 16 {
		t.Fatalf("stub puzzle = %v, selected = %v, error = %v", stub, selected, err)
	}
}
