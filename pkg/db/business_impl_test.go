package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const testEmail = "foo@bar.com"
const testDomain = "example.com"

var testAPIKeyUUID = pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true}
var testAPIKeySecret = UUIDToSecret(testAPIKeyUUID)
var testCacheKeyStr = SessionCacheKey("valid-sid").String()
var testCacheKey = SessionCacheKey("valid-sid")

type dummySessionStore struct{}

type createPropertyQuerierStub struct {
	*QuerierStub
	property *dbgen.Property
}

type createFormQuerierStub struct {
	*QuerierStub
	property       *dbgen.Property
	form           *dbgen.Form
	createdFormArg *dbgen.CreateFormParams
	calls          []string
}

type retrieveUserFormsQuerierStub struct {
	*QuerierStub
	forms          []*dbgen.Form
	count          int64
	getOrgFormsArg *dbgen.GetOrgFormsParams
	formsCalls     int
	countCalls     int
}

type deactivateFormsQuerierStub struct {
	*QuerierStub
	forms []*dbgen.Form
	ids   []int32
}

func (s *dummySessionStore) Start(ctx context.Context, interval time.Duration)        {}
func (s *dummySessionStore) Init(ctx context.Context, session *session.Session) error { return nil }
func (s *dummySessionStore) Read(ctx context.Context, sid string, skipCache bool) (*session.Session, error) {
	return nil, nil
}
func (s *dummySessionStore) Update(ctx context.Context, session *session.Session) error { return nil }
func (s *dummySessionStore) Destroy(ctx context.Context, sid string) error              { return nil }

func (s *createPropertyQuerierStub) CreateProperty(ctx context.Context, arg *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	return s.property, s.Error
}

func (s *createFormQuerierStub) CreateProperty(ctx context.Context, arg *dbgen.CreatePropertyParams) (*dbgen.Property, error) {
	s.calls = append(s.calls, "CreateProperty")
	return s.property, s.Error
}

func (s *createFormQuerierStub) CreateForm(ctx context.Context, arg *dbgen.CreateFormParams) (*dbgen.Form, error) {
	s.calls = append(s.calls, "CreateForm")
	s.createdFormArg = arg
	return s.form, s.Error
}

func (s *retrieveUserFormsQuerierStub) GetOrgForms(ctx context.Context, arg *dbgen.GetOrgFormsParams) ([]*dbgen.Form, error) {
	s.getOrgFormsArg = arg
	s.formsCalls++
	return s.forms, s.Error
}

func (s *retrieveUserFormsQuerierStub) GetUserFormsCount(ctx context.Context, orgID pgtype.Int4) (int64, error) {
	s.countCalls++
	return s.count, s.Error
}

