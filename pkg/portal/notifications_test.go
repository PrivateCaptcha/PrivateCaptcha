package portal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

var (
	errFromFailingSender = errors.New("sender error")
)

type failingSender struct{}

var _ email.Sender = (*failingSender)(nil)

func (sm *failingSender) SendEmail(ctx context.Context, msg *email.Message) error {
	return errFromFailingSender
}

func TestDifferentReferenceIDs(t *testing.T) {
	const keyID = 123
	if apiKeyExpiredReference(keyID) == apiKeyExpirationReference(keyID) {
		t.Fatal("references should be different")
	}
}

func TestUserNotificationsJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	tnow := time.Now().UTC()

	const referenceID = "referenceID"

	n := &common.ScheduledNotification{
		UserID:       user.ID,
		ReferenceID:  referenceID,
		TemplateHash: email.TwoFactorEmailTemplate.Hash(),
		Subject:      "subject",
		Data:         map[string]int{},
		DateTime:     tnow.Add(-10 * time.Minute),
		Persistent:   false,
		Condition:    common.NotificationWithSubscription,
	}
	if _, err := store.Impl().CreateUserNotification(ctx, n); err != nil {
		t.Fatal(err)
	}

	sender := &email.StubSender{}

	job := &maintenance.UserEmailNotificationsJob{
		RunInterval:  1 * time.Hour,
		Store:        store,
		Templates:    email.Templates(),
		Sender:       sender,
		ChunkSize:    100,
		MaxAttempts:  5,
		PlanService:  server.PlanService,
		EmailFrom:    config.NewStaticValue(common.EmailFromKey, "foo@bar.com"),
		ReplyToEmail: config.NewStaticValue(common.ReplyToEmailKey, "foo@bar.com"),
		UserIDs:      map[int32]struct{}{user.ID: struct{}{}},
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	if sender.Count != 1 {
		t.Errorf("Unexpected number of sent emails: %v", sender.Count)
	}

	// run again, but the notification should be processed by now
	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	if sender.Count != 1 {
		t.Errorf("Unexpected number of sent emails: %v", sender.Count)
	}
}

