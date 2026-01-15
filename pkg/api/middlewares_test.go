package api

import (
	"context"
	"testing"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
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
		name          string
		origin        string
		domain        string
		allowLocal    bool
		allowSubdoms  bool
		expected      bool
	}{
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