func (s *deactivateFormsQuerierStub) DeactivateForms(ctx context.Context, ids []int32) ([]*dbgen.Form, error) {
	s.ids = ids
	return s.forms, s.Error
}

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
		_, err := store.RetrieveFromCache(context.Background(), testCacheKeyStr)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveFromCache(context.Background(), testCacheKeyStr)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("DefaultsRequestsPerMinuteWhenMissing", func(t *testing.T) {
		property := &dbgen.Property{
			ID:         123,
			Name:       "form property",
			CreatorID:  Int(12),
			OrgID:      Int(1),
			OrgOwnerID: Int(99),
		}
		form := &dbgen.Form{
			ID:                456,
			ExternalID:        TestPropertyUUID,
			URL:               "https://example.com/submit",
			PropertyID:        property.ID,
			Enabled:           true,
			RequestsPerMinute: 60,
			Method:            dbgen.FormMethodPost,
		}
		querier := &createFormQuerierStub{
			QuerierStub: &QuerierStub{},
			property:    property,
			form:        form,
		}
		store := &BusinessStoreImpl{
			querier: querier,
			cache:   NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}),
		}

		_, _, _, err := store.CreateNewForm(context.Background(), &dbgen.CreatePropertyParams{
			CreatorID: Int(12),
			Domain:    "example.com",
		}, &dbgen.CreateFormParams{
			Name:   t.Name(),
			URL:    "https://example.com/submit",
			Fields: []byte(`{"email":"text"}`),
		}, &dbgen.Organization{ID: 1, UserID: Int(99)})
		if err != nil {
			t.Fatalf("expected form creation to succeed, got %v", err)
		}
		if querier.createdFormArg == nil {
			t.Fatal("expected form create args to be captured")
		}
		if querier.createdFormArg.RequestsPerMinute != 10 {
			t.Fatalf("expected default requests per minute 10, got %d", querier.createdFormArg.RequestsPerMinute)
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveFromCache(context.Background(), "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplStoreInCache(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.StoreInCache(context.Background(), testCacheKeyStr, []byte("val"), time.Minute)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.StoreInCache(context.Background(), testCacheKeyStr, []byte("val"), time.Minute)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.StoreInCache(context.Background(), "", nil, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
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
		err := store.DeleteUserSession(context.Background(), "valid-sid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUserSession(context.Background(), "valid-sid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCacheUserSession(t *testing.T) {

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.CacheUserSession(context.Background(), nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveUserSession(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserSession(context.Background(), "valid-sid", false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserSession(context.Background(), "valid-sid", false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveUserSession(context.Background(), "", false)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplStoreUserSessions(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		sd := session.NewSessionData("valid-sid")
		sess := session.NewSession(sd, &dummySessionStore{})
		_ = sess.Set(context.Background(), session.KeyPersistent, "true")
		store.cache.Set(context.Background(), testCacheKey, sd)

		err := store.StoreUserSessions(context.Background(), map[string]uint{"valid-sid": 1}, session.KeyPersistent, time.Minute)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		sd := session.NewSessionData("valid-sid")
		sess := session.NewSession(sd, &dummySessionStore{})
		_ = sess.Set(context.Background(), session.KeyPersistent, "true")
		store.cache.Set(context.Background(), testCacheKey, sd)

		err := store.StoreUserSessions(context.Background(), map[string]uint{"valid-sid": 1}, session.KeyPersistent, time.Minute)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrievePropertyBySitekey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePropertyBySitekey(context.Background(), TestPropertySitekey)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePropertyBySitekey(context.Background(), TestPropertySitekey)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrievePropertiesBySitekey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePropertiesBySitekey(context.Background(), map[string]uint{TestPropertySitekey: 1}, 1)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePropertiesBySitekey(context.Background(), map[string]uint{TestPropertySitekey: 1}, 1)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected expectedErr, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrievePropertiesByID(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrievePropertiesByID(context.Background(), map[int32]uint{1: 1})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrievePropertiesByID(context.Background(), map[int32]uint{1: 1})
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected expectedErr, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveFormsByExternalID(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveFormsByExternalID(context.Background(), map[string]uint{TestPropertySitekey: 1}, 1)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveFormsByExternalID(context.Background(), map[string]uint{TestPropertySitekey: 1}, 1)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected expectedErr, got %v", err)
		}
	})
}

func TestBusinessStoreImplGetCachedAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedAPIKey(context.Background(), testAPIKeySecret)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedAPIKey(context.Background(), testAPIKeySecret)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplFindUserAPIKeyByName(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindUserAPIKeyByName(context.Background(), &dbgen.User{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindUserAPIKeyByName(context.Background(), &dbgen.User{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.FindUserAPIKeyByName(context.Background(), &dbgen.User{}, "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveAPIKey(context.Background(), testAPIKeySecret)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveAPIKey(context.Background(), testAPIKeySecret)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplFindUserByEmail(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindUserByEmail(context.Background(), testEmail)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindUserByEmail(context.Background(), testEmail)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.FindUserByEmail(context.Background(), "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveUserOrganizations(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserOrganizations(context.Background(), 1)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserOrganizations(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedOrgProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedOrgProperties(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedOrgProperties(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSubscription(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSubscription(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplFindOrgProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindOrgProperty(context.Background(), "valid", &dbgen.Organization{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindOrgProperty(context.Background(), "valid", &dbgen.Organization{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.FindOrgProperty(context.Background(), "", &dbgen.Organization{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplFindOrgForm(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindOrgForm(context.Background(), "valid", &dbgen.Organization{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindOrgForm(context.Background(), "valid", &dbgen.Organization{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.FindOrgForm(context.Background(), "", &dbgen.Organization{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplFindOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.FindOrg(context.Background(), "valid", &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.FindOrg(context.Background(), "valid", &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.FindOrg(context.Background(), "", &dbgen.User{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplCreateNewProperty(t *testing.T) {
	t.Run("AllowsEmptyDomain", func(t *testing.T) {
		querier := &createPropertyQuerierStub{
			QuerierStub: &QuerierStub{},
			property: &dbgen.Property{
				ID:              1,
				Name:            "valid",
				Domain:          "",
				CreatorID:       Int(12),
				OrgID:           Int(1),
				OrgOwnerID:      Int(99),
				AllowSubdomains: true,
				AllowLocalhost:  true,
			},
		}
		store := &BusinessStoreImpl{
			querier: querier,
			cache:   NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}),
		}

		property, _, err := store.CreateNewProperty(context.Background(), &dbgen.CreatePropertyParams{
			Name:            "valid",
			CreatorID:       Int(12),
			Domain:          "",
			AllowSubdomains: true,
			AllowLocalhost:  true,
		}, &dbgen.Organization{ID: 1, UserID: Int(99)})
		if err != nil {
			t.Fatalf("expected empty domain to be allowed, got %v", err)
		}
		if property == nil {
			t.Fatal("expected property to be returned")
		}
		if property.Domain != "" {
			t.Fatalf("expected empty domain, got %q", property.Domain)
		}
	})

	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.CreateNewProperty(context.Background(), &dbgen.CreatePropertyParams{Name: "valid", Domain: testDomain}, &dbgen.Organization{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.CreateNewProperty(context.Background(), &dbgen.CreatePropertyParams{Name: "valid", Domain: testDomain}, &dbgen.Organization{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.CreateNewProperty(context.Background(), &dbgen.CreatePropertyParams{}, &dbgen.Organization{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplCreateNewForm(t *testing.T) {
	t.Run("CreatesPropertyBeforeForm", func(t *testing.T) {
		propertyParams := &dbgen.CreatePropertyParams{
			CreatorID: Int(12),
			Domain:    "example.com",
		}
		formParams := &dbgen.CreateFormParams{
			Name:              t.Name(),
			URL:               "https://example.com/submit",
			Fields:            []byte(`{"email":"text"}`),
			RequestsPerMinute: 60,
			RetryRequestCount: 2,
			Method:            dbgen.FormMethodPost,
		}
		property := &dbgen.Property{
			ID:         123,
			Name:       "form property",
			CreatorID:  Int(12),
			OrgID:      Int(1),
			OrgOwnerID: Int(99),
		}
		form := &dbgen.Form{
			ID:         456,
			ExternalID: TestPropertyUUID,
			URL:        "https://example.com/submit",
			PropertyID: property.ID,
			Enabled:    true,
			Method:     dbgen.FormMethodPost,
		}
		querier := &createFormQuerierStub{
			QuerierStub: &QuerierStub{},
			property:    property,
			form:        form,
		}
		store := &BusinessStoreImpl{
			querier: querier,
			cache:   NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}),
		}

		createdForm, createdProperty, auditEvents, err := store.CreateNewForm(context.Background(), propertyParams, formParams, &dbgen.Organization{ID: 1, UserID: Int(99)})
		if err != nil {
			t.Fatalf("expected form creation to succeed, got %v", err)
		}
		if propertyParams.Name != formParams.Name+formPropertyNameSuffix {
			t.Fatalf("expected generated property name %q, got %q", formParams.Name+formPropertyNameSuffix, propertyParams.Name)
		}
		if createdProperty != property {
			t.Fatalf("expected created property to be returned")
		}
		if createdForm != form {
			t.Fatalf("expected created form to be returned")
		}
		if len(auditEvents) != 2 {
			t.Fatalf("expected property and form audit events, got %d", len(auditEvents))
		}
		if auditEvents[0].TableName != TableNameProperties || auditEvents[1].TableName != TableNameForms {
			t.Fatalf("expected property and form audit events, got %s and %s", auditEvents[0].TableName, auditEvents[1].TableName)
		}
		if len(querier.calls) != 2 || querier.calls[0] != "CreateProperty" || querier.calls[1] != "CreateForm" {
			t.Fatalf("expected property to be created before form, got %v", querier.calls)
		}
		if querier.createdFormArg == nil || querier.createdFormArg.PropertyID != property.ID {
			t.Fatalf("expected created form property ID to be %d, got %#v", property.ID, querier.createdFormArg)
		}
		cachedForm, needsRefresh, err := store.GetCachedFormByExternalID(context.Background(), UUIDToString(form.ExternalID))
		if err != nil {
			t.Fatalf("expected cached form, got %v", err)
		}
		if needsRefresh {
			t.Fatalf("expected cached form not to need refresh")
		}
		if cachedForm != form {
			t.Fatalf("expected cached form to match created form")
		}
	})

	t.Run("SkipsPropertyNameSuffixWhenItWouldOverflow", func(t *testing.T) {
		formName := strings.Repeat("a", maxPropertyNameLength)
		property := &dbgen.Property{
			ID:         123,
			Name:       formName,
			CreatorID:  Int(12),
			OrgID:      Int(1),
			OrgOwnerID: Int(99),
		}
		querier := &createFormQuerierStub{
			QuerierStub: &QuerierStub{},
			property:    property,
			form: &dbgen.Form{
				ID:         456,
				ExternalID: TestPropertyUUID,
				URL:        "https://example.com/submit",
				PropertyID: property.ID,
				Enabled:    true,
				Method:     dbgen.FormMethodPost,
			},
		}
		store := &BusinessStoreImpl{
			querier: querier,
			cache:   NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}),
		}
		propertyParams := &dbgen.CreatePropertyParams{
			CreatorID: Int(12),
			Domain:    "example.com",
		}
		formParams := &dbgen.CreateFormParams{
			Name:   formName,
			URL:    "https://example.com/submit",
			Fields: []byte(`{"email":"text"}`),
		}

		_, _, _, err := store.CreateNewForm(context.Background(), propertyParams, formParams, &dbgen.Organization{ID: 1, UserID: Int(99)})
		if err != nil {
			t.Fatalf("expected form creation to succeed, got %v", err)
		}
		if propertyParams.Name != formName {
			t.Fatalf("expected generated property name %q, got %q", formName, propertyParams.Name)
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, _, err := store.CreateNewForm(context.Background(), &dbgen.CreatePropertyParams{Name: "valid"}, &dbgen.CreateFormParams{}, &dbgen.Organization{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplUpdateProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateProperty(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1}, &dbgen.UpdatePropertyParams{Name: "valid"})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateProperty(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1}, &dbgen.UpdatePropertyParams{Name: "valid"})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.UpdateProperty(context.Background(), nil, nil, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplUpdateForm(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateForm(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1}, &dbgen.UpdateFormParams{Name: "valid"})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateForm(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1}, &dbgen.UpdateFormParams{Name: "valid"})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.UpdateForm(context.Background(), nil, nil, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplSoftDeleteProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.SoftDeleteProperty(context.Background(), &dbgen.Property{ID: 1}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.SoftDeleteProperty(context.Background(), &dbgen.Property{ID: 1}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplSoftDeleteProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.SoftDeleteProperties(context.Background(), []int32{1}, &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1})
		if !errors.Is(err, ErrPermissions) {
			t.Errorf("expected ErrPermissions, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.SoftDeleteProperties(context.Background(), []int32{1}, &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1})
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected expectedErr, got %v", err)
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.SoftDeleteProperties(context.Background(), []int32{1}, nil, &dbgen.Organization{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveOrgProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RetrieveOrgProperties(context.Background(), &dbgen.Organization{ID: 1}, 1, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RetrieveOrgProperties(context.Background(), &dbgen.Organization{ID: 1}, 1, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.RetrieveOrgProperties(context.Background(), &dbgen.Organization{}, 0, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveOrgForms(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RetrieveOrgForms(context.Background(), &dbgen.Organization{ID: 1}, 1, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RetrieveOrgForms(context.Background(), &dbgen.Organization{ID: 1}, 1, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.RetrieveOrgForms(context.Background(), &dbgen.Organization{}, 0, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("FirstPageReturnsLimitedResultsAndHasMore", func(t *testing.T) {
		querier := &retrieveUserFormsQuerierStub{
			QuerierStub: &QuerierStub{},
			forms: []*dbgen.Form{
				{ID: 1, URL: "https://one.example/submit", Enabled: true},
				{ID: 2, URL: "https://two.example/submit", Enabled: true},
				{ID: 3, URL: "https://three.example/submit", Enabled: true},
			},
		}

		store := &BusinessStoreImpl{
			querier: querier,
			cache:   NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}),
		}

		forms, hasMore, err := store.RetrieveOrgForms(context.Background(), &dbgen.Organization{ID: 1}, 0, 2)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(forms) != 2 {
			t.Fatalf("expected 2 forms, got %d", len(forms))
		}
		if !hasMore {
			t.Fatalf("expected hasMore to be true")
		}
		if querier.getOrgFormsArg == nil {
			t.Fatalf("expected GetOrgForms to receive args")
		}
		if querier.getOrgFormsArg.OrgID.Int32 != 1 {
			t.Fatalf("expected org ID 1, got %d", querier.getOrgFormsArg.OrgID.Int32)
		}
		if querier.getOrgFormsArg.Offset != 0 {
			t.Fatalf("expected offset 0, got %d", querier.getOrgFormsArg.Offset)
		}
	})
}

func TestBusinessStoreImplUpdateOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateOrganization(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateOrganization(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplSoftDeleteOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.SoftDeleteOrganization(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.SoftDeleteOrganization(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrganizationUsers(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrganizationUsers(context.Background(), 1)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrganizationUsers(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrganizationUsersWithEmailInvites(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrganizationUsersWithEmailInvites(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrganizationUsersWithEmailInvites(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplInviteUserToOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.InviteUserToOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.InviteUserToOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplInviteEmailToOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.InviteEmailToOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.InviteEmailToOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedOrgInviteByID(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedOrgInviteByID(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedOrgInviteByID(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplLinkOrgInviteToUser(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.LinkOrgInviteToUser(context.Background(), 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.LinkOrgInviteToUser(context.Background(), 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplJoinOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.JoinOrg(context.Background(), 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.JoinOrg(context.Background(), 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplLeaveOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.LeaveOrg(context.Background(), 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.LeaveOrg(context.Background(), 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRemoveUserFromOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RemoveUserFromOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, 1)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RemoveUserFromOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRemoveEmailInviteFromOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RemoveEmailInviteFromOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RemoveEmailInviteFromOrg(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateUserSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateUserSubscription(context.Background(), &dbgen.User{ID: 1}, &dbgen.Subscription{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateUserSubscription(context.Background(), &dbgen.User{ID: 1}, &dbgen.Subscription{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.UpdateUserSubscription(context.Background(), &dbgen.User{}, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplUpdateUser(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.UpdateUser(context.Background(), &dbgen.User{ID: 1}, "valid", "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.UpdateUser(context.Background(), &dbgen.User{ID: 1}, "valid", "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserAPIKeys(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserAPIKeys(context.Background(), 1)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserAPIKeys(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.UpdateAPIKey(context.Background(), &dbgen.User{ID: 1}, &dbgen.APIKey{ID: 1}, time.Now(), false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.UpdateAPIKey(context.Background(), &dbgen.User{ID: 1}, &dbgen.APIKey{ID: 1}, time.Now(), false)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.UpdateAPIKey(context.Background(), &dbgen.User{}, &dbgen.APIKey{}, time.Time{}, false)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplCreateAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.CreateAPIKey(context.Background(), &dbgen.User{ID: 1}, &dbgen.CreateAPIKeyParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.CreateAPIKey(context.Background(), &dbgen.User{ID: 1}, &dbgen.CreateAPIKeyParams{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.CreateAPIKey(context.Background(), &dbgen.User{}, &dbgen.CreateAPIKeyParams{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRotateAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RotateAPIKey(context.Background(), &dbgen.User{ID: 1}, 1)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RotateAPIKey(context.Background(), &dbgen.User{ID: 1}, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteAPIKey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.DeleteAPIKey(context.Background(), &dbgen.User{ID: 1}, 1)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.DeleteAPIKey(context.Background(), &dbgen.User{ID: 1}, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateAPIKeysLastUsedAt(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.UpdateAPIKeysLastUsedAt(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.UpdateAPIKeysLastUsedAt(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUsersWithoutSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUsersWithoutSubscription(context.Background(), []int32{1})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUsersWithoutSubscription(context.Background(), []int32{1})
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected expectedErr, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveLock(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveLock(context.Background(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveLock(context.Background(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveLock(context.Background(), "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplAcquireLock(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.AcquireLock(context.Background(), "valid", []byte("val"), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.AcquireLock(context.Background(), "valid", []byte("val"), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.AcquireLock(context.Background(), "", nil, time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplReleaseLock(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.ReleaseLock(context.Background(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.ReleaseLock(context.Background(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteDeletedRecords(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteDeletedRecords(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteDeletedRecords(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.DeleteDeletedRecords(context.Background(), time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveSoftDeletedProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSoftDeletedProperties(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSoftDeletedProperties(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveSoftDeletedProperties(context.Background(), time.Time{}, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteProperties(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteProperties(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSoftDeletedForms(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSoftDeletedForms(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSoftDeletedForms(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveSoftDeletedForms(context.Background(), time.Time{}, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteForms(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteForms(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteForms(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSoftDeletedOrganizations(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSoftDeletedOrganizations(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSoftDeletedOrganizations(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveSoftDeletedOrganizations(context.Background(), time.Time{}, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteOrganizations(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteOrganizations(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteOrganizations(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSoftDeletedUsers(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSoftDeletedUsers(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSoftDeletedUsers(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveSoftDeletedUsers(context.Background(), time.Time{}, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteUsers(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteUsers(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUsers(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSystemNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSystemNotification(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSystemNotification(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveSystemUserNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveSystemUserNotification(context.Background(), time.Now(), 1)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveSystemUserNotification(context.Background(), time.Now(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateSystemNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateSystemNotification(context.Background(), "valid", time.Now(), nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateSystemNotification(context.Background(), "valid", time.Now(), nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.CreateSystemNotification(context.Background(), "", time.Time{}, nil, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveProperties(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveProperties(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveProperties(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserPropertiesCount(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserPropertiesCount(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserPropertiesCount(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedPropertyBySitekey(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.GetCachedPropertyBySitekey(context.Background(), TestPropertySitekey)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.GetCachedPropertyBySitekey(context.Background(), TestPropertySitekey)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.GetCachedPropertyBySitekey(context.Background(), "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveUser(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUser(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUser(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RetrieveUserOrganization(context.Background(), &dbgen.User{ID: 1}, 1)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RetrieveUserOrganization(context.Background(), &dbgen.User{ID: 1}, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrgProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrgProperty(context.Background(), &dbgen.Organization{ID: 1}, 1)
		if !errors.Is(err, ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrgProperty(context.Background(), &dbgen.Organization{ID: 1}, 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateNewAccount(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, _, err := store.CreateNewAccount(context.Background(), &dbgen.CreateSubscriptionParams{ExternalSubscriptionID: Text("123")}, testEmail, "valid", "valid", 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, _, err := store.CreateNewAccount(context.Background(), &dbgen.CreateSubscriptionParams{ExternalSubscriptionID: Text("123")}, testEmail, "valid", "valid", 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, _, err := store.CreateNewAccount(context.Background(), &dbgen.CreateSubscriptionParams{}, "", "", "", 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplCreateNotificationTemplate(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateNotificationTemplate(context.Background(), "valid", "valid", "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateNotificationTemplate(context.Background(), "valid", "valid", "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveNotificationTemplate(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveNotificationTemplate(context.Background(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveNotificationTemplate(context.Background(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateUserNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateUserNotification(context.Background(), nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateUserNotification(context.Background(), nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.CreateUserNotification(context.Background(), &common.ScheduledNotification{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrievePendingUserNotifications(t *testing.T) {
	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrievePendingUserNotifications(context.Background(), time.Time{}, 0, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplMarkUserNotificationsAttempted(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.MarkUserNotificationsAttempted(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.MarkUserNotificationsAttempted(context.Background(), []int32{1, 2})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplMarkUserNotificationsProcessed(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.MarkUserNotificationsProcessed(context.Background(), []int32{1, 2}, time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.MarkUserNotificationsProcessed(context.Background(), []int32{1, 2}, time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteUnusedNotificationTemplates(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteUnusedNotificationTemplates(context.Background(), time.Now(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUnusedNotificationTemplates(context.Background(), time.Now(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplDeleteSentUserNotifications(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteSentUserNotifications(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteSentUserNotifications(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.DeleteSentUserNotifications(context.Background(), time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteUnsentUserNotifications(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteUnsentUserNotifications(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteUnsentUserNotifications(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.DeleteUnsentUserNotifications(context.Background(), time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeletePendingUserNotification(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeletePendingUserNotification(context.Background(), &dbgen.User{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeletePendingUserNotification(context.Background(), &dbgen.User{ID: 1}, "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplExpireInternalTrials(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.ExpireInternalTrials(context.Background(), time.Now(), time.Now(), "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.ExpireInternalTrials(context.Background(), time.Now(), time.Now(), "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplMoveProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.MoveProperty(context.Background(), &dbgen.User{ID: 1}, &dbgen.Property{ID: 1}, &dbgen.GetUserOrganizationsRow{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.MoveProperty(context.Background(), &dbgen.User{ID: 1}, &dbgen.Property{ID: 1}, &dbgen.GetUserOrganizationsRow{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.MoveProperty(context.Background(), &dbgen.User{}, &dbgen.Property{}, &dbgen.GetUserOrganizationsRow{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeactivateForms(t *testing.T) {
	ctx := context.Background()
	form := &dbgen.Form{
		ID:         1,
		ExternalID: testAPIKeyUUID,
		OrgID:      Int(10),
		Active:     false,
		Enabled:    true,
	}
	stub := &deactivateFormsQuerierStub{
		QuerierStub: &QuerierStub{},
		forms:       []*dbgen.Form{form},
	}
	store := &BusinessStoreImpl{
		querier: stub,
		cache:   NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}),
	}
	store.cacheForm(ctx, form)
	if err := store.cache.Set(ctx, OrgFormsCacheKey(10, orgFormsCacheKeyStr), []*dbgen.Form{form}); err != nil {
		t.Fatal(err)
	}
	if err := store.cache.Set(ctx, orgFormsCountCacheKey(10), int64(1)); err != nil {
		t.Fatal(err)
	}

	forms, err := store.DeactivateForms(ctx, []int32{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 {
		t.Fatalf("DeactivateForms() returned %d forms, want 1", len(forms))
	}
	if len(stub.ids) != 1 || stub.ids[0] != 1 {
		t.Fatalf("DeactivateForms() ids = %v, want [1]", stub.ids)
	}
	if _, err := store.cache.Get(ctx, OrgFormsCacheKey(10, orgFormsCacheKeyStr)); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("DeactivateForms() org forms cache err = %v, want ErrCacheMiss", err)
	}
	if _, err := store.cache.Get(ctx, orgFormsCountCacheKey(10)); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("DeactivateForms() org forms count cache err = %v, want ErrCacheMiss", err)
	}
}

func TestBusinessStoreImplMoveForm(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, _, err := store.MoveForm(
			context.Background(),
			&dbgen.User{ID: 1},
			&dbgen.Form{ID: 1, PropertyID: 2, OrgID: Int(1)},
			&dbgen.Property{ID: 2},
			&dbgen.GetUserOrganizationsRow{Organization: dbgen.Organization{ID: 2, UserID: Int(9)}},
		)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, _, err := store.MoveForm(context.Background(), &dbgen.User{ID: 1}, &dbgen.Form{ID: 1, PropertyID: 2}, &dbgen.Property{ID: 2}, &dbgen.GetUserOrganizationsRow{})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, _, err := store.MoveForm(context.Background(), &dbgen.User{}, &dbgen.Form{}, &dbgen.Property{}, &dbgen.GetUserOrganizationsRow{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplSoftDeleteForm(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.SoftDeleteForm(context.Background(), &dbgen.Form{ID: 1, PropertyID: 2}, &dbgen.Property{ID: 2}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.SoftDeleteForm(context.Background(), &dbgen.Form{ID: 1, PropertyID: 2}, &dbgen.Property{ID: 2}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.SoftDeleteForm(context.Background(), nil, nil, nil, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplTransferOrganization(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.TransferOrganization(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.TransferOrganization(context.Background(), &dbgen.User{ID: 1}, &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.TransferOrganization(context.Background(), &dbgen.User{}, &dbgen.Organization{}, &dbgen.User{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteOldAuditLogs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteOldAuditLogs(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteOldAuditLogs(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.DeleteOldAuditLogs(context.Background(), time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplGetCachedAuditLogs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedAuditLogs(context.Background(), &dbgen.User{ID: 1}, 1, time.Now(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedAuditLogs(context.Background(), &dbgen.User{ID: 1}, 1, time.Now(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.GetCachedAuditLogs(context.Background(), &dbgen.User{}, 0, time.Time{}, time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveUserAuditLogs(t *testing.T) {
	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveUserAuditLogs(context.Background(), &dbgen.User{}, 0, time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrievePropertyAuditLogs(t *testing.T) {
	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrievePropertyAuditLogs(context.Background(), &dbgen.Property{}, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveOrganizationAuditLogs(t *testing.T) {
	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveOrganizationAuditLogs(context.Background(), &dbgen.Organization{}, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplCreateNewAsyncTask(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.CreateNewAsyncTask(context.Background(), nil, "valid", &dbgen.User{ID: 1}, time.Now(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.CreateNewAsyncTask(context.Background(), nil, "valid", &dbgen.User{ID: 1}, time.Now(), "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.CreateNewAsyncTask(context.Background(), nil, "", nil, time.Time{}, "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveAsyncTask(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveAsyncTask(context.Background(), pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveAsyncTask(context.Background(), pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrievePendingAsyncTasks(t *testing.T) {
	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrievePendingAsyncTasks(context.Background(), 0, time.Time{}, 0)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteOldAsyncTasks(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.DeleteOldAsyncTasks(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.DeleteOldAsyncTasks(context.Background(), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.DeleteOldAsyncTasks(context.Background(), time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplUpdateAsyncTask(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.UpdateAsyncTask(context.Background(), pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, []byte("val"), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.UpdateAsyncTask(context.Background(), pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, []byte("val"), time.Now())
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		err := store.UpdateAsyncTask(context.Background(), pgtype.UUID{}, nil, time.Time{})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveOrgOwnerWithSubscription(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.RetrieveOrgOwnerWithSubscription(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.RetrieveOrgOwnerWithSubscription(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrgPropertiesCount(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrgPropertiesCount(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrgPropertiesCount(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveOrgFormsCount(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveOrgFormsCount(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveOrgFormsCount(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("CachesCount", func(t *testing.T) {
		querier := &retrieveUserFormsQuerierStub{
			QuerierStub: &QuerierStub{},
			count:       7,
		}

		store := &BusinessStoreImpl{
			querier: querier,
			cache:   NewStaticCache[CacheKey, any](1000, &CacheMissingValue{}),
		}

		count, err := store.RetrieveUserFormsCount(context.Background(), 1)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if count != 7 {
			t.Fatalf("expected count 7, got %d", count)
		}

		querier.count = 13

		cachedCount, err := store.RetrieveUserFormsCount(context.Background(), 1)
		if err != nil {
			t.Fatalf("expected cached count without error, got %v", err)
		}
		if cachedCount != 7 {
			t.Fatalf("expected cached count 7, got %d", cachedCount)
		}
		if querier.countCalls != 1 {
			t.Fatalf("expected count query to run once, got %d", querier.countCalls)
		}
	})
}

func TestBusinessStoreImplRetrieveDifficultyRulesByPropertyIDs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveDifficultyRulesByPropertyIDs(context.Background(), map[int32]uint{1: 1})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveDifficultyRulesByPropertyIDs(context.Background(), map[int32]uint{1: 1})
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected expectedErr, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveDifficultyRulesByOrgIDs(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveDifficultyRulesByOrgIDs(context.Background(), map[int32]uint{1: 1})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveDifficultyRulesByOrgIDs(context.Background(), map[int32]uint{1: 1})
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected expectedErr, got %v", err)
		}
	})
}

func TestBusinessStoreImplGetCachedOrgRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedOrgRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedOrgRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedPropertyRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.GetCachedPropertyRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.GetCachedPropertyRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplCreateDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.CreateDifficultyRule(context.Background(), nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.CreateDifficultyRule(context.Background(), nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.CreateDifficultyRule(context.Background(), nil, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplGetCachedCompiledPropertyRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.GetCachedCompiledPropertyRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.GetCachedCompiledPropertyRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplGetCachedCompiledOrgRules(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.GetCachedCompiledOrgRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.GetCachedCompiledOrgRules(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveDifficultyRule(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveDifficultyRule(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplUpdateDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.UpdateDifficultyRule(context.Background(), nil, nil, nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.UpdateDifficultyRule(context.Background(), nil, nil, nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.UpdateDifficultyRule(context.Background(), nil, nil, nil, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplDeleteDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.DeleteDifficultyRule(context.Background(), nil, nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.DeleteDifficultyRule(context.Background(), nil, nil, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.DeleteDifficultyRule(context.Background(), nil, nil, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplMoveDifficultyRule(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.MoveDifficultyRule(context.Background(), nil, nil, 0, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.MoveDifficultyRule(context.Background(), nil, nil, 0, nil)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, _, err := store.MoveDifficultyRule(context.Background(), nil, nil, 0, nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRebalanceDifficultyRulesForProperty(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.RebalanceDifficultyRulesForProperty(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.RebalanceDifficultyRulesForProperty(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRebalanceDifficultyRulesForOrg(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		err := store.RebalanceDifficultyRulesForOrg(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		err := store.RebalanceDifficultyRulesForOrg(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplMoveDifficultyRuleWithRebalancing(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, _, err := store.MoveDifficultyRuleWithRebalancing(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.DifficultyRule{ID: 1}, 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, _, err := store.MoveDifficultyRuleWithRebalancing(context.Background(), &dbgen.Organization{ID: 1}, &dbgen.DifficultyRule{ID: 1}, 1, &dbgen.User{ID: 1})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestBusinessStoreImplRetrieveUserSettings(t *testing.T) {
	t.Run("ErrNoRows", func(t *testing.T) {
		store := setupTestStore(t, pgx.ErrNoRows)
		_, err := store.RetrieveUserSettings(context.Background(), 1)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUserSettings(context.Background(), 1)
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

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUsersWithPendingWeeklyReport(context.Background(), 1, 1, "valid", "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveUsersWithPendingWeeklyReport(context.Background(), 0, 0, "", "", "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestBusinessStoreImplRetrieveUsersWithPendingMonthlyReport(t *testing.T) {

	t.Run("GenericError", func(t *testing.T) {
		expectedErr := errors.New("db error")
		store := setupTestStore(t, expectedErr)
		_, err := store.RetrieveUsersWithPendingMonthlyReport(context.Background(), 1, 1, "valid", "valid", "valid")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		store := setupTestStore(t, nil)
		_, err := store.RetrieveUsersWithPendingMonthlyReport(context.Background(), 0, 0, "", "", "")
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}