func TestDeleteSentNotifications(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	tnow := time.Now().UTC()

	sn := &common.ScheduledNotification{
		ReferenceID:  "referenceID",
		UserID:       user.ID,
		Subject:      "subject",
		Data:         map[string]int{},
		DateTime:     tnow.Add(-10 * time.Minute),
		TemplateHash: email.TwoFactorEmailTemplate.Hash(),
	}

	notif, err := store.Impl().CreateUserNotification(ctx, sn)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Impl().CreateUserNotification(ctx, sn); err == nil {
		t.Fatal("Shouldn't create a notification with the same referenceID")
	}

	if err := store.Impl().MarkUserNotificationsProcessed(ctx, []int32{notif.ID}, tnow.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := store.Impl().DeleteSentUserNotifications(ctx, tnow.Add(-1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// should be able to create again (unlike before)
	if _, err := store.Impl().CreateUserNotification(ctx, sn); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteScheduledNotification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	tnow := time.Now().UTC()

	sn := &common.ScheduledNotification{
		ReferenceID:  "referenceID",
		UserID:       user.ID,
		Subject:      "subject",
		Data:         map[string]int{},
		DateTime:     tnow.Add(-10 * time.Minute),
		TemplateHash: email.TwoFactorEmailTemplate.Hash(),
	}

	if _, err := store.Impl().CreateUserNotification(ctx, sn); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Impl().CreateUserNotification(ctx, sn); err == nil {
		t.Fatal("Shouldn't create a notification with the same referenceID")
	}

	if err := store.Impl().DeletePendingUserNotification(ctx, user, sn.ReferenceID); err != nil {
		t.Fatal(err)
	}

	// should be able to create again (unlike before)
	if _, err := store.Impl().CreateUserNotification(ctx, sn); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationMaxAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	tnow := time.Now().UTC()

	sn := &common.ScheduledNotification{
		ReferenceID:  "referenceID",
		UserID:       user.ID,
		Subject:      "subject",
		Data:         map[string]int{},
		DateTime:     tnow.Add(-10 * time.Minute),
		TemplateHash: email.TwoFactorEmailTemplate.Hash(),
	}

	notif, err := store.Impl().CreateUserNotification(ctx, sn)
	if err != nil {
		t.Fatal(err)
	}

	const times = 4

	for _ = range times {
		if err := store.Impl().MarkUserNotificationsAttempted(ctx, []int32{notif.ID}); err != nil {
			t.Fatal(err)
		}
	}

	sender := &email.StubSender{}

	job := &maintenance.UserEmailNotificationsJob{
		RunInterval:  1 * time.Hour,
		Store:        store,
		Templates:    email.Templates(),
		Sender:       sender,
		ChunkSize:    100,
		MaxAttempts:  times,
		PlanService:  server.PlanService,
		EmailFrom:    config.NewStaticValue(common.EmailFromKey, "foo@bar.com"),
		ReplyToEmail: config.NewStaticValue(common.ReplyToEmailKey, "foo@bar.com"),
		UserIDs:      map[int32]struct{}{user.ID: struct{}{}},
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	// notification should have been skipped
	if sender.Count != 0 {
		t.Errorf("Unexpected number of sent emails: %v", sender.Count)
	}
}

// difference from TestNotificationMaxAttempts is that we fail during processing instead of "externally"
func TestNotificationProcessingAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	tnow := time.Now().UTC()

	sn := &common.ScheduledNotification{
		ReferenceID:  "referenceID",
		UserID:       user.ID,
		Subject:      "subject",
		Data:         map[string]int{},
		DateTime:     tnow.Add(-10 * time.Minute),
		TemplateHash: email.TwoFactorEmailTemplate.Hash(),
	}

	if _, err := store.Impl().CreateUserNotification(ctx, sn); err != nil {
		t.Fatal(err)
	}

	job := &maintenance.UserEmailNotificationsJob{
		RunInterval:  1 * time.Hour,
		Store:        store,
		Templates:    email.Templates(),
		Sender:       &failingSender{}, // <-- the most important part in this test
		ChunkSize:    100,
		MaxAttempts:  100,
		PlanService:  server.PlanService,
		EmailFrom:    config.NewStaticValue(common.EmailFromKey, "foo@bar.com"),
		ReplyToEmail: config.NewStaticValue(common.ReplyToEmailKey, "foo@bar.com"),
		UserIDs:      map[int32]struct{}{user.ID: struct{}{}},
	}

	const times = 4

	for _ = range times {
		if err := job.RunOnce(ctx, job.NewParams()); err != nil {
			t.Fatal(err)
		}
	}

	// now it should succeed, but we run out of attempts
	sender := &email.StubSender{}
	job.Sender = sender
	job.MaxAttempts = times

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	// notification should have been skipped
	if sender.Count != 0 {
		t.Errorf("Unexpected number of sent emails: %v", sender.Count)
	}
}

func TestRequireSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	// this has to reflect what we actually use instead of db_helpers where we fill external IDs too
	subscrParams := createInternalTrial(testPlan, server.PlanService.ActiveTrialStatus())
	user, _, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), subscrParams)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	tnow := time.Now().UTC()

	sn := &common.ScheduledNotification{
		ReferenceID:  "referenceID",
		UserID:       user.ID,
		Subject:      "subject",
		Data:         map[string]int{},
		DateTime:     tnow.Add(-10 * time.Minute),
		TemplateHash: email.TwoFactorEmailTemplate.Hash(),
		Condition:    common.NotificationWithoutSubscription,
	}

	if _, err := store.Impl().CreateUserNotification(ctx, sn); err != nil {
		t.Fatal(err)
	}

	sender := &email.StubSender{}

	job := &maintenance.UserEmailNotificationsJob{
		RunInterval:  1 * time.Hour,
		Store:        store,
		Templates:    email.Templates(),
		Sender:       sender,
		ChunkSize:    100,
		MaxAttempts:  100,
		PlanService:  server.PlanService,
		EmailFrom:    config.NewStaticValue(common.EmailFromKey, "foo@bar.com"),
		ReplyToEmail: config.NewStaticValue(common.ReplyToEmailKey, "foo@bar.com"),
		UserIDs:      map[int32]struct{}{user.ID: struct{}{}},
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	// notification should have been skipped based on condition (without subscription)
	if sender.Count != 0 {
		t.Errorf("Unexpected number of sent emails: %v", sender.Count)
	}

	subscr, err := store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Impl().ExpireInternalTrials(ctx,
		subscr.TrialEndsAt.Time.Add(-1*time.Minute),
		subscr.TrialEndsAt.Time.Add(+1*time.Minute),
		server.PlanService.ActiveTrialStatus(),
		server.PlanService.ExpiredTrialStatus()); err != nil {
		t.Fatal(err)
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	// now it should have been processed because user should not have a subscription anymore
	if sender.Count != 1 {
		t.Errorf("Unexpected number of sent emails: %v", sender.Count)
	}
}
