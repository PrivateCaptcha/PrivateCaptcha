package portal

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type propertyWizardRenderContext struct {
	Token       string
	NameError   string
	DomainError string
	CurrentOrg  *userOrg
}

type orgPropertiesRenderContext struct {
	Properties []interface{}
	CurrentOrg *userOrg
}

func (s *Server) getOrgProperties(w http.ResponseWriter, r *http.Request) {
	data := &orgPropertiesRenderContext{
		Properties: []interface{}{},
		CurrentOrg: &userOrg{
			ID: r.PathValue("org"),
		},
	}
	s.render(w, r, "portal/properties.html", data)
}

func (s *Server) getNewOrgProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok || (len(email) == 0) {
		slog.ErrorContext(ctx, "Failed to get user email from context")
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	orgID, err := strconv.Atoi(r.PathValue("org"))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert orgID to int", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusBadRequest, w, r)
		return
	}

	org, err := s.Store.RetrieveOrganization(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	data := &propertyWizardRenderContext{
		Token: s.XSRF.Token(email, actionNewProperty),
		CurrentOrg: &userOrg{
			Name:  org.OrgName,
			ID:    strconv.Itoa(int(org.ID)),
			Level: "",
		},
	}

	s.render(w, r, "property-wizard/wizard.html", data)
}
