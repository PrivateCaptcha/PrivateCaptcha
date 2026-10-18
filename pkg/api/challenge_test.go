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
		name           string
		selected       dbgen.ChallengeType
		logical        uint8
		captchaVersion string
		argonEnabled   bool
		challenge      puzzle.Challenge
		wire           uint8
		fallback       bool
		invalid        bool
	}{
		{name: "Blake", selected: dbgen.ChallengeTypeBlake2b, logical: 136, challenge: puzzle.ChallengeBlake2b, wire: 136},
		{name: "LegacyCache", selected: "", logical: 152, challenge: puzzle.ChallengeBlake2b, wire: 152},
		{name: "LegacyArgon", selected: dbgen.ChallengeTypeArgon2ID, logical: 152, captchaVersion: "1", argonEnabled: true, challenge: puzzle.ChallengeBlake2b, wire: 152, fallback: true},
		{name: "LowArgon", selected: dbgen.ChallengeTypeArgon2ID, logical: 151, argonEnabled: true, challenge: puzzle.ChallengeBlake2b, wire: 151, fallback: true},
		{name: "ArgonZero", selected: dbgen.ChallengeTypeArgon2ID, logical: 128, argonEnabled: true, challenge: puzzle.ChallengeBlake2b, wire: 128, fallback: true},
		{name: "ArgonMinimum", selected: dbgen.ChallengeTypeArgon2ID, logical: 152, argonEnabled: true, challenge: puzzle.ChallengeArgon2ID, wire: 24},
		{name: "ArgonMedium", selected: dbgen.ChallengeTypeArgon2ID, logical: 160, argonEnabled: true, challenge: puzzle.ChallengeArgon2ID, wire: 32},
		{name: "ArgonHigh", selected: dbgen.ChallengeTypeArgon2ID, logical: 168, argonEnabled: true, challenge: puzzle.ChallengeArgon2ID, wire: 40},
		{name: "DisabledArgon", selected: dbgen.ChallengeTypeArgon2ID, logical: 152, challenge: puzzle.ChallengeBlake2b, wire: 152},
		{name: "Unknown", selected: "unknown", logical: 152, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			challenge, wire, fallback, err := selectPropertyChallenge(tc.selected, tc.logical, tc.captchaVersion, tc.argonEnabled)
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
	cfg := config.NewBaseConfig(testsConfigStore())
	budget := &budgetConfigItem{value: "256"}
	cfg.Add(budget)
	verifier := NewVerifier(cfg, nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		logical   uint8
		version   string
		budget    string
		challenge puzzle.Challenge
		wire      uint8
	}{
		{name: "Legacy", logical: 152, version: "1", budget: "256", challenge: puzzle.ChallengeBlake2b, wire: 152},
		{name: "Unspecified", logical: 152, budget: "256", challenge: puzzle.ChallengeArgon2ID, wire: 24},
		{name: "Extended", logical: 152, version: "2", budget: "256", challenge: puzzle.ChallengeArgon2ID, wire: 24},
		{name: "Cutover", logical: 151, version: "2", budget: "256", challenge: puzzle.ChallengeBlake2b, wire: 151},
		{name: "Disabled", logical: 152, budget: "0", challenge: puzzle.ChallengeBlake2b, wire: 152},
		{name: "Missing", logical: 152, budget: "", challenge: puzzle.ChallengeBlake2b, wire: 152},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget.value = tc.budget
			verifier.UpdateMemoryBudget(t.Context())
			levels := difficulty.NewLevelsEx(db.NewMemoryTimeSeries(), fixedPuzzleDifficulty(tc.logical), 1, time.Minute)
			ctx := context.WithValue(t.Context(), common.PropertyContextKey, property)
			ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.1"))
			req := httptest.NewRequest("GET", "/puzzle", nil).WithContext(ctx)
			if tc.version != "" {
				req.Header.Set(common.HeaderCaptchaVersion, tc.version)
			}
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

// TestPuzzleForRequestEdgeTokenGating asserts that attaching edge-token validity
// (which upgrades the wire format to v3) is gated on the client's declared captcha
// version. Clients announcing "1" (the legacy/standard-widget wire version, which
// pre-v3 widget bundles also send) must receive a payload they can parse, so the
// server must NOT bump the wire format to v3 just because the property has edge
// tokens enabled. Clients announcing anything else (or omitting the header) are
// treated as v3-capable and receive a v3 payload carrying the validity field.
func TestPuzzleForRequestEdgeTokenGating(t *testing.T) {
	property := &dbgen.Property{
		ID:                        1,
		ExternalID:                pgtype.UUID{Valid: true, Bytes: [16]byte{1}},
		OrgID:                     pgtype.Int4{Valid: true, Int32: 2},
		OrgOwnerID:                pgtype.Int4{Valid: true, Int32: 3},
		Level:                     pgtype.Int2{Valid: true, Int16: int16(common.DifficultyLevelMedium)},
		Growth:                    dbgen.DifficultyGrowthMedium,
		ValidityInterval:          time.Hour,
		Challenge:                 dbgen.ChallengeTypeArgon2ID,
		EdgeTokenValidityInterval: time.Hour,
	}
	cfg := config.NewBaseConfig(testsConfigStore())
	budget := &budgetConfigItem{value: "256"}
	cfg.Add(budget)
	verifier := NewVerifier(cfg, nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		version       string // "" means the header is omitted entirely
		challenge     dbgen.ChallengeType
		wantChallenge puzzle.Challenge
		wantWire      uint8
		wantValidity  time.Duration
	}{
		{
			name:          "Legacy",
			version:       "1",
			challenge:     dbgen.ChallengeTypeArgon2ID,
			wantChallenge: puzzle.ChallengeBlake2b, // downgraded for legacy clients
			wantWire:      1,                       // must stay on v1 (pre-v3 widgets cannot parse v3)
			wantValidity:  0,                       // v1 never carries the edge-token validity field
		},
		{
			name:          "Unspecified",
			version:       "",
			challenge:     dbgen.ChallengeTypeArgon2ID,
			wantChallenge: puzzle.ChallengeArgon2ID,
			wantWire:      3,         // v3-capable: edge-token validity bumps v2 -> v3
			wantValidity:  time.Hour, // validity is carried in the v3 payload
		},
		{
			name:          "Extended",
			version:       "2",
			challenge:     dbgen.ChallengeTypeArgon2ID,
			wantChallenge: puzzle.ChallengeArgon2ID,
			wantWire:      3,
			wantValidity:  time.Hour,
		},
		{
			name:          "Future",
			version:       "3",
			challenge:     dbgen.ChallengeTypeArgon2ID,
			wantChallenge: puzzle.ChallengeArgon2ID,
			wantWire:      3,
			wantValidity:  time.Hour,
		},
		{
			name:          "LegacyBlakeProperty",
			version:       "1",
			challenge:     dbgen.ChallengeTypeBlake2b,
			wantChallenge: puzzle.ChallengeBlake2b,
			wantWire:      1,
			wantValidity:  0,
		},
		{
			name:          "BlakePropertyExtended",
			version:       "2",
			challenge:     dbgen.ChallengeTypeBlake2b,
			wantChallenge: puzzle.ChallengeBlake2b,
			wantWire:      3, // v3 bump applies to blake2b properties too for v3-capable clients
			wantValidity:  time.Hour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			property.Challenge = tc.challenge
			levels := difficulty.NewLevelsEx(db.NewMemoryTimeSeries(), fixedPuzzleDifficulty(152), 1, time.Minute)
			ctx := context.WithValue(t.Context(), common.PropertyContextKey, property)
			ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.1"))
			req := httptest.NewRequest("GET", "/puzzle", nil).WithContext(ctx)
			if tc.version != "" {
				req.Header.Set(common.HeaderCaptchaVersion, tc.version)
			}
			issued, _, err := verifier.PuzzleForRequest(req, levels, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if issued.Challenge() != tc.wantChallenge {
				t.Fatalf("challenge = %d, want %d", issued.Challenge(), tc.wantChallenge)
			}
			if got := issued.WireVersion(); got != tc.wantWire {
				t.Fatalf("wire version = %d, want %d (legacy clients must not receive a v3 payload)", got, tc.wantWire)
			}
			if got := issued.EdgeTokenValidity(); got != tc.wantValidity {
				t.Fatalf("edge-token validity = %v, want %v", got, tc.wantValidity)
			}
			body, err := issued.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if len(body) < 1 || body[0] != tc.wantWire {
				t.Fatalf("on-wire version byte = %d (body=%x), want %d", func() byte {
					if len(body) > 0 {
						return body[0]
					}
					return 0
				}(), body, tc.wantWire)
			}
		})
	}

	// When edge tokens are disabled, no client receives a v3 bump regardless of
	// the announced captcha version: this guards against regressing the gate's
	// EdgeTokenValidityInterval > 0 precondition.
	property.Challenge = dbgen.ChallengeTypeArgon2ID
	property.EdgeTokenValidityInterval = 0
	levels := difficulty.NewLevelsEx(db.NewMemoryTimeSeries(), fixedPuzzleDifficulty(152), 1, time.Minute)
	ctx := context.WithValue(t.Context(), common.PropertyContextKey, property)
	ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, netip.MustParseAddr("192.0.2.1"))
	req := httptest.NewRequest("GET", "/puzzle", nil).WithContext(ctx)
	req.Header.Set(common.HeaderCaptchaVersion, "2")
	issued, _, err := verifier.PuzzleForRequest(req, levels, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := issued.WireVersion(); got == puzzle.PuzzleVersion3 {
		t.Fatalf("edge-token-disabled property bumped wire format to v3: %d", got)
	}
	if got := issued.EdgeTokenValidity(); got != 0 {
		t.Fatalf("edge-token-disabled property carried edge-token validity: %v", got)
	}
}
