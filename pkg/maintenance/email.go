package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	htmltpl "html/template"
	"log/slog"
	texttpl "text/template"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/jpillora/backoff"
)

type RegisterEmailTemplatesJob struct {
	Templates []*common.EmailTemplate
	Store     db.Implementor
}

var _ common.OneOffJob = (*RegisterEmailTemplatesJob)(nil)

func (j *RegisterEmailTemplatesJob) Name() string {
	return "register_email_templates_job"
}
func (j *RegisterEmailTemplatesJob) InitialPause() time.Duration {
	return 20 * time.Second
}
func (j *RegisterEmailTemplatesJob) NewParams() any {
	return struct{}{}
}

func (j *RegisterEmailTemplatesJob) RunOnce(ctx context.Context, params any) error {
	var anyError error

	for _, tpl := range j.Templates {
		if _, err := j.Store.Impl().CreateNotificationTemplate(ctx, tpl.Name(), tpl.ContentHTML(), tpl.ContentText(), tpl.Hash()); err != nil {
			slog.ErrorContext(ctx, "Failed to upsert notification template", "name", tpl.Name(), common.ErrAttr(err))
			anyError = err
		}
	}

	return anyError
}

type UserEmailNotificationsJob struct {
	// this is the "actual" interval since we will be running as a DB-locked distributed job
	RunInterval  time.Duration
	Store        db.Implementor
	Templates    []*common.EmailTemplate
	Sender       email.Sender
	ChunkSize    int
	MaxAttempts  int
	EmailFrom    common.ConfigItem
	ReplyToEmail common.ConfigItem
	PlanService  billing.PlanService
	CDNURL       string
	PortalURL    string
	UserIDs      map[int32]struct{}
}

var _ common.PeriodicJob = (*UserEmailNotificationsJob)(nil)

func (j *UserEmailNotificationsJob) Interval() time.Duration {
	return 20 * time.Minute
}

func (j *UserEmailNotificationsJob) Jitter() time.Duration {
	return 10 * time.Minute
}

func (j *UserEmailNotificationsJob) Name() string {
	return "user_email_notifications_job"
}

func groupNotificationsByTemplate(ctx context.Context, notifications []*dbgen.GetPendingUserNotificationsRow) map[string][]*dbgen.GetPendingUserNotificationsRow {
	result := make(map[string][]*dbgen.GetPendingUserNotificationsRow)

	for _, n := range notifications {
		un := &n.UserNotification
		if !un.TemplateID.Valid {
			slog.ErrorContext(ctx, "Skipping notification template with orphanned hash", "nid", un.ID)
			continue
		}

		if list, ok := result[un.TemplateID.String]; ok {
			result[un.TemplateID.String] = append(list, n)
		} else {
			result[un.TemplateID.String] = []*dbgen.GetPendingUserNotificationsRow{n}
		}
	}

	return result
}

func indexTemplates(ctx context.Context, templates []*common.EmailTemplate) map[string]*common.EmailTemplate {
	tplMap := make(map[string]*common.EmailTemplate)
	for _, tpl := range templates {
		hash := tpl.Hash()
		if _, ok := tplMap[hash]; ok {
			slog.ErrorContext(ctx, "Found two templates with the same hash", "hash", hash, "name", tpl.Name())
			continue
		}
		tplMap[hash] = tpl
	}
	return tplMap
}

type preparedNotificationTemplate struct {
	htmlTemplate *htmltpl.Template
	textTemplate *texttpl.Template
	name         string
}

