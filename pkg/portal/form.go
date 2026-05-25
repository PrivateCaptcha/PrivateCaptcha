package portal

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	webhookPrefixPathLimit         = 12
	formWizardTemplate             = "form-wizard/wizard.html"
	formWizardNewTemplate          = "form-wizard/new.html"
	formWizardSetupTemplate        = "form-wizard/client-setup.html"
	activeSubscriptionForFormError = "You need an active subscription to create new forms."
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
	ExternalID    string
	Enabled       bool
}

type orgFormsRenderContext struct {
	portalBaseRenderContext
	PaginationRenderContext
	Forms []*userForm
}

type formIntegrationRenderContext struct {
	CsrfRenderContext
	CurrentOrg *UserOrg
	Form       *userForm
	Sitekey    string
}

func (s *Server) validateFormsLimit(ctx context.Context, org *dbgen.Organization, sessUser *dbgen.User) string {
	owner, subscr, err := s.Store.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, sessUser)
	if err != nil {
		return ""
	}

	isOrgOwner := org.UserID.Int32 == sessUser.ID

	ok, extra, err := s.SubscriptionLimits.CheckFormsLimit(ctx, org.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			if isOrgOwner {
				return activeSubscriptionForFormError
			}

			return "Organization owner needs an active subscription to create new forms."
		}
		return ""
	}

	if !ok {
		slog.WarnContext(ctx, "Forms limit check failed", "extra", extra, "userID", owner.ID, "subscriptionID", subscr.ID,
			"orgOwner", isOrgOwner, "internal", db.IsInternalSubscription(subscr.Source))

		if isOrgOwner {
			return "Forms limit reached for current subscription plan."
		}

		return "Forms limit reached for this organization's owner, contact them to upgrade."
	}

	return ""
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
		data.ErrorMessage = activeSubscriptionForFormError
	}

	return &ViewModel{Model: data, View: formWizardTemplate}, nil
}

func (s *Server) postNewOrgForm(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	if err = r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, db.ErrInvalidInput
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	// Invited users cannot create properties - must join the org first
	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User is only invited, not a member of this org", "orgID", org.ID, "userID", user.ID)
		return nil, db.ErrPermissions
	}

	renderCtx := &formWizardRenderContext{
		CsrfRenderContext:  s.CreateCsrfContext(user),
		AlertRenderContext: AlertRenderContext{},
		CurrentOrg:         orgToUserOrg(org, user.ID, s.IDHasher),
	}

	renderCtx.Name = strings.TrimSpace(r.FormValue(common.ParamName))
	if nameStatus := s.Store.Impl().ValidateFormName(ctx, renderCtx.Name, org); !nameStatus.Success() {
		renderCtx.NameError = nameStatus.String()
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	renderCtx.Domain = strings.TrimSpace(r.FormValue(common.ParamDomain))
	domain, err := common.ParseDomainName(renderCtx.Domain)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse domain name", "domain", renderCtx.Domain, common.ErrAttr(err))
		renderCtx.DomainError = common.StatusPropertyDomainFormatError.String()
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	_, ignoreError := r.Form[common.ParamIgnoreError]
	if domainStatus := s.validateDomainName(ctx, domain, ignoreError); !domainStatus.Success() {
		renderCtx.DomainError = domainStatus.String()
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	renderCtx.URL = strings.TrimSpace(r.FormValue(common.ParamURL))
	if len(renderCtx.URL) == 0 {
		renderCtx.URLError = "URL cannot be empty."
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	if err := s.FormURLVerifier.VerifyURL(ctx, renderCtx.URL); err != nil {
		slog.WarnContext(ctx, "Failed to verify form URL", "url", renderCtx.URL, common.ErrAttr(err))
		renderCtx.URLError = "URL is not valid."
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	if limitError := s.validatePropertiesLimit(ctx, org, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	if limitError := s.validateFormsLimit(ctx, org, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	propertyParams := db.NewDefaultPropertyParams("" /*name*/, domain, user.ID)
	form, property, auditEvents, err := s.Store.Impl().CreateNewForm(ctx, propertyParams, &dbgen.CreateFormParams{
		Name:              renderCtx.Name,
		URL:               renderCtx.URL,
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create the form", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to create the form. Please try again later."
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate}, nil
	}

	return &ViewModel{
		Model: &formIntegrationRenderContext{
			CsrfRenderContext: s.CreateCsrfContext(user),
			CurrentOrg:        orgToUserOrg(org, user.ID, s.IDHasher),
			Form:              formToUserForm(form, s.IDHasher),
			Sitekey:           db.UUIDToSiteKey(property.ExternalID),
		},
		View:        formWizardSetupTemplate,
		AuditEvents: auditEvents,
	}, nil
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
		ExternalID:    db.UUIDToString(form.ExternalID),
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
