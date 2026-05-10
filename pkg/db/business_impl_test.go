package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func setupTestStore(t *testing.T, expectedErr error) *BusinessStoreImpl {
	stub := &QuerierStub{Error: expectedErr}
	cache := NewStaticCache[CacheKey, any](1000, &CacheMissingValue{})
	return &BusinessStoreImpl{
		querier: stub,
		cache:   cache,
	}
}

func TestBusinessStoreImplRetrieveFromCache(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveFromCache(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveFromCache(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplStoreInCache(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.StoreInCache(context.Background(), "", nil, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.StoreInCache(context.Background(), "", nil, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteExpiredCache(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteExpiredCache(context.Background())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteExpiredCache(context.Background())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateNewSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateNewSubscription(context.Background(), &dbgen.CreateSubscriptionParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateNewSubscription(context.Background(), &dbgen.CreateSubscriptionParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateNewOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.CreateNewOrganization(context.Background(), "", 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.CreateNewOrganization(context.Background(), "", 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplSoftDeleteUser(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.SoftDeleteUser(context.Background(), &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.SoftDeleteUser(context.Background(), &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteUserSession(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteUserSession(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUserSession(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCacheUserSession(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.CacheUserSession(context.Background(), &session.SessionData{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.CacheUserSession(context.Background(), &session.SessionData{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveUserSession(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserSession(context.Background(), "", false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserSession(context.Background(), "", false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplStoreUserSessions(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.StoreUserSessions(context.Background(), nil, 0, 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.StoreUserSessions(context.Background(), nil, 0, 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrievePropertyBySitekey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePropertyBySitekey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePropertyBySitekey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrievePropertiesBySitekey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePropertiesBySitekey(context.Background(), nil, 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePropertiesBySitekey(context.Background(), nil, 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrievePropertiesByID(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePropertiesByID(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePropertiesByID(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplGetCachedAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedAPIKey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedAPIKey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplFindUserAPIKeyByName(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindUserAPIKeyByName(context.Background(), &dbgen.User{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindUserAPIKeyByName(context.Background(), &dbgen.User{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveAPIKey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveAPIKey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplFindUserByEmail(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindUserByEmail(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindUserByEmail(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserOrganizations(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserOrganizations(context.Background(), 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserOrganizations(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedOrgProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedOrgProperties(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedOrgProperties(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSubscription(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSubscription(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplFindOrgProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindOrgProperty(context.Background(), "", &dbgen.Organization{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindOrgProperty(context.Background(), "", &dbgen.Organization{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplFindOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindOrg(context.Background(), "", &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindOrg(context.Background(), "", &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateNewProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.CreateNewProperty(context.Background(), &dbgen.CreatePropertyParams{}, &dbgen.Organization{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.CreateNewProperty(context.Background(), &dbgen.CreatePropertyParams{}, &dbgen.Organization{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateProperty(context.Background(), &dbgen.Organization{}, &dbgen.User{}, &dbgen.UpdatePropertyParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateProperty(context.Background(), &dbgen.Organization{}, &dbgen.User{}, &dbgen.UpdatePropertyParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplSoftDeleteProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.SoftDeleteProperty(context.Background(), &dbgen.Property{}, &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.SoftDeleteProperty(context.Background(), &dbgen.Property{}, &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplSoftDeleteProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.SoftDeleteProperties(context.Background(), nil, &dbgen.User{}, &dbgen.Organization{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.SoftDeleteProperties(context.Background(), nil, &dbgen.User{}, &dbgen.Organization{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveOrgProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RetrieveOrgProperties(context.Background(), &dbgen.Organization{}, 0, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RetrieveOrgProperties(context.Background(), &dbgen.Organization{}, 0, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateOrganization(context.Background(), &dbgen.User{}, &dbgen.Organization{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateOrganization(context.Background(), &dbgen.User{}, &dbgen.Organization{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplSoftDeleteOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.SoftDeleteOrganization(context.Background(), &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.SoftDeleteOrganization(context.Background(), &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrganizationUsers(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrganizationUsers(context.Background(), 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrganizationUsers(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrganizationUsersWithEmailInvites(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrganizationUsersWithEmailInvites(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrganizationUsersWithEmailInvites(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplInviteUserToOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.InviteUserToOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.InviteUserToOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplInviteEmailToOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.InviteEmailToOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.InviteEmailToOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedOrgInviteByID(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedOrgInviteByID(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedOrgInviteByID(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplLinkOrgInviteToUser(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.LinkOrgInviteToUser(context.Background(), 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.LinkOrgInviteToUser(context.Background(), 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplJoinOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.JoinOrg(context.Background(), 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.JoinOrg(context.Background(), 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplLeaveOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.LeaveOrg(context.Background(), 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.LeaveOrg(context.Background(), 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRemoveUserFromOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RemoveUserFromOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, 0)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RemoveUserFromOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRemoveEmailInviteFromOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RemoveEmailInviteFromOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RemoveEmailInviteFromOrg(context.Background(), &dbgen.User{}, &dbgen.Organization{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateUserSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateUserSubscription(context.Background(), &dbgen.User{}, &dbgen.Subscription{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateUserSubscription(context.Background(), &dbgen.User{}, &dbgen.Subscription{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateUser(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.UpdateUser(context.Background(), &dbgen.User{}, "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.UpdateUser(context.Background(), &dbgen.User{}, "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserAPIKeys(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserAPIKeys(context.Background(), 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserAPIKeys(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.UpdateAPIKey(context.Background(), &dbgen.User{}, &dbgen.APIKey{}, time.Time{}, false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.UpdateAPIKey(context.Background(), &dbgen.User{}, &dbgen.APIKey{}, time.Time{}, false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.CreateAPIKey(context.Background(), &dbgen.User{}, &dbgen.CreateAPIKeyParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.CreateAPIKey(context.Background(), &dbgen.User{}, &dbgen.CreateAPIKeyParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRotateAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RotateAPIKey(context.Background(), &dbgen.User{}, 0)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RotateAPIKey(context.Background(), &dbgen.User{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.DeleteAPIKey(context.Background(), &dbgen.User{}, 0)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.DeleteAPIKey(context.Background(), &dbgen.User{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateAPIKeysLastUsedAt(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.UpdateAPIKeysLastUsedAt(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.UpdateAPIKeysLastUsedAt(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveUsersWithoutSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUsersWithoutSubscription(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUsersWithoutSubscription(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveLock(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveLock(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveLock(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplAcquireLock(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.AcquireLock(context.Background(), "", nil, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.AcquireLock(context.Background(), "", nil, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplReleaseLock(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.ReleaseLock(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.ReleaseLock(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteDeletedRecords(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteDeletedRecords(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteDeletedRecords(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSoftDeletedProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSoftDeletedProperties(context.Background(), time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSoftDeletedProperties(context.Background(), time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteProperties(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteProperties(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveSoftDeletedOrganizations(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSoftDeletedOrganizations(context.Background(), time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSoftDeletedOrganizations(context.Background(), time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteOrganizations(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteOrganizations(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteOrganizations(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveSoftDeletedUsers(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSoftDeletedUsers(context.Background(), time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSoftDeletedUsers(context.Background(), time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteUsers(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteUsers(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUsers(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveSystemNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSystemNotification(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSystemNotification(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSystemUserNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSystemUserNotification(context.Background(), time.Time{}, 0)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSystemUserNotification(context.Background(), time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateSystemNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateSystemNotification(context.Background(), "", time.Time{}, nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateSystemNotification(context.Background(), "", time.Time{}, nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveProperties(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveProperties(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserPropertiesCount(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserPropertiesCount(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserPropertiesCount(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedPropertyBySitekey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.GetCachedPropertyBySitekey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.GetCachedPropertyBySitekey(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUser(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUser(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUser(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RetrieveUserOrganization(context.Background(), &dbgen.User{}, 0)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RetrieveUserOrganization(context.Background(), &dbgen.User{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrgProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrgProperty(context.Background(), &dbgen.Organization{}, 0)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrgProperty(context.Background(), &dbgen.Organization{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateNewAccount(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, _, err := store.CreateNewAccount(context.Background(), &dbgen.CreateSubscriptionParams{}, "", "", "", 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, _, err := store.CreateNewAccount(context.Background(), &dbgen.CreateSubscriptionParams{}, "", "", "", 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateNotificationTemplate(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateNotificationTemplate(context.Background(), "", "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateNotificationTemplate(context.Background(), "", "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveNotificationTemplate(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveNotificationTemplate(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveNotificationTemplate(context.Background(), "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateUserNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateUserNotification(context.Background(), &common.ScheduledNotification{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateUserNotification(context.Background(), &common.ScheduledNotification{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrievePendingUserNotifications(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePendingUserNotifications(context.Background(), time.Time{}, 0, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePendingUserNotifications(context.Background(), time.Time{}, 0, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplMarkUserNotificationsAttempted(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.MarkUserNotificationsAttempted(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.MarkUserNotificationsAttempted(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplMarkUserNotificationsProcessed(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.MarkUserNotificationsProcessed(context.Background(), nil, time.Time{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.MarkUserNotificationsProcessed(context.Background(), nil, time.Time{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteUnusedNotificationTemplates(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteUnusedNotificationTemplates(context.Background(), time.Time{}, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUnusedNotificationTemplates(context.Background(), time.Time{}, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteSentUserNotifications(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteSentUserNotifications(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteSentUserNotifications(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteUnsentUserNotifications(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteUnsentUserNotifications(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUnsentUserNotifications(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeletePendingUserNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeletePendingUserNotification(context.Background(), &dbgen.User{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeletePendingUserNotification(context.Background(), &dbgen.User{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplExpireInternalTrials(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.ExpireInternalTrials(context.Background(), time.Time{}, time.Time{}, "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.ExpireInternalTrials(context.Background(), time.Time{}, time.Time{}, "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplMoveProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.MoveProperty(context.Background(), &dbgen.User{}, &dbgen.Property{}, &dbgen.GetUserOrganizationsRow{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.MoveProperty(context.Background(), &dbgen.User{}, &dbgen.Property{}, &dbgen.GetUserOrganizationsRow{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplTransferOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.TransferOrganization(context.Background(), &dbgen.User{}, &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.TransferOrganization(context.Background(), &dbgen.User{}, &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteOldAuditLogs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteOldAuditLogs(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteOldAuditLogs(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedAuditLogs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedAuditLogs(context.Background(), &dbgen.User{}, 0, time.Time{}, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedAuditLogs(context.Background(), &dbgen.User{}, 0, time.Time{}, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserAuditLogs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserAuditLogs(context.Background(), &dbgen.User{}, 0, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserAuditLogs(context.Background(), &dbgen.User{}, 0, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrievePropertyAuditLogs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePropertyAuditLogs(context.Background(), &dbgen.Property{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePropertyAuditLogs(context.Background(), &dbgen.Property{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrganizationAuditLogs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrganizationAuditLogs(context.Background(), &dbgen.Organization{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrganizationAuditLogs(context.Background(), &dbgen.Organization{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateNewAsyncTask(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateNewAsyncTask(context.Background(), nil, "", &dbgen.User{}, time.Time{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateNewAsyncTask(context.Background(), nil, "", &dbgen.User{}, time.Time{}, "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveAsyncTask(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveAsyncTask(context.Background(), pgtype.UUID{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveAsyncTask(context.Background(), pgtype.UUID{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrievePendingAsyncTasks(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePendingAsyncTasks(context.Background(), 0, time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePendingAsyncTasks(context.Background(), 0, time.Time{}, 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteOldAsyncTasks(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteOldAsyncTasks(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteOldAsyncTasks(context.Background(), time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateAsyncTask(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.UpdateAsyncTask(context.Background(), pgtype.UUID{}, nil, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.UpdateAsyncTask(context.Background(), pgtype.UUID{}, nil, time.Time{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrgOwnerWithSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RetrieveOrgOwnerWithSubscription(context.Background(), &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RetrieveOrgOwnerWithSubscription(context.Background(), &dbgen.Organization{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrgPropertiesCount(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrgPropertiesCount(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrgPropertiesCount(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveDifficultyRulesByPropertyIDs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveDifficultyRulesByPropertyIDs(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveDifficultyRulesByPropertyIDs(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveDifficultyRulesByOrgIDs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveDifficultyRulesByOrgIDs(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveDifficultyRulesByOrgIDs(context.Background(), nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBusinessStoreImplGetCachedOrgRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedOrgRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedOrgRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedPropertyRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedPropertyRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedPropertyRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.CreateDifficultyRule(context.Background(), &dbgen.User{}, &dbgen.CreateDifficultyRuleParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.CreateDifficultyRule(context.Background(), &dbgen.User{}, &dbgen.CreateDifficultyRuleParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedCompiledPropertyRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.GetCachedCompiledPropertyRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.GetCachedCompiledPropertyRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedCompiledOrgRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.GetCachedCompiledOrgRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.GetCachedCompiledOrgRules(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveDifficultyRule(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveDifficultyRule(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateDifficultyRule(context.Background(), &dbgen.Organization{}, &dbgen.Property{}, &dbgen.User{}, &dbgen.UpdateDifficultyRuleParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateDifficultyRule(context.Background(), &dbgen.Organization{}, &dbgen.Property{}, &dbgen.User{}, &dbgen.UpdateDifficultyRuleParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.DeleteDifficultyRule(context.Background(), &dbgen.Organization{}, &dbgen.DifficultyRule{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.DeleteDifficultyRule(context.Background(), &dbgen.Organization{}, &dbgen.DifficultyRule{}, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplMoveDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.MoveDifficultyRule(context.Background(), &dbgen.Organization{}, &dbgen.DifficultyRule{}, 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.MoveDifficultyRule(context.Background(), &dbgen.Organization{}, &dbgen.DifficultyRule{}, 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRebalanceDifficultyRulesForProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.RebalanceDifficultyRulesForProperty(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.RebalanceDifficultyRulesForProperty(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRebalanceDifficultyRulesForOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.RebalanceDifficultyRulesForOrg(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.RebalanceDifficultyRulesForOrg(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplMoveDifficultyRuleWithRebalancing(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.MoveDifficultyRuleWithRebalancing(context.Background(), &dbgen.Organization{}, &dbgen.DifficultyRule{}, 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.MoveDifficultyRuleWithRebalancing(context.Background(), &dbgen.Organization{}, &dbgen.DifficultyRule{}, 0, &dbgen.User{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserSettings(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserSettings(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserSettings(context.Background(), 0)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpsertUserSettings(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpsertUserSettings(context.Background(), &dbgen.UpsertUserSettingsParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpsertUserSettings(context.Background(), &dbgen.UpsertUserSettingsParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUsersWithPendingWeeklyReport(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUsersWithPendingWeeklyReport(context.Background(), 0, 0, "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUsersWithPendingWeeklyReport(context.Background(), 0, 0, "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUsersWithPendingMonthlyReport(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUsersWithPendingMonthlyReport(context.Background(), 0, 0, "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUsersWithPendingMonthlyReport(context.Background(), 0, 0, "", "", "")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}