func (j *UserEmailNotificationsJob) retrieveTemplate(ctx context.Context,
	templates map[string]*common.EmailTemplate,
	templateHash string) (*preparedNotificationTemplate, error) {
	hlog := slog.With("hash", templateHash)
	var contentHTML string
	var contentText string
	var name string
	itpl, ok := templates[templateHash]
	if ok {
		contentHTML, contentText = itpl.ContentHTML(), itpl.ContentText()
		name = itpl.Name()
	} else {
		hlog.WarnContext(ctx, "Template is not found locally")
		if dbTemplate, err := j.Store.Impl().RetrieveNotificationTemplate(ctx, templateHash); err == nil {
			contentHTML, contentText = dbTemplate.ContentHtml, dbTemplate.ContentText
			name = dbTemplate.Name
		} else {
			hlog.ErrorContext(ctx, "Failed to retrieve template from DB", common.ErrAttr(err))
			return nil, err
		}
	}

	nt := &preparedNotificationTemplate{name: name}

	if len(contentHTML) > 0 {
		if tplHTML, err := htmltpl.New("NotificationHTML").Parse(contentHTML); err != nil {
			hlog.ErrorContext(ctx, "Failed to parse HTML template", "name", name, common.ErrAttr(err))
			return nil, err
		} else {
			nt.htmlTemplate = tplHTML
		}
	}

	if len(contentText) > 0 {
		if tplText, err := texttpl.New("NotificationText").Parse(contentText); err != nil {
			hlog.ErrorContext(ctx, "Failed to parse text template", "name", name, common.ErrAttr(err))
			return nil, err
		} else {
			nt.textTemplate = tplText
		}
	}

	hlog.InfoContext(ctx, "Parsed templates", "name", name)

	return nt, nil
}

func (j *UserEmailNotificationsJob) retrievePendingNotifications(ctx context.Context, params *UserEmailNotificationsParams) ([]*dbgen.GetPendingUserNotificationsRow, error) {
	// just for safety, we fetch overlapping segments, but it will be filtered out on the way
	since := time.Now().UTC().Add(-(params.RunInterval + j.Interval() + j.Jitter()))
	notifications, err := j.Store.Impl().RetrievePendingUserNotifications(ctx, since, params.ChunkSize, params.MaxAttempts)
	if err != nil {
		return nil, err
	}

	if len(notifications) == 0 {
		slog.DebugContext(ctx, "No pending notifications", "since", since)
		return notifications, nil
	}

	if len(j.UserIDs) == 0 {
		return notifications, nil
	}

	filtered := make([]*dbgen.GetPendingUserNotificationsRow, 0, len(notifications))
	for _, n := range notifications {
		shouldAdd := false
		if n.UserNotification.UserID.Valid {
			if _, ok := j.UserIDs[n.UserNotification.UserID.Int32]; ok {
				shouldAdd = true
			}
		}

		if shouldAdd {
			filtered = append(filtered, n)
		}
	}

	return filtered, nil
}

type UserEmailNotificationsParams struct {
	RunInterval time.Duration `json:"run_interval"`
	ChunkSize   int           `json:"chunk_size"`
	MaxAttempts int           `json:"max_attempts"`
}

func (j *UserEmailNotificationsJob) NewParams() any {
	return &UserEmailNotificationsParams{
		RunInterval: j.RunInterval,
		ChunkSize:   j.ChunkSize,
		MaxAttempts: j.MaxAttempts,
	}
}

// NOTE: we should NOT refactor this into "while we have pending notifications {}" loop because some notifications
// are unprocessable by design (e.g. "subscribed-only" notifications for users who don't have subscriptions), therefore
// there are cases when there will always be "pending" notifications.
// If we are not managing to process all of them, we need to modify ChunkSize and Interval (or Lock Interval) instead
func (j *UserEmailNotificationsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*UserEmailNotificationsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*UserEmailNotificationsParams)
	}

	// TODO: Monitor pending notifications count in Postgres
	// so we will know if we have enough processing capacity
	notifications, err := j.retrievePendingNotifications(ctx, p)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve pending user notifications", common.ErrAttr(err))
		return err
	}

	if len(notifications) == 0 {
		return nil
	}

	templates := indexTemplates(ctx, j.Templates)

	b := &backoff.Backoff{
		Min:    50 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	groups := groupNotificationsByTemplate(ctx, notifications)
	for tplHash, nn := range groups {
		if len(nn) == 0 {
			slog.WarnContext(ctx, "Skipping empty notifications for template", "hash", tplHash)
			continue
		}

		if tpl, err := j.retrieveTemplate(ctx, templates, tplHash); err == nil {
			processedIDs := j.processNotificationsChunk(ctx, tpl, nn, b)
			// NOTE: potentially it's not most efficient to update them piece by piece, but it's less error-prone
			j.updateNotifications(ctx, nn, processedIDs)
		} else {
			slog.ErrorContext(ctx, "Failed to get notifications template", common.ErrAttr(err))
		}
	}

	return nil
}

