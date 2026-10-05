package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
)

// nonTestPropertyUUID is a UUID that is NOT db.TestPropertyUUID, so the
// TestPropertySitekey short-circuit in GetCachedPropertyBySitekey does not
// fire and the cache lookup is exercised directly.
var nonTestPropertyUUID = db.UUIDFromString("0123456789abcdef0123456789abcdef")

func newSitekeyMiddleware(cache common.Cache[db.CacheKey, any]) *AuthMiddleware {
	store := db.NewBusinessWithQuerier(nil, &db.QuerierStub{}, cache)
	return &AuthMiddleware{
		Store:               store,
		Limiter:             errUserLimiter{},
		SitekeyChan:         make(chan string, 1),
		backpressureTimeout: time.Second,
		Metrics:             monitoring.NewStub(),
	}
}

func newNonTestProperty() *dbgen.Property {
	return &dbgen.Property{
		ID:         789,
		OrgOwnerID: db.Int(7),
		OrgID:      db.Int(8),
		ExternalID: nonTestPropertyUUID,
		Domain:     "example.com",
		Enabled:    true,
	}
}

// TestSitekeyMiddlewareNormalizesUppercaseSitekey mirrors
// TestAuthMiddlewareFormNormalizesExternalIDToLowerCase for the Sitekey path:
// an uppercase / mixed-case sitekey that decodes to the same UUID as the
// canonical lowercase entry must hit the warm lowercase cache entry, must be
// canonicalized to lowercase when stored in SitekeyContextKey, and must
// enqueue the backfill using the lowercase key.
func TestSitekeyMiddlewareNormalizesUppercaseSitekey(t *testing.T) {
	property := newNonTestProperty()
	lowerSitekey := db.UUIDToSiteKey(property.ExternalID)
	upperSitekey := strings.ToUpper(lowerSitekey)
	if !db.CanBeValidSitekey(upperSitekey) {
		t.Fatalf("uppercase sitekey should pass CanBeValidSitekey")
	}
	if got := db.UUIDFromSiteKey(upperSitekey); got != property.ExternalID {
		t.Fatalf("uppercase sitekey should decode to the same UUID")
	}

	origin := "https://" + property.Domain

	t.Run("CacheHitWithLowercase", func(t *testing.T) {
		cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
		if err := cache.Set(context.Background(), db.PropertyBySitekeyCacheKey(lowerSitekey), property); err != nil {
			t.Fatalf("Failed to seed cache: %v", err)
		}
		am := newSitekeyMiddleware(cache)

		var capturedProperty *dbgen.Property
		var capturedSitekey string
		handler := am.Sitekey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedProperty, _ = r.Context().Value(common.PropertyContextKey).(*dbgen.Property)
			capturedSitekey, _ = r.Context().Value(common.SitekeyContextKey).(string)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/"+common.PuzzleEndpoint+"?"+common.ParamSiteKey+"="+lowerSitekey, nil)
		req.Header.Set(common.HeaderOrigin, origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if capturedProperty == nil {
			t.Fatal("PropertyContextKey not set; expected cache hit for lowercase sitekey")
		}
		if capturedProperty.ID != property.ID {
			t.Fatalf("PropertyContextKey.ID = %d, want %d", capturedProperty.ID, property.ID)
		}
		if capturedSitekey != "" {
			t.Fatalf("SitekeyContextKey = %q, want empty on cache hit (property takes precedence)", capturedSitekey)
		}
		select {
		case refreshed := <-am.SitekeyChan:
			t.Fatalf("did not expect refresh backfill on cache hit, got %q", refreshed)
		default:
		}
	})

	t.Run("CacheHitWithUppercaseDespiteLowercaseEntry", func(t *testing.T) {
		cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
		if err := cache.Set(context.Background(), db.PropertyBySitekeyCacheKey(lowerSitekey), property); err != nil {
			t.Fatalf("Failed to seed cache: %v", err)
		}
		am := newSitekeyMiddleware(cache)

		var capturedProperty *dbgen.Property
		var capturedSitekey string
		handler := am.Sitekey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedProperty, _ = r.Context().Value(common.PropertyContextKey).(*dbgen.Property)
			capturedSitekey, _ = r.Context().Value(common.SitekeyContextKey).(string)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/"+common.PuzzleEndpoint+"?"+common.ParamSiteKey+"="+upperSitekey, nil)
		req.Header.Set(common.HeaderOrigin, origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (uppercase sitekey should hit the lowercase cache entry after normalization)", w.Code, http.StatusOK)
		}
		if capturedProperty == nil {
			t.Fatal("PropertyContextKey not set; expected cache hit after normalizing uppercase sitekey to the lowercase cache key")
		}
		if capturedProperty.ID != property.ID {
			t.Fatalf("PropertyContextKey.ID = %d, want %d", capturedProperty.ID, property.ID)
		}
		if capturedSitekey != "" {
			t.Fatalf("SitekeyContextKey = %q, want empty on cache hit (property takes precedence)", capturedSitekey)
		}
		select {
		case refreshed := <-am.SitekeyChan:
			t.Fatalf("did not expect refresh backfill on cache hit even with uppercase input, got %q", refreshed)
		default:
		}
	})

	t.Run("CacheMissCanonicalizesContextValueAndBackfill", func(t *testing.T) {
		cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
		am := newSitekeyMiddleware(cache)

		var capturedProperty *dbgen.Property
		var capturedSitekey string
		handler := am.Sitekey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedProperty, _ = r.Context().Value(common.PropertyContextKey).(*dbgen.Property)
			capturedSitekey, _ = r.Context().Value(common.SitekeyContextKey).(string)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/"+common.PuzzleEndpoint+"?"+common.ParamSiteKey+"="+upperSitekey, nil)
		req.Header.Set(common.HeaderOrigin, origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if capturedProperty != nil {
			t.Fatal("did not expect PropertyContextKey to be set on cache miss")
		}
		if capturedSitekey != lowerSitekey {
			t.Fatalf("SitekeyContextKey = %q, want lowercase %q (sitekey should be canonicalized before being stored in context)", capturedSitekey, lowerSitekey)
		}
		select {
		case refreshed := <-am.SitekeyChan:
			if refreshed != lowerSitekey {
				t.Fatalf("refreshPropertyBySitekey backfill received %q, want lowercase %q", refreshed, lowerSitekey)
			}
		default:
			t.Fatal("expected refreshPropertyBySitekey to be queued on cache miss")
		}
	})
}

// TestGetCachedPropertyBySitekeyTestPropertySitekeyCaseSensitiveBypass
// documents the lower-level reason the Sitekey middleware must normalize:
// GetCachedPropertyBySitekey compares the input against TestPropertySitekey
// with a bare case-sensitive `==`. An uppercase variant decodes to the same
// UUID bytes (UUIDFromSiteKey uses hex.DecodeString, which is case-insensitive)
// yet bypasses the ErrTestProperty short-circuit and falls through to a
// regular cache miss. The fix at the middleware layer (lowercasing before
// this call) restores the test-property special casing.
func TestGetCachedPropertyBySitekeyTestPropertySitekeyCaseSensitiveBypass(t *testing.T) {
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	store := db.NewBusinessWithQuerier(nil, &db.QuerierStub{}, cache)

	_, _, errLower := store.Impl().GetCachedPropertyBySitekey(context.Background(), db.TestPropertySitekey)
	if errLower != db.ErrTestProperty {
		t.Fatalf("lowercase TestPropertySitekey: err = %v, want ErrTestProperty", errLower)
	}

	upper := strings.ToUpper(db.TestPropertySitekey)
	if !db.CanBeValidSitekey(upper) {
		t.Fatalf("uppercase TestPropertySitekey should pass CanBeValidSitekey")
	}
	if got := db.UUIDFromSiteKey(upper); got != db.TestPropertyUUID {
		t.Fatalf("uppercase TestPropertySitekey should decode to TestPropertyUUID")
	}

	_, _, errUpper := store.Impl().GetCachedPropertyBySitekey(context.Background(), upper)
	if errUpper != db.ErrCacheMiss {
		t.Fatalf("uppercase TestPropertySitekey: err = %v, want ErrCacheMiss (the case-sensitive == bypass at the impl level that the middleware must normalize away)", errUpper)
	}
}

// TestSitekeyMiddlewareTestPropertySitekeyUppercaseRouting confirms that,
// after the middleware-level normalization, an uppercase TestPropertySitekey
// request is routed through the ErrTestProperty branch (rather than the
// cache-miss / stub-puzzle path) and that the canonical lowercase value is
// stored in SitekeyContextKey so the test-property special casing in
// puzzlePreFlight / PuzzleForRequest continues to fire.
func TestSitekeyMiddlewareTestPropertySitekeyUppercaseRouting(t *testing.T) {
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	am := newSitekeyMiddleware(cache)

	upper := strings.ToUpper(db.TestPropertySitekey)
	if !db.CanBeValidSitekey(upper) {
		t.Fatalf("uppercase TestPropertySitekey should pass CanBeValidSitekey")
	}

	var capturedProperty *dbgen.Property
	var capturedSitekey string
	handler := am.Sitekey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedProperty, _ = r.Context().Value(common.PropertyContextKey).(*dbgen.Property)
		capturedSitekey, _ = r.Context().Value(common.SitekeyContextKey).(string)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/"+common.PuzzleEndpoint+"?"+common.ParamSiteKey+"="+upper, nil)
	req.Header.Set(common.HeaderOrigin, "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if capturedProperty != nil {
		t.Fatal("did not expect PropertyContextKey to be set for test property sitekey")
	}
	if capturedSitekey != db.TestPropertySitekey {
		t.Fatalf("SitekeyContextKey = %q, want lowercase TestPropertySitekey %q (so puzzlePreFlight / PuzzleForRequest test-property guards continue to match)", capturedSitekey, db.TestPropertySitekey)
	}
	// ErrTestProperty branch does NOT enqueue a backfill (only ErrCacheMiss and needsRefresh do).
	select {
	case refreshed := <-am.SitekeyChan:
		t.Fatalf("did not expect refresh backfill for test property sitekey, got %q", refreshed)
	default:
	}
}

// TestSitekeyOptionsMiddlewareNormalizesUppercaseSitekey confirms that the
// OPTIONS preflight middleware canonicalizes the sitekey to lowercase before
// storing it in SitekeyContextKey, so the downstream puzzlePreFlight
// test-property equality check (which uses a case-sensitive ==) keeps
// firing for uppercase inputs.
func TestSitekeyOptionsMiddlewareNormalizesUppercaseSitekey(t *testing.T) {
	am := &AuthMiddleware{}

	cases := []struct {
		name    string
		sitekey string
		want    string
	}{
		{name: "LowercaseStaysLowercase", sitekey: db.TestPropertySitekey, want: db.TestPropertySitekey},
		{name: "UppercaseCanonicalizedToLowercase", sitekey: strings.ToUpper(db.TestPropertySitekey), want: db.TestPropertySitekey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedSitekey string
			handler := am.SitekeyOptions(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedSitekey, _ = r.Context().Value(common.SitekeyContextKey).(string)
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodOptions, "/"+common.PuzzleEndpoint+"?"+common.ParamSiteKey+"="+tc.sitekey, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
			}
			if capturedSitekey != tc.want {
				t.Fatalf("SitekeyContextKey = %q, want %q", capturedSitekey, tc.want)
			}
		})
	}
}

// TestPuzzlePreFlightEmitsAnyOriginForUppercaseTestPropertySitekeyViaMiddleware
// is the end-to-end proof of the fix: chaining SitekeyOptions (which
// populates SitekeyContextKey) with puzzlePreFlight (which uses a
// case-sensitive == db.TestPropertySitekey) must emit the
// Access-Control-Allow-Origin: * header for an uppercase TestPropertySitekey
// request, exactly as it does for the lowercase variant.
func TestPuzzlePreFlightEmitsAnyOriginForUppercaseTestPropertySitekeyViaMiddleware(t *testing.T) {
	am := &AuthMiddleware{}
	srv := &Server{}

	runOne := func(t *testing.T, sitekey string) {
		handler := am.SitekeyOptions(http.HandlerFunc(srv.puzzlePreFlight))

		req := httptest.NewRequest(http.MethodOptions, "/"+common.PuzzleEndpoint+"?"+common.ParamSiteKey+"="+sitekey, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
		}
		if got := w.Header().Get(common.HeaderAccessControlOrigin); got != "*" {
			t.Fatalf("Access-Control-Allow-Origin = %q, want %q (test property preflight should be allowed from any origin)", got, "*")
		}
	}

	t.Run("Lowercase", func(t *testing.T) { runOne(t, db.TestPropertySitekey) })
	t.Run("Uppercase", func(t *testing.T) { runOne(t, strings.ToUpper(db.TestPropertySitekey)) })
}
