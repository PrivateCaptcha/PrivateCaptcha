package portal

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	webhookPrefixPathLimit = 12
	formWizardTemplate     = "form-wizard/wizard.html"
)

type formWizardRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	Name        string
	Domain      string
	URL         string
	NameError   string
	DomainError string
	URLError    string
	CurrentOrg  *UserOrg
	Step        int
}

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

func (s *Server) getNewOrgForm(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	data := &formWizardRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg: &UserOrg{
			Name:  org.Name,
			ID:    s.IDHasher.Encrypt(int(org.ID)),
			Level: "",
		},
	}

	if isUserOrgOwner := org.UserID.Int32 == user.ID; isUserOrgOwner && !user.SubscriptionID.Valid {
		data.ErrorMessage = activeSubscriptionForPropertyError
	}

	return &ViewModel{Model: data, View: formWizardTemplate}, nil
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
