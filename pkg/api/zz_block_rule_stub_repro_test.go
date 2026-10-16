package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

// TestStubPuzzleEdgeTokenWithBlockRules guards against the edge-protection bypass
// where a client could obtain an edge token through the cold-cache stub-puzzle
// flow when block rules are configured for the property.
//
// Block rules are enforced only in puzzleHandler, and only when the property is
// present in the request context. When the property-by-sitekey cache is cold,
// PuzzleForRequest returns a verifiable stub puzzle (PuzzleID == 0) and omits
// the property, so puzzleHandler never evaluates the rules. The /verify
// request originates from the customer's backend (or edge), not the end user,
// so block rules cannot be evaluated against the actual client at verify time.
// addEdgeToken therefore checks whether the property has any enabled block
// rules (without evaluating them against the request): if it does, the edge
// token is withheld for the stub because we cannot determine whether the real
// client would have been blocked.
//
// This test reproduces the cold-cache stub flow end-to-end and asserts that:
//  1. A warm-cache /puzzle request with an enabled block rule is blocked (403).
//  2. The cold-cache stub flow still verifies successfully (the captcha was solved).
//  3. With a block rule configured, no edge token is issued for the stub.
//  4. Without a block rule, the stub still issues an edge token using the
//     property's configured validity (the normal cold-cache path).
func TestStubPuzzleEdgeTokenWithBlockRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := t.Context()
	previous := server.EdgeTokens
	server.EdgeTokens = newTestEdgeSigner(t)
	t.Cleanup(func() { server.EdgeTokens = previous })

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := store.Impl().CreateAPIKey(ctx, user, db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), time.Hour, 10))
	if err != nil {
		t.Fatal(err)
	}
	secret := db.UUIDToSecret(key.ExternalID)

	for _, tc := range []struct {
		name      string
		withBlock bool
		wantToken bool
	}{
		{
			name:      "BlockRuleWithholdsStubEdgeToken",
			withBlock: true,
			wantToken: false,
		},
		{
			name:      "NoBlockRuleIssuesStubEdgeToken",
			withBlock: false,
			wantToken: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := db_tests.CreateNewPropertyParams(user.ID, "block-stub-"+tc.name+".example.com")
			params.EdgeTokenValidityInterval = time.Hour
			property, _, err := store.Impl().CreateNewProperty(ctx, params, org)
			if err != nil {
				t.Fatal(err)
			}
			if tc.withBlock {
				if _, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
					Name:              "Block all requests for " + tc.name,
					PropertyID:        db.Int(property.ID),
					CreatorID:         db.Int(user.ID),
					Enabled:           true,
					ConditionProperty: dbgen.RuleConditionPropertyAlways,
					ConditionOperator: dbgen.RuleConditionOperatorEquals,
					ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
				}); err != nil {
					t.Fatal(err)
				}
				if err := server.Auth.backfillRulesImpl(ctx, map[int32]uint{property.ID: 1}); err != nil {
					t.Fatal(err)
				}
			}

			sitekey := db.UUIDToSiteKey(property.ExternalID)
			cacheKey := db.PropertyBySitekeyCacheKey(sitekey)

			// Warm the property cache so the next /puzzle request hits the warm-cache
			// branch in puzzleHandler, where the property IS in the request context
			// and block rules are enforced.
			warmed, err := store.Impl().RetrievePropertyBySitekey(ctx, sitekey)
			if err != nil {
				t.Fatal(err)
			}
			if warmed == nil {
				t.Fatal("warming cache returned nil property")
			}

			warmResp, err := puzzleSuite(ctx, sitekey, property.Domain)
			if err != nil {
				t.Fatal(err)
			}
			defer warmResp.Body.Close()
			if tc.withBlock {
				if warmResp.StatusCode != http.StatusForbidden {
					t.Fatalf("warm-cache /puzzle with enabled block rule: status = %d, want %d (block rules must be enforced when property is cached)", warmResp.StatusCode, http.StatusForbidden)
				}
			} else {
				if warmResp.StatusCode != http.StatusOK {
					t.Fatalf("warm-cache /puzzle without block rule: status = %d, want %d", warmResp.StatusCode, http.StatusOK)
				}
			}

			// Force the cold-cache /puzzle flow: evict the cached property so the
			// Sitekey middleware misses, queues an async refresh, and continues
			// without a property in context - causing PuzzleForRequest to return a
			// stub puzzle (PuzzleID == 0) that bypasses the block-rule check in
			// puzzleHandler.
			cache.Delete(ctx, cacheKey)
			puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
			if err != nil {
				t.Fatal(err)
			}

			proof := solutionsStr + "." + puzzleStr
			parsed, err := server.Verifier.ParseSolutionPayload(ctx, []byte(proof))
			if err != nil {
				t.Fatal(err)
			}
			if got := parsed.Puzzle().PuzzleID(); got != 0 {
				t.Fatalf("cold-cache /puzzle must return a stub puzzle (PuzzleID == 0), got %d", got)
			}

			// Evict again so /verify re-fetches the property from the DB - the real
			// branch exercised by the cold-start window - rather than the
			// async-refreshed cache populated by the previous /puzzle.
			cache.Delete(ctx, cacheKey)

			resp, err := verifySuite(proof, secret, sitekey)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("verify status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			var result VerificationResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			// Stub verification itself must still succeed: the captcha was solved.
			if !result.Success {
				t.Fatalf("cold-cache stub-puzzle verification must still succeed: %+v", result)
			}
			if tc.wantToken {
				parts := strings.Split(result.EdgeToken, ".")
				if len(parts) != 3 {
					t.Fatalf("stub flow without a block rule must still issue an edge token: %+v", result)
				}
				if got := edgeTokenLifetimeSeconds(result.EdgeToken); got != int64(time.Hour/time.Second) {
					t.Errorf("stub edge token validity = %ds, want %ds", got, int64(time.Hour/time.Second))
				}
			} else {
				if result.EdgeToken != "" {
					t.Fatalf("stub flow with a block rule must not issue an edge token: %+v", result)
				}
			}
		})
	}
}

// edgeTokenLifetimeSeconds decodes the JWT payload and returns exp-iat in
// seconds. It returns -1 on malformed input.
func edgeTokenLifetimeSeconds(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return -1
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return -1
	}
	var claims struct {
		IssuedAt  int64 `json:"iat"`
		ExpiresAt int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return -1
	}
	return claims.ExpiresAt - claims.IssuedAt
}
