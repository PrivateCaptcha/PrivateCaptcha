package portal

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const webhookPrefixPathLimit = 12

type userForm struct {
	ID            string
	OrgID         string
	Name          string
	WebhookPrefix string
	Enabled       bool
}

type orgFormsRenderContext struct {
	portalBaseRenderContext
	PaginationRenderContext
	Forms []*userForm
}

func webhookPrefixFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Hostname() == "") {
		return rawURL
	}

	prefix := u.Hostname()
	segment := strings.TrimPrefix(u.EscapedPath(), "/")
	if segment == "" {
		return prefix
	}

	if idx := strings.Index(segment, "/"); idx >= 0 {
		segment = segment[:idx]
	}

	if len(segment) > webhookPrefixPathLimit {
		segment = segment[:webhookPrefixPathLimit]
	}

	if segment == "" {
		return prefix
	}

	return prefix + "/" + segment
}

func formToUserForm(form *dbgen.Form, hasher common.IdentifierHasher) *userForm {
	if form == nil {
		return nil
	}

	return &userForm{
		ID:            hasher.Encrypt(int(form.ID)),
		OrgID:         hasher.Encrypt(int(form.OrgID.Int32)),
		Name:          form.Name,
		WebhookPrefix: webhookPrefixFromURL(form.URL),
		Enabled:       form.Enabled,
	}
}

func formsToUserForms(ctx context.Context, forms []*dbgen.Form, hasher common.IdentifierHasher) []*userForm {
	result := make([]*userForm, 0, len(forms))

	for _, form := range forms {
		if form == nil {
			continue
		}
		if form.DeletedAt.Valid {
			slog.WarnContext(ctx, "Skipping soft-deleted form", "formID", form.ID, "orgID", form.OrgID, "deletedAt", form.DeletedAt)
			continue
		}

		result = append(result, formToUserForm(form, hasher))
	}

	return result
}
