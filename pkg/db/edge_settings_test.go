package db

import (
	"context"
	"testing"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type upsertEdgeSettingsQuerierStub struct {
	*QuerierStub
	row *dbgen.UpsertEdgeSettingsRow
}

func (s *upsertEdgeSettingsQuerierStub) UpsertEdgeSettings(context.Context, *dbgen.UpsertEdgeSettingsParams) (*dbgen.UpsertEdgeSettingsRow, error) {
	return s.row, s.Error
}

func TestUpdateEdgeSettingsCachesCurrentSettings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		missing  bool
		disabled bool
	}{
		{name: "overwrite negative cache", missing: true},
		{name: "overwrite stale settings"},
		{name: "delete disabled edge cache", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			const sitekey = "11111111222233334444555555555555"
			property := &dbgen.Property{
				ID: 1, ExternalID: UUIDFromSiteKey(sitekey), OrgID: Int(2), CreatorID: Int(3),
				Domain: "example.com", Challenge: dbgen.ChallengeTypeArgon2ID,
				EdgeTokenValidityInterval: time.Hour, Enabled: true,
			}
			if tc.disabled {
				property.EdgeTokenValidityInterval = 0
			}
			row := &dbgen.UpsertEdgeSettingsRow{
				PropertyID: property.ID, ExternalID: property.ExternalID,
				EdgeWidgetStartMode: dbgen.EdgeWidgetStartModeLoad, OldEdgeWidgetStartMode: dbgen.EdgeWidgetStartModeClick,
				UpdatedAt: Timestampz(time.Now()),
			}
			cache := NewStaticCache[CacheKey, any](10, &CacheMissingValue{})
			key := EdgeSettingsBySitekeyCacheKey(sitekey)
			if tc.missing {
				if err := cache.SetMissing(ctx, key); err != nil {
					t.Fatal(err)
				}
			} else if err := cache.Set(ctx, key, &dbgen.GetEdgeSettingsBySitekeyRow{EdgeWidgetStartMode: dbgen.EdgeWidgetStartModeClick}); err != nil {
				t.Fatal(err)
			}
			store := NewBusinessWithQuerier(nil, &upsertEdgeSettingsQuerierStub{QuerierStub: &QuerierStub{}, row: row}, cache)
			settings, _, err := store.Impl().UpdateEdgeSettings(ctx, &dbgen.Organization{ID: 2}, &dbgen.User{ID: 3}, property, dbgen.EdgeWidgetStartModeLoad)
			if err != nil {
				t.Fatal(err)
			}
			cached, needsRefresh, err := store.Impl().GetCachedEdgeSettingsBySitekey(ctx, sitekey)
			if tc.disabled {
				if err != ErrCacheMiss {
					t.Fatalf("disabled edge cache = %+v, error = %v, want %v", cached, err, ErrCacheMiss)
				}
				return
			}
			want := dbgen.GetEdgeSettingsBySitekeyRow{
				PropertyID: settings.PropertyID, ExternalID: settings.ExternalID,
				EdgeWidgetStartMode: settings.EdgeWidgetStartMode, UpdatedAt: settings.UpdatedAt,
				Domain: property.Domain, Challenge: property.Challenge,
			}
			if err != nil || needsRefresh || cached == nil || *cached != want {
				t.Fatalf("edge cache = %+v, refresh = %v, error = %v, want %+v", cached, needsRefresh, err, want)
			}
		})
	}
}
