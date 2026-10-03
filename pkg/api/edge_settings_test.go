package api

import (
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func TestPropertyWritesDoNotCreateEdgeSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"create", "update"} {
		t.Run(name, func(t *testing.T) {
			params := db_tests.CreateNewPropertyParams(user.ID, name+".edge.example.com")
			if name == "create" {
				params.EdgeTokenValidityInterval = time.Hour
			}
			property, _, err := store.Impl().CreateNewProperty(ctx, params, org)
			if err != nil {
				t.Fatal(err)
			}
			if name == "update" {
				property, _, err = store.Impl().UpdateProperty(ctx, org, user, &dbgen.UpdatePropertyParams{
					ID: property.ID, Name: property.Name, Level: property.Level, Growth: property.Growth,
					ValidityInterval: property.ValidityInterval, EdgeTokenValidityInterval: time.Hour,
					AllowSubdomains: property.AllowSubdomains, AllowLocalhost: property.AllowLocalhost,
					MaxReplayCount: property.MaxReplayCount,
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			sitekey := db.UUIDToSiteKey(property.ExternalID)
			if _, err := store.Impl().RetrieveEdgeSettingsBySitekey(ctx, sitekey, true); err != db.ErrRecordNotFound {
				t.Fatalf("property %s created edge settings: %v", name, err)
			}
			if _, err := store.Impl().RetrieveEdgeSettingsBySitekey(ctx, sitekey, false); err != db.ErrRecordNotFound {
				t.Fatalf("cached missing settings: %v", err)
			}
		})
	}
}

func TestEdgeSettingsStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	params := db_tests.CreateNewPropertyParams(user.ID, "edge-storage.example.com")
	params.EdgeTokenValidityInterval = time.Hour
	property, _, err := store.Impl().CreateNewProperty(ctx, params, org)
	if err != nil {
		t.Fatal(err)
	}
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	if _, _, err := store.Impl().GetCachedEdgeSettingsBySitekey(ctx, sitekey); err != db.ErrCacheMiss {
		t.Fatalf("uncached settings: %v", err)
	}
	if _, _, err := store.Impl().UpdateEdgeSettings(ctx, org, user, property, dbgen.EdgeWidgetStartModeLoad); err != nil {
		t.Fatal(err)
	}
	edge, err := store.Impl().RetrieveEdgeSettingsBySitekey(ctx, sitekey, false)
	if err != nil || edge.PropertyID != property.ID || edge.EdgeWidgetStartMode != dbgen.EdgeWidgetStartModeLoad || edge.Domain != property.Domain {
		t.Fatalf("stored settings: %+v, err=%v", edge, err)
	}
	if !edge.UpdatedAt.Valid {
		t.Fatal("new edge settings have no update timestamp")
	}
	oldTimestamp := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.edge_property_settings SET updated_at=$1 WHERE property_id=$2", oldTimestamp, property.ID); err != nil {
		t.Fatal(err)
	}
	updated, _, err := store.Impl().UpdateEdgeSettings(ctx, org, user, property, dbgen.EdgeWidgetStartModeLoad)
	if err != nil || !updated.UpdatedAt.Valid || !updated.UpdatedAt.Time.After(oldTimestamp) {
		t.Fatalf("edge settings update did not refresh timestamp: %+v, err=%v", updated, err)
	}
	if _, err := store.Impl().RetrieveEdgeSettingsBySitekey(ctx, sitekey, false); err != nil {
		t.Fatal(err)
	}
	store.MaintenanceMode.Store(true)
	cached, _, err := store.Impl().GetCachedEdgeSettingsBySitekey(ctx, sitekey)
	store.MaintenanceMode.Store(false)
	if err != nil || cached.EdgeWidgetStartMode != dbgen.EdgeWidgetStartModeLoad {
		t.Fatalf("cache-only settings: %+v, err=%v", cached, err)
	}
	store.Cache.Delete(ctx, db.EdgeSettingsBySitekeyCacheKey(sitekey))
	if err := server.Auth.backfillEdgeImpl(ctx, map[string]uint{sitekey: 1, "99999999222233334444555555555555": 1}); err != nil {
		t.Fatal(err)
	}
	refreshed, _, err := store.Impl().GetCachedEdgeSettingsBySitekey(ctx, sitekey)
	if err != nil || refreshed.EdgeWidgetStartMode != dbgen.EdgeWidgetStartModeLoad {
		t.Fatalf("backfill did not refresh stale settings: %+v, err=%v", refreshed, err)
	}
	if _, _, err := store.Impl().GetCachedEdgeSettingsBySitekey(ctx, "99999999222233334444555555555555"); err != db.ErrNegativeCacheHit {
		t.Fatalf("backfill did not negative-cache missing settings: %v", err)
	}
	if _, _, err := store.Impl().UpdateEdgeSettings(ctx, org, &dbgen.User{ID: -1}, property, dbgen.EdgeWidgetStartModeClick); err != db.ErrPermissions {
		t.Fatalf("unauthorized update: %v", err)
	}
	if _, err := store.Impl().SoftDeleteProperty(ctx, property, org, user); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Impl().GetCachedEdgeSettingsBySitekey(ctx, sitekey); err != db.ErrNegativeCacheHit {
		t.Fatalf("deleted property settings remained cached: %v", err)
	}
}
