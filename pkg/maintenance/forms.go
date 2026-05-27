package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

const formDeactivationReferencePrefix = "forms/deactivated/"

type userNotificationCreator interface {
	CreateUserNotification(context.Context, *common.ScheduledNotification) (*dbgen.UserNotification, error)
}

func formDeactivationReference(userID int32, t time.Time) string {
	return fmt.Sprintf("%s%d/%s", formDeactivationReferencePrefix, userID, t.UTC().Format(time.DateOnly))
}

func formDashboardURL(portalURL string, hasher common.IdentifierHasher, form *dbgen.Form) string {
	base := strings.TrimRight(portalURL, "/")
	return fmt.Sprintf("%s/%s/%s/%s/%s", base, common.OrgEndpoint, hasher.Encrypt(int(form.OrgID.Int32)), common.FormEndpoint, hasher.Encrypt(int(form.ID)))
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
			Link: formDashboardURL(portalURL, hasher, form),
		})
	}

	persistUntil := tnow.AddDate(0, 1, 0)
	var errs []error
	for ownerID, group := range groups {
		if len(group.forms) == 0 {
			continue
		}

		notif := &common.ScheduledNotification{
			ReferenceID:  formDeactivationReference(ownerID, tnow),
			UserID:       ownerID,
			Subject:      fmt.Sprintf("[%s] Forms were deactivated", common.PrivateCaptcha),
			Data:         &email.FormDeactivationContext{Forms: group.forms},
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