func (j *UserEmailNotificationsJob) updateNotifications(ctx context.Context,
	notifications []*dbgen.GetPendingUserNotificationsRow,
	processedIDs []int32) {
	if err := j.Store.Impl().MarkUserNotificationsProcessed(ctx, processedIDs, time.Now().UTC()); err != nil {
		slog.ErrorContext(ctx, "Failed to mark notifications processed", common.ErrAttr(err))
	}

	processedNotifications := make(map[int32]struct{})
	t := struct{}{}
	for _, id := range processedIDs {
		processedNotifications[id] = t
	}

	attemptedNotificationIDs := make([]int32, 0, len(notifications)-len(processedNotifications)+1)
	for _, n := range notifications {
		if _, ok := processedNotifications[n.UserNotification.ID]; !ok {
			attemptedNotificationIDs = append(attemptedNotificationIDs, n.UserNotification.ID)
		}
	}

	if err := j.Store.Impl().MarkUserNotificationsAttempted(ctx, attemptedNotificationIDs); err != nil {
		slog.ErrorContext(ctx, "Failed to update failed notifications", common.ErrAttr(err))
	}
}

func isValidUserNotification(un *dbgen.UserNotification) bool {
	return len(un.Subject) > 0 &&
		len(un.ReferenceID) > 0 &&
		un.ScheduledAt.Valid &&
		un.TemplateID.Valid
}

func (j *UserEmailNotificationsJob) processNotificationsChunk(ctx context.Context,
	tpl *preparedNotificationTemplate,
	notifications []*dbgen.GetPendingUserNotificationsRow,
	b *backoff.Backoff) []int32 {
	emailFrom := j.EmailFrom.Value()
	replyToEmail := j.ReplyToEmail.Value()
	processedNotificationIDs := make([]int32, 0, len(notifications))
	lastSentCount := 0

	for _, n := range notifications {
		if currSentCount := len(processedNotificationIDs); currSentCount > lastSentCount {
			// backoff a little not to overwhelm transactional email provider
			time.Sleep(b.Duration())
			lastSentCount = currSentCount
		}

		un := &n.UserNotification
		nlog := slog.With("notifID", un.ID, "template_id", un.TemplateID.String, "template_name", tpl.name)
		nlog.Log(ctx, common.LevelTrace, "Processing notification", "attempts", un.ProcessingAttempts, "subject", un.Subject, "userID", un.UserID.Int32)

		if !isValidUserNotification(un) {
			nlog.WarnContext(ctx, "Skipping invalid user notification")
			continue
		}

		if un.RequiresSubscription.Valid {
			// NOTE: checking this logic in code (instead of SQL) means that we might attempt to process same notifications
			// again and again so we rely on "processing_attempts" circuit breaker logic
			if isActive := n.Status.Valid && j.PlanService.IsSubscriptionActive(n.Status.String); isActive != un.RequiresSubscription.Bool {
				nlog.WarnContext(ctx, "Skipping user notification without matching subscription status", "userID", un.UserID.Int32, "expected", un.RequiresSubscription.Bool, "actual", isActive, "subscID", n.SubscriptionID.Int32)
				continue
			}
		}

		var data map[string]interface{}
		if err := json.Unmarshal(un.Payload, &data); err != nil {
			nlog.ErrorContext(ctx, "Failed to parse notification context", common.ErrAttr(err))
			continue
		}
		// "common" context
		data["CDNURL"] = j.CDNURL
		data["PortalURL"] = j.PortalURL
		data["CurrentYear"] = time.Now().Year()

		var htmlBodyTpl bytes.Buffer
		if tpl.htmlTemplate != nil {
			if err := tpl.htmlTemplate.Execute(&htmlBodyTpl, data); err != nil {
				nlog.ErrorContext(ctx, "Failed to execute HTML template with notification context", common.ErrAttr(err))
				continue
			}
		}

		var textBodyTpl bytes.Buffer
		if tpl.textTemplate != nil {
			if err := tpl.textTemplate.Execute(&textBodyTpl, data); err != nil {
				nlog.ErrorContext(ctx, "Failed to execute Text template with notification context", common.ErrAttr(err))
				continue
			}
		}

		msg := &email.Message{
			Subject:   un.Subject,
			EmailTo:   n.Email,
			EmailFrom: emailFrom,
			NameFrom:  common.PrivateCaptchaTeam,
			ReplyTo:   replyToEmail,
			HTMLBody:  htmlBodyTpl.String(),
			TextBody:  textBodyTpl.String(),
		}

		if err := j.Sender.SendEmail(ctx, msg); err != nil {
			nlog.ErrorContext(ctx, "Failed to send notification email", common.ErrAttr(err))
			continue
		}

		nlog.DebugContext(ctx, "Processed user notification")

		processedNotificationIDs = append(processedNotificationIDs, un.ID)
	}

	return processedNotificationIDs
}

