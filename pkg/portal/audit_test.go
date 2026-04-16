package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
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
			ul := &UserAuditLog{}
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
		name         string
		oldValue     *db.AuditLogOrg
		newValue     *db.AuditLogOrg
		wantErr      bool
		wantProperty string
		wantValue    string
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
			wantErr:      false,
			wantProperty: "Name",
			wantValue:    "New Org",
		},
		{
			name:     "org creation",
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
		{
			name: "org transfer",
			oldValue: &db.AuditLogOrg{
				ID:   1,
				Name: "Test Org",
			},
			newValue: &db.AuditLogOrg{
				ID:            1,
				Name:          "Test Org",
				NewOwnerID:    2,
				NewOwnerEmail: "newowner@example.com",
			},
			wantErr:      false,
			wantProperty: "Owner",
			wantValue:    "newo****@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &UserAuditLog{}
			err := ul.initFromOrg(tt.oldValue, tt.newValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromOrg() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantProperty != "" && ul.Property != tt.wantProperty {
				t.Errorf("initFromOrg() Property = %v, want %v", ul.Property, tt.wantProperty)
			}
			if tt.wantValue != "" && ul.Value != tt.wantValue {
				t.Errorf("initFromOrg() Value = %v, want %v", ul.Value, tt.wantValue)
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
			name:     "subscription creation",
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
			ul := &UserAuditLog{}
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
			name:     "org user creation",
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
			ul := &UserAuditLog{}
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
			name:     "property creation",
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
			ul := &UserAuditLog{}
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
			name:     "api key creation",
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
			ul := &UserAuditLog{}
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
			ul := &UserAuditLog{}
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
			ul, err := server.NewUserAuditLog(ctx, tt.log)
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

func TestAuditLogParserExtension(t *testing.T) {
	ctx := context.Background()
	planService := billing.NewPlanService(nil)

	// Create a custom parser that handles a custom table type
	customParser := func(ctx context.Context, log *dbgen.AuditLog, ul *UserAuditLog) error {
		if log.EntityTable == "custom_table" {
			ul.Resource = "Custom Resource"
			ul.Property = "Custom Property"
			ul.Value = "custom_value"
		}
		return nil
	}

	// Test with custom parser set
	serverWithParser := &Server{
		PlanService:    planService,
		Stage:          "production",
		AuditLogParser: customParser,
	}

	// Test that custom table type is handled by the parser
	customLog := &dbgen.AuditLog{
		ID:          1,
		UserID:      db.Int(1),
		Action:      dbgen.AuditLogActionCreate,
		EntityTable: "custom_table",
		CreatedAt:   db.Timestampz(time.Now()),
		Source:      dbgen.AuditLogSourcePortal,
	}

	ul, err := serverWithParser.NewUserAuditLog(ctx, customLog)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if ul.Resource != "Custom Resource" {
		t.Errorf("Expected Resource to be 'Custom Resource', got '%s'", ul.Resource)
	}
	if ul.Property != "Custom Property" {
		t.Errorf("Expected Property to be 'Custom Property', got '%s'", ul.Property)
	}
	if ul.Value != "custom_value" {
		t.Errorf("Expected Value to be 'custom_value', got '%s'", ul.Value)
	}

	// Test that standard table types still work with custom parser
	standardLog := &dbgen.AuditLog{
		ID:          2,
		UserID:      db.Int(1),
		Action:      dbgen.AuditLogActionCreate,
		EntityTable: db.TableNameUsers,
		CreatedAt:   db.Timestampz(time.Now()),
		Source:      dbgen.AuditLogSourcePortal,
		NewValue:    mustMarshalJSON(&db.AuditLogUser{Name: "Test User", Email: "test@example.com"}),
	}

	ul2, err := serverWithParser.NewUserAuditLog(ctx, standardLog)
	if err != nil {
		t.Fatalf("Expected no error for standard table, got: %v", err)
	}
	if ul2.Resource != "User" {
		t.Errorf("Expected Resource to be 'User', got '%s'", ul2.Resource)
	}

	// Test without custom parser - custom table types should return empty fields
	serverWithoutParser := &Server{
		PlanService: planService,
		Stage:       "production",
	}

	ul3, err := serverWithoutParser.NewUserAuditLog(ctx, customLog)
	if err != nil {
		t.Fatalf("Expected no error without parser, got: %v", err)
	}
	if ul3.Resource != "" {
		t.Errorf("Expected Resource to be empty without parser, got '%s'", ul3.Resource)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

func TestInitFromSubscriptionPlan(t *testing.T) {
	planService := billing.NewPlanService(nil)
	ul := &UserAuditLog{}

	sub := &db.AuditLogSubscription{
		Source:            "internal",
		ExternalProductID: "test-product",
		ExternalPriceID:   "test-price",
	}

	ul.initFromSubscriptionPlan(sub, planService, "production")

	if ul.Property != "Product" {
		t.Errorf("Expected Property to be 'Product', got '%s'", ul.Property)
	}
}

func TestInitFromPropertyOrgChange(t *testing.T) {
	ul := &UserAuditLog{}

	oldValue := &db.AuditLogProperty{
		Name:  "Test Property",
		OrgID: 1,
	}
	newValue := &db.AuditLogProperty{
		Name:    "Test Property",
		OrgID:   2,
		OrgName: "New Org",
	}

	err := ul.initFromProperty(oldValue, newValue)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Organization" {
		t.Errorf("Expected Property to be 'Organization', got '%s'", ul.Property)
	}
}

func TestInitFromPropertyGrowthChange(t *testing.T) {
	ul := &UserAuditLog{}

	oldValue := &db.AuditLogProperty{
		Name:   "Test Property",
		Growth: "slow",
	}
	newValue := &db.AuditLogProperty{
		Name:   "Test Property",
		Growth: "fast",
	}

	err := ul.initFromProperty(oldValue, newValue)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Growth" {
		t.Errorf("Expected Property to be 'Growth', got '%s'", ul.Property)
	}

	if ul.Value != "fast" {
		t.Errorf("Expected Value to be 'fast', got '%s'", ul.Value)
	}
}

func TestInitFromPropertyMaxReplayCountChange(t *testing.T) {
	ul := &UserAuditLog{}

	oldValue := &db.AuditLogProperty{
		Name:           "Test Property",
		MaxReplayCount: 10,
	}
	newValue := &db.AuditLogProperty{
		Name:           "Test Property",
		MaxReplayCount: 20,
	}

	err := ul.initFromProperty(oldValue, newValue)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Replay count" {
		t.Errorf("Expected Property to be 'Replay count', got '%s'", ul.Property)
	}
}

func TestInitFromPropertyValidityChange(t *testing.T) {
	ul := &UserAuditLog{}

	oldValue := &db.AuditLogProperty{
		Name:                "Test Property",
		ValidityIntervalSec: 3600,
	}
	newValue := &db.AuditLogProperty{
		Name:                "Test Property",
		ValidityIntervalSec: 7200,
	}

	err := ul.initFromProperty(oldValue, newValue)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Validity" {
		t.Errorf("Expected Property to be 'Validity', got '%s'", ul.Property)
	}
}

func TestInitFromPropertyAllowSubdomainsChange(t *testing.T) {
	ul := &UserAuditLog{}

	oldValue := &db.AuditLogProperty{
		Name:            "Test Property",
		AllowSubdomains: false,
	}
	newValue := &db.AuditLogProperty{
		Name:            "Test Property",
		AllowSubdomains: true,
	}

	err := ul.initFromProperty(oldValue, newValue)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Subdomains" {
		t.Errorf("Expected Property to be 'Subdomains', got '%s'", ul.Property)
	}
}

func TestInitFromPropertyAllowLocalhostChange(t *testing.T) {
	ul := &UserAuditLog{}

	oldValue := &db.AuditLogProperty{
		Name:           "Test Property",
		AllowLocalhost: false,
	}
	newValue := &db.AuditLogProperty{
		Name:           "Test Property",
		AllowLocalhost: true,
	}

	err := ul.initFromProperty(oldValue, newValue)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Localhost" {
		t.Errorf("Expected Property to be 'Localhost', got '%s'", ul.Property)
	}
}

