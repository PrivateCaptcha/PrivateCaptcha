package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func TestUserAuditLogInitFromUser(t *testing.T) {
	tests := []struct {
		name     string
		oldValue *db.AuditLogUser
		newValue *db.AuditLogUser
		wantErr  bool
	}{
		{
			name: "name change",
			oldValue: &db.AuditLogUser{
				Name:  "Old Name",
				Email: "test@example.com",
			},
			newValue: &db.AuditLogUser{
				Name:  "New Name",
				Email: "test@example.com",
			},
			wantErr: false,
		},
		{
			name: "email change",
			oldValue: &db.AuditLogUser{
				Name:  "Test User",
				Email: "old@example.com",
			},
			newValue: &db.AuditLogUser{
				Name:  "Test User",
				Email: "new@example.com",
			},
			wantErr: false,
		},
		{
			name:     "nil values",
			oldValue: nil,
			newValue: nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &userAuditLog{}
			err := ul.initFromUser(tt.oldValue, tt.newValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromUser() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ul.Resource != "User" {
				t.Errorf("initFromUser() Resource = %v, want User", ul.Resource)
			}
		})
	}
}

func TestUserAuditLogInitFromOrg(t *testing.T) {
	tests := []struct {
		name     string
		oldValue *db.AuditLogOrg
		newValue *db.AuditLogOrg
		wantErr  bool
	}{
		{
			name: "org name change",
			oldValue: &db.AuditLogOrg{
				ID:   1,
				Name: "Old Org",
			},
			newValue: &db.AuditLogOrg{
				ID:   1,
				Name: "New Org",
			},
			wantErr: false,
		},
		{
			name: "org creation",
			oldValue: nil,
			newValue: &db.AuditLogOrg{
				ID:   1,
				Name: "New Org",
			},
			wantErr: false,
		},
		{
			name: "org deletion",
			oldValue: &db.AuditLogOrg{
				ID:   1,
				Name: "Old Org",
			},
			newValue: nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &userAuditLog{}
			err := ul.initFromOrg(tt.oldValue, tt.newValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromOrg() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUserAuditLogInitFromSubscription(t *testing.T) {
	planService := billing.NewPlanService(nil)

	tests := []struct {
		name     string
		oldValue *db.AuditLogSubscription
		newValue *db.AuditLogSubscription
		wantErr  bool
	}{
		{
			name: "subscription status change",
			oldValue: &db.AuditLogSubscription{
				Source: "external",
				Status: "active",
			},
			newValue: &db.AuditLogSubscription{
				Source: "external",
				Status: "canceled",
			},
			wantErr: false,
		},
		{
			name: "subscription creation",
			oldValue: nil,
			newValue: &db.AuditLogSubscription{
				Source:            "external",
				Status:            "active",
				ExternalProductID: "prod_123",
				ExternalPriceID:   "price_123",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &userAuditLog{}
			err := ul.initFromSubscription(tt.oldValue, tt.newValue, planService, "production")
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromSubscription() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ul.Resource != "Subscription" {
				t.Errorf("initFromSubscription() Resource = %v, want Subscription", ul.Resource)
			}
		})
	}
}

func TestUserAuditLogInitFromOrgUser(t *testing.T) {
	tests := []struct {
		name     string
		oldValue *db.AuditLogOrgUser
		newValue *db.AuditLogOrgUser
		wantErr  bool
	}{
		{
			name: "org user creation",
			oldValue: nil,
			newValue: &db.AuditLogOrgUser{
				OrgName: "Test Org",
				UserID:  1,
				Email:   "user@example.com",
				Level:   "member",
			},
			wantErr: false,
		},
		{
			name: "org user deletion",
			oldValue: &db.AuditLogOrgUser{
				OrgName: "Test Org",
				UserID:  1,
				Email:   "user@example.com",
				Level:   "member",
			},
			newValue: nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &userAuditLog{}
			err := ul.initFromOrgUser(tt.oldValue, tt.newValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromOrgUser() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ul.Resource != "Organization 'Test Org'" {
				t.Errorf("initFromOrgUser() Resource = %v, want Organization 'Test Org'", ul.Resource)
			}
		})
	}
}

func TestUserAuditLogInitFromProperty(t *testing.T) {
	tests := []struct {
		name     string
		oldValue *db.AuditLogProperty
		newValue *db.AuditLogProperty
		wantErr  bool
	}{
		{
			name: "property name change",
			oldValue: &db.AuditLogProperty{
				Name:   "Old Property",
				Domain: "example.com",
				Level:  1,
			},
			newValue: &db.AuditLogProperty{
				Name:   "New Property",
				Domain: "example.com",
				Level:  1,
			},
			wantErr: false,
		},
		{
			name: "property level change",
			oldValue: &db.AuditLogProperty{
				Name:   "Test Property",
				Domain: "example.com",
				Level:  1,
			},
			newValue: &db.AuditLogProperty{
				Name:   "Test Property",
				Domain: "example.com",
				Level:  2,
			},
			wantErr: false,
		},
		{
			name: "property creation",
			oldValue: nil,
			newValue: &db.AuditLogProperty{
				Name:   "New Property",
				Domain: "example.com",
				Level:  1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &userAuditLog{}
			err := ul.initFromProperty(tt.oldValue, tt.newValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromProperty() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUserAuditLogInitFromAPIKey(t *testing.T) {
	now := time.Now()
	later := now.Add(24 * time.Hour)

	tests := []struct {
		name     string
		oldValue *db.AuditLogAPIKey
		newValue *db.AuditLogAPIKey
		wantErr  bool
	}{
		{
			name: "api key expiration change",
			oldValue: &db.AuditLogAPIKey{
				Name:      "Test Key",
				ExpiresAt: common.JSONTime(now),
			},
			newValue: &db.AuditLogAPIKey{
				Name:      "Test Key",
				ExpiresAt: common.JSONTime(later),
			},
			wantErr: false,
		},
		{
			name: "api key creation",
			oldValue: nil,
			newValue: &db.AuditLogAPIKey{
				Name:      "New Key",
				ExpiresAt: common.JSONTime(later),
			},
			wantErr: false,
		},
		{
			name: "api key deletion",
			oldValue: &db.AuditLogAPIKey{
				Name:      "Old Key",
				ExpiresAt: common.JSONTime(now),
			},
			newValue: nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &userAuditLog{}
			err := ul.initFromAPIKey(tt.oldValue, tt.newValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromAPIKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ul.Resource != "API key" && tt.oldValue == nil && tt.newValue == nil {
				t.Errorf("initFromAPIKey() Resource = %v, want API key", ul.Resource)
			}
		})
	}
}

func TestUserAuditLogInitFromAccess(t *testing.T) {
	tests := []struct {
		name    string
		log     *dbgen.AuditLog
		payload *db.AuditLogAccess
		wantErr bool
	}{
		{
			name: "access with entity name",
			log: &dbgen.AuditLog{
				EntityTable: "properties",
			},
			payload: &db.AuditLogAccess{
				View:       "details",
				EntityName: "Test Property",
			},
			wantErr: false,
		},
		{
			name: "access without entity name",
			log: &dbgen.AuditLog{
				EntityTable: "api_keys",
			},
			payload: &db.AuditLogAccess{
				View: "list",
			},
			wantErr: false,
		},
		{
			name: "nil payload",
			log: &dbgen.AuditLog{
				EntityTable: "users",
			},
			payload: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &userAuditLog{}
			err := ul.initFromAccess(tt.log, tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromAccess() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != errUnexpectedAuditLogPayload {
				t.Errorf("initFromAccess() error = %v, want %v", err, errUnexpectedAuditLogPayload)
			}
		})
	}
}

func TestNewUserAuditLog(t *testing.T) {
	ctx := context.Background()
	planService := billing.NewPlanService(nil)

	server := &Server{
		PlanService: planService,
		Stage:       "production",
	}

	tests := []struct {
		name    string
		log     *dbgen.AuditLog
		wantErr bool
	}{
		{
			name: "user audit log",
			log: &dbgen.AuditLog{
				ID:          1,
				UserID:      db.Int(1),
				Action:      dbgen.AuditLogActionCreate,
				EntityTable: db.TableNameUsers,
				CreatedAt:   db.Timestampz(time.Now()),
				Source:      dbgen.AuditLogSourcePortal,
				NewValue:    mustMarshalJSON(&db.AuditLogUser{Name: "Test User", Email: "test@example.com"}),
			},
			wantErr: false,
		},
		{
			name: "org audit log",
			log: &dbgen.AuditLog{
				ID:          2,
				UserID:      db.Int(1),
				Action:      dbgen.AuditLogActionCreate,
				EntityTable: db.TableNameOrgs,
				CreatedAt:   db.Timestampz(time.Now()),
				Source:      dbgen.AuditLogSourcePortal,
				NewValue:    mustMarshalJSON(&db.AuditLogOrg{ID: 1, Name: "Test Org"}),
			},
			wantErr: false,
		},
		{
			name: "property audit log",
			log: &dbgen.AuditLog{
				ID:          3,
				UserID:      db.Int(1),
				Action:      dbgen.AuditLogActionCreate,
				EntityTable: db.TableNameProperties,
				CreatedAt:   db.Timestampz(time.Now()),
				Source:      dbgen.AuditLogSourcePortal,
				NewValue:    mustMarshalJSON(&db.AuditLogProperty{Name: "Test Property", Domain: "example.com"}),
			},
			wantErr: false,
		},
		{
			name: "api key audit log",
			log: &dbgen.AuditLog{
				ID:          4,
				UserID:      db.Int(1),
				Action:      dbgen.AuditLogActionCreate,
				EntityTable: db.TableNameAPIKeys,
				CreatedAt:   db.Timestampz(time.Now()),
				Source:      dbgen.AuditLogSourceApi,
				NewValue:    mustMarshalJSON(&db.AuditLogAPIKey{Name: "Test Key"}),
			},
			wantErr: false,
		},
		{
			name: "access audit log",
			log: &dbgen.AuditLog{
				ID:          5,
				UserID:      db.Int(1),
				Action:      dbgen.AuditLogActionAccess,
				EntityTable: db.TableNameProperties,
				CreatedAt:   db.Timestampz(time.Now()),
				Source:      dbgen.AuditLogSourcePortal,
				NewValue:    mustMarshalJSON(&db.AuditLogAccess{View: "details", EntityName: "Test"}),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul, err := server.newUserAuditLog(ctx, tt.log)
			if (err != nil) != tt.wantErr {
				t.Errorf("newUserAuditLog() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && ul == nil {
				t.Error("newUserAuditLog() returned nil without error")
			}
			if !tt.wantErr && ul != nil {
				if ul.Time == "" {
					t.Error("newUserAuditLog() Time is empty")
				}
				if ul.Action == "" {
					t.Error("newUserAuditLog() Action is empty")
				}
			}
		})
	}
}

func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestGetAuditLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/events", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.getAuditLogs(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != auditLogsTemplate {
		t.Errorf("Expected view to be %s, got %s", auditLogsTemplate, viewModel.View)
	}

	if viewModel.AuditEvent == nil {
		t.Error("Expected AuditEvent to be populated")
	}
}

func TestCreateAuditLogsContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	renderCtx, err := server.CreateAuditLogsContext(ctx, user, 14, 0)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if renderCtx == nil {
		t.Fatal("Expected render context to be populated, got nil")
	}

	if renderCtx.Days != 14 {
		t.Errorf("Expected Days to be 14, got %d", renderCtx.Days)
	}

	if renderCtx.AuditLogs == nil {
		t.Error("Expected AuditLogs to be initialized")
	}
}
