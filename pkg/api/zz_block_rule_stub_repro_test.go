package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

// TestStubPuzzleWithholdsEdgeToken guards against the edge-protection bypass
// where a client could obtain an edge token through the cold-cache stub-puzzle
// flow.
//
// Block rules are enforced only in puzzleHandler, and only when the property is
// present in the request context. When the property-by-sitekey cache is cold,
// PuzzleForRequest returns a verifiable stub puzzle (PuzzleID == 0) and omits
// the property, so puzzleHandler never evaluates the rules. The /verify
// request originates from the customer's backend (or edge), not the end user,
// so block rules cannot be reliably evaluated there either. addEdgeToken
// therefore withholds the edge token for all stub puzzles.
//
// This test reproduces the cold-cache stub flow end-to-end and asserts that:
//  1. A warm-cache /puzzle request with an enabled block rule is blocked (403).
//  2. The cold-cache stub flow still verifies successfully (the captcha was solved).
//  3. No edge token is issued for the stub verification, with or without a block rule.
func TestStubPuzzleWithholdsEdgeToken(t *testing.T) {
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
	}{
		{
			name:      "BlockRuleWithholdsStubEdgeToken",
			withBlock: true,
		},
		{
			name:      "NoBlockRuleStillWithholdsStubEdgeToken",
			withBlock: false,
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
			// No edge token may be issued for a stub puzzle, regardless of block
			// rules, because the /verify request comes from the customer's backend
			// (or edge), not the end user, so block rules cannot be evaluated there.
			if result.EdgeToken != "" {
				t.Fatalf("stub puzzle must not issue an edge token: %+v", result)
			}
		})
	}
}