func TestInitFromSubscriptionSourceChange(t *testing.T) {
	planService := billing.NewPlanService(nil)
	ul := &UserAuditLog{}

	oldValue := &db.AuditLogSubscription{
		Source: "internal",
	}
	newValue := &db.AuditLogSubscription{
		Source: "external",
	}

	err := ul.initFromSubscription(oldValue, newValue, planService, "production")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Type" {
		t.Errorf("Expected Property to be 'Type', got '%s'", ul.Property)
	}
}

func TestInitFromSubscriptionCancelAtChange(t *testing.T) {
	planService := billing.NewPlanService(nil)
	ul := &UserAuditLog{}

	now := time.Now()
	later := now.Add(24 * time.Hour)

	oldValue := &db.AuditLogSubscription{
		Source:   "external",
		CancelAt: common.JSONTime(now),
	}
	newValue := &db.AuditLogSubscription{
		Source:   "external",
		CancelAt: common.JSONTime(later),
	}

	err := ul.initFromSubscription(oldValue, newValue, planService, "production")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Cancel" {
		t.Errorf("Expected Property to be 'Cancel', got '%s'", ul.Property)
	}
}

func TestInitFromAPIKeyPeriodChange(t *testing.T) {
	ul := &UserAuditLog{}

	now := time.Now()

	oldValue := &db.AuditLogAPIKey{
		Name:      "Test Key",
		Period:    24 * time.Hour,
		ExpiresAt: common.JSONTime(now),
	}
	newValue := &db.AuditLogAPIKey{
		Name:      "Test Key",
		Period:    48 * time.Hour,
		ExpiresAt: common.JSONTime(now),
	}

	err := ul.initFromAPIKey(oldValue, newValue)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul.Property != "Period" {
		t.Errorf("Expected Property to be 'Period', got '%s'", ul.Property)
	}
}

