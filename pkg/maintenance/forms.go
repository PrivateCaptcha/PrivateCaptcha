package maintenance

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/url"
	"slices"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

const (
	formDeactivationReferencePrefix = "forms/deactivated/"
	formEmailUTM                    = "utm_medium=email&utm_source=form"
)

const (
	defaultFailingFormsThreshold = 5
	defaultFailingFormsMaxForms  = 50
)

type DeactivateFailingFormsJob struct {
	Store      db.Implementor
	TimeSeries common.TimeSeriesStore
	PortalURL  string
	IDHasher   common.IdentifierHasher
	Threshold  int
	MaxForms   int
}

type DeactivateFailingFormsParams struct {
	Threshold int `json:"threshold"`
	MaxForms  int `json:"max_forms"`
}

var _ common.PeriodicJob = (*DeactivateFailingFormsJob)(nil)

type userNotificationCreator interface {
	CreateUserNotification(context.Context, *common.ScheduledNotification) (*dbgen.UserNotification, error)
}

func (j *DeactivateFailingFormsJob) Name() string {
	return "deactivate_failing_forms_job"
}

func (j *DeactivateFailingFormsJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *DeactivateFailingFormsJob) Timeout() time.Duration {
	return 5 * time.Minute
}

func (j *DeactivateFailingFormsJob) Jitter() time.Duration {
	return 10 * time.Minute
}

func (j *DeactivateFailingFormsJob) Trigger() <-chan struct{} {
	return nil
}

func (j *DeactivateFailingFormsJob) NewParams() any {
	threshold := j.Threshold
	if threshold <= 0 {
		threshold = defaultFailingFormsThreshold
	}

	maxForms := j.MaxForms
	if maxForms <= 0 {
		maxForms = defaultFailingFormsMaxForms
	}

	return &DeactivateFailingFormsParams{Threshold: threshold, MaxForms: maxForms}
}

func (j *DeactivateFailingFormsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*DeactivateFailingFormsParams)
	if !ok || p == nil {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*DeactivateFailingFormsParams)
	}
	if p.Threshold <= 0 || p.MaxForms <= 0 {
		slog.ErrorContext(ctx, "Job parameters are invalid", "threshold", p.Threshold, "maxForms", p.MaxForms, "job", j.Name())
		p = j.NewParams().(*DeactivateFailingFormsParams)
	}

	if j.Store == nil || j.TimeSeries == nil || j.IDHasher == nil {
		return db.ErrInvalidInput
	}

	candidates, err := j.TimeSeries.RetrieveFailingForms(ctx, p.Threshold, p.MaxForms)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve failing forms", common.ErrAttr(err))
		return err
	}
	if len(candidates) == 0 {
		slog.DebugContext(ctx, "No failing forms found")
		return nil
	}

	formIDs := make([]int32, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		formIDs = append(formIDs, candidate.FormID)
	}
	if len(formIDs) == 0 {
		return nil
	}

	forms, err := j.Store.Impl().DeactivateForms(ctx, formIDs)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to deactivate failing forms", "count", len(formIDs), common.ErrAttr(err))
		return err
	}
	if len(forms) == 0 {
		slog.DebugContext(ctx, "No active failing forms were deactivated", "candidates", len(formIDs))
		return nil
	}

	return scheduleFormDeactivationNotifications(ctx, j.Store.Impl(), forms, j.PortalURL, j.IDHasher, time.Now().UTC())
}

func hashIDs(ids []int32) uint32 {
	slices.Sort(ids)

	h := fnv.New32a()

	var buf [4]byte
	for _, id := range ids {
		binary.LittleEndian.PutUint32(buf[:], uint32(id))
		h.Write(buf[:])
	}

	return h.Sum32()
}

func formDashboardURL(ctx context.Context, portalURL string, hasher common.IdentifierHasher, form *dbgen.Form, utm string) string {
	if (len(portalURL) == 0) || (hasher == nil) || (form == nil) || (!form.OrgID.Valid) {
		return ""
	}

	link, err := url.JoinPath(portalURL,
		common.OrgEndpoint,
		hasher.Encrypt(int(form.OrgID.Int32)),
		common.FormEndpoint,
		hasher.Encrypt(int(form.ID)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to build form dashboard URL", "formID", form.ID, common.ErrAttr(err))
		return ""
	}

	if len(utm) > 0 {
		return link + "?" + utm
	}
	return link
}

func scheduleFormDeactivationNotifications(ctx context.Context, creator userNotificationCreator, forms []*dbgen.Form, portalURL string, hasher common.IdentifierHasher, tnow time.Time) error {
	if len(forms) == 0 {
		return nil
	}
	if creator == nil || hasher == nil || tnow.IsZero() {
		return db.ErrInvalidInput
	}

	type ownerForms struct {
		forms []*email.DeactivatedForm
		ids   []int32
	}

	groups := make(map[int32]*ownerForms)
	for _, form := range forms {
		if form == nil || !form.OrgOwnerID.Valid || !form.OrgID.Valid {
			continue
		}
		ownerID := form.OrgOwnerID.Int32
		if groups[ownerID] == nil {
			groups[ownerID] = &ownerForms{}
		}
		groups[ownerID].forms = append(groups[ownerID].forms, &email.DeactivatedForm{
			Name: form.Name,
			Link: formDashboardURL(ctx, portalURL, hasher, form, formEmailUTM),
		})
		groups[ownerID].ids = append(groups[ownerID].ids, form.ID)
	}

	persistUntil := tnow.AddDate(0, 1, 0)
	var errs []error
	for ownerID, group := range groups {
		if len(group.forms) == 0 {
			continue
		}

		idHash := hashIDs(group.ids)

		referenceID := fmt.Sprintf("%s%d/%d/%s", formDeactivationReferencePrefix, ownerID, idHash, tnow.UTC().Format(time.DateOnly))

		notif := &common.ScheduledNotification{
			ReferenceID:  referenceID,
			UserID:       ownerID,
			Subject:      fmt.Sprintf("[%s] Forms were deactivated", common.PrivateCaptcha),
			Data:         &email.FormDeactivationContext{Forms: group.forms, UTM: formEmailUTM},
			DateTime:     tnow,
			TemplateHash: email.FormDeactivationTemplate.Hash(),
			PersistUntil: &persistUntil,
			Condition:    common.NotificationWithSubscription,
		}
		if _, err := creator.CreateUserNotification(ctx, notif); err != nil {
			slog.WarnContext(ctx, "Failed to create form deactivation notification", "userID", ownerID, common.ErrAttr(err))
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