type CleanupUserNotificationsJob struct {
	Store              db.Implementor
	NotificationMonths int
	TemplateMonths     int
}

var _ common.PeriodicJob = (*CleanupUserNotificationsJob)(nil)

type CleanupUserNotificationsParams struct {
	NotificationMonths int `json:"notification_months"`
	TemplateMonths     int `json:"template_months"`
}

func (j *CleanupUserNotificationsJob) NewParams() any {
	return &CleanupUserNotificationsParams{
		NotificationMonths: j.NotificationMonths,
		TemplateMonths:     j.TemplateMonths,
	}
}

func (j *CleanupUserNotificationsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*CleanupUserNotificationsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*CleanupUserNotificationsParams)
	}

	var anyError error

	tnow := time.Now().UTC()

	if err := j.Store.Impl().DeleteSentUserNotifications(ctx, tnow.AddDate(0, -p.NotificationMonths, 0)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete sent user notifications", common.ErrAttr(err))
		anyError = err
	}

	if err := j.Store.Impl().DeleteUnsentUserNotifications(ctx, tnow.AddDate(0, -p.NotificationMonths, 0)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete UNsent user notifications", common.ErrAttr(err))
		anyError = err
	}

	// we delete notification templates only if there're no dangling references in
	// user_notifications table so the date should be smaller than the other two
	templateUpdatedBefore := tnow.AddDate(0, -p.TemplateMonths, 0)
	notifDeliveredBefore := tnow.AddDate(0, -p.TemplateMonths, 0)
	if err := j.Store.Impl().DeleteUnusedNotificationTemplates(ctx, notifDeliveredBefore, templateUpdatedBefore); err != nil {
		slog.ErrorContext(ctx, "Failed to delete unused notification templates", common.ErrAttr(err))
		anyError = err
	}

	return anyError
}

func (j *CleanupUserNotificationsJob) Interval() time.Duration {
	return 3 * time.Hour
}

func (j *CleanupUserNotificationsJob) Jitter() time.Duration {
	return 1 * time.Hour
}
func (j *CleanupUserNotificationsJob) Name() string {
	return "cleanup_user_notifications_job"
}