func TestAuditLogsDaysFromParam(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		input    string
		expected int
	}{
		{"14", 14},
		{"30", 30},
		{"90", 90},
		{"180", 180},
		{"365", 365},
		{"", 14},
		{"invalid", 14},
		{"7", 14},
		{"100", 14},
		{"-1", 14},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := auditLogsDaysFromParam(ctx, tc.input)
			if result != tc.expected {
				t.Errorf("auditLogsDaysFromParam(%q) = %d, want %d", tc.input, result, tc.expected)
			}
		})
	}
}

func TestMaxAuditLogsForDays(t *testing.T) {
	tests := []struct {
		days     int
		expected int
	}{
		{14, 1400},
		{30, 3000},
		{90, 9000},
		{180, 18000},
		{365, 36500},
	}

	for _, tc := range tests {
		result := maxAuditLogsForDays(tc.days)
		if result != tc.expected {
			t.Errorf("maxAuditLogsForDays(%d) = %d, want %d", tc.days, result, tc.expected)
		}
	}
}

func TestAuditLogEndpointsInvalidParams(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		wantCode int
	}{
		{"GetAuditLogsInvalidDays", "/auditlogs/events?days=999", http.StatusOK},
		{"GetAuditLogsInvalidPage", "/auditlogs/events?page=-1", http.StatusOK},
		{"ExportAuditLogsInvalidDays", "/auditlogs/export?days=abc", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestExportAuditLogsCSV(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create properties and persist their audit events synchronously
	_, auditEvent1, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "audit-export-test1.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	_, auditEvent2, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "audit-export-test2.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	auditLog := store.AuditLog().(*db.AuditLog)
	now := time.Now().UTC()
	auditEvent1.Timestamp = now
	auditEvent1.Source = common.AuditLogSourcePortal
	auditEvent2.Timestamp = now
	auditEvent2.Source = common.AuditLogSourcePortal
	if err := auditLog.PersistAuditLog(ctx, []*common.AuditLogEvent{auditEvent1, auditEvent2}); err != nil {
		t.Fatalf("Failed to persist audit logs: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Request CSV export
	req := httptest.NewRequest("GET", "/auditlogs/export?days=14", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	// Check content type
	contentType := w.Header().Get(common.HeaderContentType)
	if contentType != common.ContentTypeCSV {
		t.Errorf("Expected Content-Type %s, got %s", common.ContentTypeCSV, contentType)
	}

	// Check content disposition
	contentDisposition := w.Header().Get("Content-Disposition")
	if contentDisposition == "" {
		t.Error("Expected Content-Disposition header to be set")
	}

	// Check CSV content
	body := w.Body.String()
	if body == "" {
		t.Error("Expected non-empty CSV content")
	}

	// Verify CSV has header row
	if len(body) > 0 {
		lines := strings.Split(body, "\n")
		if len(lines) < 1 {
			t.Error("Expected at least one line in CSV (header)")
		}
		// Check header columns
		header := lines[0]
		if !strings.Contains(header, "id") || !strings.Contains(header, "action") {
			t.Error("Expected CSV header to contain 'id' and 'action' columns")
		}
	}

	// Verify CSV has data rows (not just header)
	lines := strings.Split(strings.TrimSpace(body), "\n")
	// Header + at least 2 data rows (2 property creations)
	if len(lines) < 3 {
		t.Errorf("Expected CSV to have at least 3 lines (header + 2 data rows), got %d", len(lines))
	}

	// Verify at least one data row contains "create" action from property creation
	hasCreateAction := false
	for _, line := range lines[1:] {
		if strings.Contains(line, "create") {
			hasCreateAction = true
			break
		}
	}
	if !hasCreateAction {
		t.Error("Expected at least one CSV row to contain 'create' action")
	}
}

func TestInitFromSubscriptionPlanWithValidPlan(t *testing.T) {
	// Use the internal trial plan which we know exists
	planService := billing.NewPlanService(nil)
	ul := &UserAuditLog{}

	trialPlan := planService.GetInternalTrialPlan()
	priceMonthly, priceYearly := trialPlan.PriceIDs()

	// Test with yearly price
	sub := &db.AuditLogSubscription{
		Source:            string(dbgen.SubscriptionSourceInternal),
		ExternalProductID: trialPlan.ProductID(),
		ExternalPriceID:   priceYearly,
	}

	ul.initFromSubscriptionPlan(sub, planService, "production")

	if ul.Property != "Product" {
		t.Errorf("Expected Property to be 'Product', got '%s'", ul.Property)
	}

	if ul.Value == "" {
		t.Error("Expected Value to be set with plan name")
	}

	// Test with monthly price if available
	if priceMonthly != "" {
		ul2 := &UserAuditLog{}
		sub2 := &db.AuditLogSubscription{
			Source:            string(dbgen.SubscriptionSourceInternal),
			ExternalProductID: trialPlan.ProductID(),
			ExternalPriceID:   priceMonthly,
		}

		ul2.initFromSubscriptionPlan(sub2, planService, "production")

		if ul2.Property != "Product" {
			t.Errorf("Expected Property to be 'Product', got '%s'", ul2.Property)
		}
	}
}

func TestInitFromOrgUserEmptyEmail(t *testing.T) {
	ul := &UserAuditLog{}

	orgUser := &db.AuditLogOrgUser{
		OrgName: "Test Org",
		UserID:  1,
		Email:   "", // Empty email
		Level:   "member",
	}

	err := ul.initFromOrgUser(nil, orgUser)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// When email is empty, Property should just say "Member"
	if ul.Property != "Member" {
		t.Errorf("Expected Property to be 'Member', got '%s'", ul.Property)
	}

	if ul.Resource != "Organization 'Test Org'" {
		t.Errorf("Expected Resource to be \"Organization 'Test Org'\", got '%s'", ul.Resource)
	}
}

func TestNewUserAuditLogForSubscriptionsTable(t *testing.T) {
	ctx := context.Background()
	planService := billing.NewPlanService(nil)

	server := &Server{
		PlanService: planService,
		Stage:       common.StageStaging,
	}

	log := &dbgen.AuditLog{
		ID:          1,
		UserID:      db.Int(1),
		Action:      dbgen.AuditLogActionCreate,
		EntityTable: db.TableNameSubscriptions,
		CreatedAt:   db.Timestampz(time.Now()),
		Source:      dbgen.AuditLogSourcePortal,
		NewValue:    mustMarshalJSON(&db.AuditLogSubscription{Source: "internal", Status: "active"}),
	}

	ul, err := server.NewUserAuditLog(ctx, log)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul == nil {
		t.Fatal("Expected non-nil UserAuditLog")
	}

	if ul.Resource != "Subscription" {
		t.Errorf("Expected Resource to be 'Subscription', got '%s'", ul.Resource)
	}
}

func TestNewUserAuditLogForOrgUsersTable(t *testing.T) {
	ctx := context.Background()
	planService := billing.NewPlanService(nil)

	server := &Server{
		PlanService: planService,
		Stage:       common.StageStaging,
	}

	log := &dbgen.AuditLog{
		ID:          1,
		UserID:      db.Int(1),
		Action:      dbgen.AuditLogActionCreate,
		EntityTable: db.TableNameOrgUsers,
		CreatedAt:   db.Timestampz(time.Now()),
		Source:      dbgen.AuditLogSourcePortal,
		NewValue:    mustMarshalJSON(&db.AuditLogOrgUser{OrgName: "Test Org", UserID: 1, Email: "test@example.com", Level: "member"}),
	}

	ul, err := server.NewUserAuditLog(ctx, log)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ul == nil {
		t.Fatal("Expected non-nil UserAuditLog")
	}

	if !strings.Contains(ul.Resource, "Organization") {
		t.Errorf("Expected Resource to contain 'Organization', got '%s'", ul.Resource)
	}
}

func TestNewUserAuditLogsArray(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create audit log events directly using PersistAuditLog
	now := time.Now().UTC()
	auditEvents := []*common.AuditLogEvent{
		{
			UserID:    user.ID,
			Action:    common.AuditLogActionCreate,
			EntityID:  int64(user.ID),
			TableName: db.TableNameProperties,
			Timestamp: now,
			Source:    common.AuditLogSourcePortal,
			NewValue: &db.AuditLogProperty{
				Name:    "test-property-1.com",
				OrgID:   org.ID,
				OrgName: org.Name,
			},
		},
		{
			UserID:    user.ID,
			Action:    common.AuditLogActionUpdate,
			EntityID:  int64(user.ID),
			TableName: db.TableNameProperties,
			Timestamp: now.Add(-1 * time.Hour),
			Source:    common.AuditLogSourcePortal,
			OldValue: &db.AuditLogProperty{
				Name: "old-name.com",
			},
			NewValue: &db.AuditLogProperty{
				Name: "new-name.com",
			},
		},
	}

	// Cast to *db.AuditLog to access PersistAuditLog
	auditLog := store.AuditLog().(*db.AuditLog)
	if err := auditLog.PersistAuditLog(ctx, auditEvents); err != nil {
		t.Fatalf("Failed to persist audit logs: %v", err)
	}

	// Retrieve audit logs
	after := time.Now().UTC().AddDate(0, 0, -14)
	logs, err := store.Impl().RetrieveUserAuditLogs(ctx, user, 100, after)
	if err != nil {
		t.Fatalf("Failed to retrieve audit logs: %v", err)
	}

	if len(logs) == 0 {
		t.Fatal("Expected non-empty audit logs array")
	}

	// Test newUserAuditLogs with NON-EMPTY array
	result := server.newUserAuditLogs(ctx, logs)

	if len(result) == 0 {
		t.Error("Expected non-empty result from newUserAuditLogs")
	}

	// Verify each log has expected fields populated
	for i, ul := range result {
		if ul.Time == "" {
			t.Errorf("Audit log %d: Expected Time to be set", i)
		}
		if ul.Action == "" {
			t.Errorf("Audit log %d: Expected Action to be set", i)
		}
	}
}

func TestCreateAuditLogsContextWithAuditLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create audit log events directly using PersistAuditLog
	now := time.Now().UTC()
	auditEvents := []*common.AuditLogEvent{
		{
			UserID:    user.ID,
			Action:    common.AuditLogActionCreate,
			EntityID:  int64(user.ID),
			TableName: db.TableNameProperties,
			Timestamp: now,
			Source:    common.AuditLogSourcePortal,
			NewValue: &db.AuditLogProperty{
				Name:    "ctx-audit-property.com",
				OrgID:   org.ID,
				OrgName: org.Name,
			},
		},
	}

	// Cast to *db.AuditLog to access PersistAuditLog
	auditLog := store.AuditLog().(*db.AuditLog)
	if err := auditLog.PersistAuditLog(ctx, auditEvents); err != nil {
		t.Fatalf("Failed to persist audit logs: %v", err)
	}

	renderCtx, err := server.CreateAuditLogsContext(ctx, user, 14, 0)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if renderCtx == nil {
		t.Fatal("Expected render context to be populated, got nil")
	}

	// Should have non-empty audit logs
	if renderCtx.Count == 0 {
		t.Error("Expected Count to be > 0 after persisting audit logs")
	}

	if len(renderCtx.AuditLogs) == 0 {
		t.Error("Expected AuditLogs to have entries")
	}
}

func TestUserAuditLogInitFromUserSettings(t *testing.T) {
	email := "test@example.com"
	tests := []struct {
		name         string
		oldValue     *db.AuditLogUserSettings
		newValue     *db.AuditLogUserSettings
		wantErr      bool
		wantResource string
		wantProperty string
	}{
		{
			name:         "nil values",
			oldValue:     nil,
			newValue:     nil,
			wantErr:      false,
			wantResource: "Notification Settings",
		},
		{
			name: "weekly report enabled",
			newValue: &db.AuditLogUserSettings{
				WeeklyReport: true,
			},
			wantErr:      false,
			wantResource: "Notification Settings",
			wantProperty: "Reports",
		},
		{
			name: "both reports enabled",
			newValue: &db.AuditLogUserSettings{
				WeeklyReport:  true,
				MonthlyReport: true,
			},
			wantErr:      false,
			wantResource: "Notification Settings",
			wantProperty: "Reports",
		},
		{
			name: "email set",
			newValue: &db.AuditLogUserSettings{
				NotificationsEmail: &email,
			},
			wantErr:      false,
			wantResource: "Notification Settings",
			wantProperty: "Email",
		},
		{
			name: "reports and email set",
			newValue: &db.AuditLogUserSettings{
				WeeklyReport:       true,
				NotificationsEmail: &email,
			},
			wantErr:      false,
			wantResource: "Notification Settings",
			wantProperty: "Reports, Email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ul := &UserAuditLog{}
			err := ul.initFromUserSettings(tt.oldValue, tt.newValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("initFromUserSettings() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ul.Resource != tt.wantResource {
				t.Errorf("initFromUserSettings() Resource = %v, want %v", ul.Resource, tt.wantResource)
			}
			if tt.wantProperty != "" && ul.Property != tt.wantProperty {
				t.Errorf("initFromUserSettings() Property = %v, want %v", ul.Property, tt.wantProperty)
			}
		})
	}
}

func TestNewUserAuditLogUserSettings(t *testing.T) {
	ctx := context.Background()
	planService := billing.NewPlanService(nil)

	server := &Server{
		PlanService: planService,
		Stage:       "production",
	}

	email := "user@example.com"
	log := &dbgen.AuditLog{
		ID:          100,
		UserID:      db.Int(1),
		Action:      dbgen.AuditLogActionUpdate,
		EntityTable: db.TableNameUserSettings,
		CreatedAt:   db.Timestampz(time.Now()),
		Source:      dbgen.AuditLogSourcePortal,
		NewValue:    mustMarshalJSON(&db.AuditLogUserSettings{WeeklyReport: true, MonthlyReport: false, NotificationsEmail: &email}),
	}

	ul, err := server.NewUserAuditLog(ctx, log)
	if err != nil {
		t.Fatalf("newUserAuditLog() error = %v", err)
	}
	if ul == nil {
		t.Fatal("newUserAuditLog() returned nil")
	}
	if ul.Resource != "Notification Settings" {
		t.Errorf("Resource = %v, want 'Notification Settings'", ul.Resource)
	}
	if ul.TableName != db.TableNameUserSettings {
		t.Errorf("TableName = %v, want %v", ul.TableName, db.TableNameUserSettings)
	}
}
