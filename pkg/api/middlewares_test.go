package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestIsAPIKeyValidNil(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnow := time.Now().UTC()

	if isAPIKeyValid(ctx, nil, tnow) {
		t.Error("Expected nil key to be invalid")
	}
}

func TestIsAPIKeyValidDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnow := time.Now().UTC()

	key := &dbgen.APIKey{
		ID:        1,
		Enabled:   pgtype.Bool{Valid: true, Bool: false},
		ExpiresAt: pgtype.Timestamptz{Valid: true, Time: tnow.Add(1 * time.Hour)},
	}

	if isAPIKeyValid(ctx, key, tnow) {
		t.Error("Expected disabled key to be invalid")
	}
}

func TestIsAPIKeyValidEnabledNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnow := time.Now().UTC()

	key := &dbgen.APIKey{
		ID:        1,
		Enabled:   pgtype.Bool{Valid: false}, // NULL
		ExpiresAt: pgtype.Timestamptz{Valid: true, Time: tnow.Add(1 * time.Hour)},
	}

	if isAPIKeyValid(ctx, key, tnow) {
		t.Error("Expected key with NULL enabled to be invalid")
	}
}

func TestIsAPIKeyValidExpired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnow := time.Now().UTC()

	key := &dbgen.APIKey{
		ID:        1,
		Enabled:   pgtype.Bool{Valid: true, Bool: true},
		ExpiresAt: pgtype.Timestamptz{Valid: true, Time: tnow.Add(-1 * time.Hour)}, // Expired
	}

	if isAPIKeyValid(ctx, key, tnow) {
		t.Error("Expected expired key to be invalid")
	}
}

func TestIsAPIKeyValidExpiresAtNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnow := time.Now().UTC()

	key := &dbgen.APIKey{
		ID:        1,
		Enabled:   pgtype.Bool{Valid: true, Bool: true},
		ExpiresAt: pgtype.Timestamptz{Valid: false}, // NULL
	}

	if isAPIKeyValid(ctx, key, tnow) {
		t.Error("Expected key with NULL expiration to be invalid")
	}
}

func TestIsAPIKeyValidValid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnow := time.Now().UTC()

	key := &dbgen.APIKey{
		ID:        1,
		Enabled:   pgtype.Bool{Valid: true, Bool: true},
		ExpiresAt: pgtype.Timestamptz{Valid: true, Time: tnow.Add(1 * time.Hour)},
	}

	if !isAPIKeyValid(ctx, key, tnow) {
		t.Error("Expected valid key to be valid")
	}
}

func TestIsAPIKeyValidExactlyExpired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tnow := time.Now().UTC()

	// Key expires exactly at tnow - according to the code, this is valid
	// because ExpiresAt.Before(tnow) is false when they are equal
	key := &dbgen.APIKey{
		ID:        1,
		Enabled:   pgtype.Bool{Valid: true, Bool: true},
		ExpiresAt: pgtype.Timestamptz{Valid: true, Time: tnow},
	}

	if !isAPIKeyValid(ctx, key, tnow) {
		t.Error("Expected key expiring exactly at tnow to be valid (equal is not before)")
	}
}

func TestIsOriginAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		origin       string
		domain       string
		allowLocal   bool
		allowSubdoms bool
		expected     bool
	}{
		{
			name:         "EmptyDomainAllowsAnyOrigin",
			origin:       "example.com",
			domain:       "",
			allowLocal:   false,
			allowSubdoms: false,
			expected:     true,
		},
		{
			name:         "ExactDomainMatch",
			origin:       "example.com",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: false,
			expected:     true,
		},
		{
			name:         "DomainMismatch",
			origin:       "other.com",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: false,
			expected:     false,
		},
		{
			name:         "SubdomainNotAllowed",
			origin:       "sub.example.com",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: false,
			expected:     false,
		},
		{
			name:         "SubdomainAllowed",
			origin:       "sub.example.com",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: true,
			expected:     true,
		},
		{
			name:         "DeepSubdomainAllowed",
			origin:       "deep.sub.example.com",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: true,
			expected:     true,
		},
		{
			name:         "LocalhostNotAllowed",
			origin:       "localhost",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: false,
			expected:     false,
		},
		{
			name:         "LocalhostAllowed",
			origin:       "localhost",
			domain:       "example.com",
			allowLocal:   true,
			allowSubdoms: false,
			expected:     true,
		},
		{
			name:         "LocalhostSubdomainOneLevelAllowed",
			origin:       "captcha.localhost",
			domain:       "example.com",
			allowLocal:   true,
			allowSubdoms: false,
			expected:     true,
		},
		{
			name:         "LocalhostSubdomainTwoLevelsAllowed",
			origin:       "my.captcha.localhost",
			domain:       "example.com",
			allowLocal:   true,
			allowSubdoms: false,
			expected:     true,
		},
		{
			name:         "LocalhostSubdomainNotAllowed",
			origin:       "my.captcha.localhost",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: false,
			expected:     false,
		},
		{
			name:         "LocalhostIP127Allowed",
			origin:       "127.0.0.1",
			domain:       "example.com",
			allowLocal:   true,
			allowSubdoms: false,
			expected:     true,
		},
		{
			name:         "LocalhostIP127NotAllowed",
			origin:       "127.0.0.1",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: false,
			expected:     false,
		},
		{
			name:         "LocalhostIPv6Allowed",
			origin:       "::1",
			domain:       "example.com",
			allowLocal:   true,
			allowSubdoms: false,
			expected:     true,
		},
		{
			name:         "SubdomainMatchExact",
			origin:       "example.com",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: true,
			expected:     true,
		},
		{
			name:         "SubdomainWithSimilarSuffix",
			origin:       "notexample.com",
			domain:       "example.com",
			allowLocal:   false,
			allowSubdoms: true,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			property := &dbgen.Property{
				Domain:          tt.domain,
				AllowLocalhost:  tt.allowLocal,
				AllowSubdomains: tt.allowSubdoms,
			}

			result := isOriginAllowed(tt.origin, property)
			if result != tt.expected {
				t.Errorf("isOriginAllowed(%q, property{Domain:%q, AllowLocalhost:%v, AllowSubdomains:%v}) = %v, want %v",
					tt.origin, tt.domain, tt.allowLocal, tt.allowSubdoms, result, tt.expected)
			}
		})
	}
}

