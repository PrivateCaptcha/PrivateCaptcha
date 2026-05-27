package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/jackc/pgx/v5/pgtype"
)

type notificationCreatorStub struct {
	notifications []*common.ScheduledNotification
}

func (s *notificationCreatorStub) CreateUserNotification(ctx context.Context, n *common.ScheduledNotification) (*dbgen.UserNotification, error) {
	s.notifications = append(s.notifications, n)
	return &dbgen.UserNotification{}, nil
}

type deactivateFailingFormsQuerierStub struct {
	*db.QuerierStub
	forms          []*dbgen.Form
	deactivatedIDs []int32
	notifications  []*dbgen.CreateUserNotificationParams
}

func (s *deactivateFailingFormsQuerierStub) DeactivateForms(ctx context.Context, ids []int32) ([]*dbgen.Form, error) {
	s.deactivatedIDs = ids
	return s.forms, nil
}

func (s *deactivateFailingFormsQuerierStub) CreateUserNotification(ctx context.Context, arg *dbgen.CreateUserNotificationParams) (*dbgen.UserNotification, error) {
	s.notifications = append(s.notifications, arg)
	return &dbgen.UserNotification{}, nil
}

func TestScheduleFormDeactivationNotificationsGroupsByOwner(t *testing.T) {
	creator := &notificationCreatorStub{}
	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "test-salt"))
	tnow := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	forms := []*dbgen.Form{
		{ID: 101, Name: "Contact", OrgID: pgtype.Int4{Int32: 201, Valid: true}, OrgOwnerID: pgtype.Int4{Int32: 301, Valid: true}},
		{ID: 102, Name: "Signup", OrgID: pgtype.Int4{Int32: 202, Valid: true}, OrgOwnerID: pgtype.Int4{Int32: 301, Valid: true}},
	}

	if err := scheduleFormDeactivationNotifications(context.Background(), creator, forms, "https://portal.example", hasher, tnow); err != nil {
		t.Fatal(err)
	}

	if len(creator.notifications) != 1 {
		t.Fatalf("scheduled %d notifications, want 1", len(creator.notifications))
	}
	n := creator.notifications[0]
	if n.UserID != 301 {
		t.Fatalf("notification user ID = %d, want 301", n.UserID)
	}
	if n.TemplateHash != email.FormDeactivationTemplate.Hash() {
		t.Fatalf("notification template hash = %q, want %q", n.TemplateHash, email.FormDeactivationTemplate.Hash())
	}
	data, ok := n.Data.(*email.FormDeactivationContext)
	if !ok {
		t.Fatalf("notification data type = %T, want *email.FormDeactivationContext", n.Data)
	}
	if len(data.Forms) != 2 {
		t.Fatalf("notification form count = %d, want 2", len(data.Forms))
	}
	if data.Forms[0].Name != "Contact" || data.Forms[0].Link == "" {
		t.Fatalf("unexpected first form payload: %+v", data.Forms[0])
	}
}

func TestDeactivateFailingFormsJobRunOnce(t *testing.T) {
	ctx := context.Background()
	timeSeries := db.NewMemoryTimeSeries()
	now := time.Now().UTC().Truncate(time.Hour)
	if err := timeSeries.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		{FormID: 101, Timestamp: now.Add(-3 * time.Hour), Status: 1},
		{FormID: 101, Timestamp: now.Add(-2 * time.Hour), Status: 1},
		{FormID: 101, Timestamp: now.Add(-1 * time.Hour), Status: 1},
	}); err != nil {
		t.Fatal(err)
	}

	form := &dbgen.Form{ID: 101, Name: "Contact", OrgID: pgtype.Int4{Int32: 201, Valid: true}, OrgOwnerID: pgtype.Int4{Int32: 301, Valid: true}}
	querier := &deactivateFailingFormsQuerierStub{
		QuerierStub: &db.QuerierStub{},
		forms:       []*dbgen.Form{form},
	}
	store := db.NewBusinessWithQuerier(nil, querier, db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{}))
	job := &DeactivateFailingFormsJob{
		Store:      store,
		TimeSeries: timeSeries,
		PortalURL:  "https://portal.example",
		IDHasher:   common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "test-salt")),
		Threshold:  3,
		MaxForms:   50,
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	if len(querier.deactivatedIDs) != 1 || querier.deactivatedIDs[0] != 101 {
		t.Fatalf("deactivated IDs = %v, want [101]", querier.deactivatedIDs)
	}
	if len(querier.notifications) != 1 {
		t.Fatalf("scheduled notifications = %d, want 1", len(querier.notifications))
	}
}