type errUserLimiter struct{}

func (errUserLimiter) CheckUsers(context.Context, map[int32]uint) error {
	return errors.New("no limits")
}

func (errUserLimiter) EvaluatePropertyAccess(context.Context, int32) (bool, error) {
	return false, errors.New("no limits")
}

func (errUserLimiter) EvaluateFormAccess(context.Context, int32) (bool, error) {
	return false, errors.New("no limits")
}

func (errUserLimiter) EvaluateAPIAccess(context.Context, int32) (bool, error) {
	return false, errors.New("no limits")
}

func (errUserLimiter) DropUser(context.Context, int32) {}

func TestAuthMiddlewareFormNormalizesExternalIDToLowerCase(t *testing.T) {
	form := &dbgen.Form{
		ID:         123,
		PropertyID: 456,
		OrgOwnerID: db.Int(7),
		OrgID:      db.Int(8),
		ExternalID: db.TestPropertyUUID,
		Enabled:    true,
		Active:     true,
	}
	lowerID := db.UUIDToString(form.ExternalID)
	upperID := strings.ToUpper(lowerID)
	if !db.CanBeValidSitekey(upperID) {
		t.Fatalf("uppercase external ID should pass CanBeValidSitekey")
	}
	if got := db.UUIDFromString(upperID); got != form.ExternalID {
		t.Fatalf("uppercase external ID should decode to the same UUID")
	}

	newMiddleware := func(cache common.Cache[db.CacheKey, any]) *AuthMiddleware {
		store := db.NewBusinessWithQuerier(nil, &db.QuerierStub{}, cache)
		return &AuthMiddleware{
			Store:               store,
			Limiter:             errUserLimiter{},
			FormChan:            make(chan string, 1),
			backpressureTimeout: time.Second,
			Metrics:             monitoring.NewStub(),
		}
	}

	t.Run("CacheHitCanonicalizesLookupAndContextValue", func(t *testing.T) {
		cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
		if err := cache.Set(context.Background(), db.FormByExternalIDCacheKey(lowerID), form); err != nil {
			t.Fatalf("Failed to seed cache: %v", err)
		}
		am := newMiddleware(cache)

		var capturedForm *dbgen.Form
		var capturedFormID string
		handler := am.Form(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedForm, _ = r.Context().Value(common.FormContextKey).(*dbgen.Form)
			capturedFormID, _ = r.Context().Value(common.FormIDContextKey).(string)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/"+common.FormEndpoint+"/"+upperID, nil)
		req.SetPathValue(common.ParamForm, upperID)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (uppercase path should be accepted and served)", w.Code, http.StatusOK)
		}
		if capturedForm == nil {
			t.Fatal("FormContextKey not set; expected cache hit after normalizing uppercase path to the lowercase cache key")
		}
		if capturedFormID != lowerID {
			t.Fatalf("FormIDContextKey = %q, want lowercase %q", capturedFormID, lowerID)
		}
		select {
		case refreshed := <-am.FormChan:
			t.Fatalf("did not expect refresh backfill on cache hit, got %q", refreshed)
		default:
		}
	})

	t.Run("CacheMissCanonicalizesContextValueAndBackfill", func(t *testing.T) {
		cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
		am := newMiddleware(cache)

		var capturedForm *dbgen.Form
		var capturedFormID string
		handler := am.Form(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedForm, _ = r.Context().Value(common.FormContextKey).(*dbgen.Form)
			capturedFormID, _ = r.Context().Value(common.FormIDContextKey).(string)
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/"+common.FormEndpoint+"/"+upperID, nil)
		req.SetPathValue(common.ParamForm, upperID)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if capturedForm != nil {
			t.Fatal("did not expect FormContextKey to be set on cache miss")
		}
		if capturedFormID != lowerID {
			t.Fatalf("FormIDContextKey = %q, want lowercase %q (path should be canonicalized before being stored in context)", capturedFormID, lowerID)
		}
		select {
		case refreshed := <-am.FormChan:
			if refreshed != lowerID {
				t.Fatalf("refreshForm backfill received %q, want lowercase %q", refreshed, lowerID)
			}
		default:
			t.Fatal("expected refreshForm to be queued on cache miss")
		}
	})
}
