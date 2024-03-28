package portal

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type userOrg struct {
	Name  string
	ID    string
	Level string
}

type portalRenderContext struct {
	UserName   string
	Orgs       []*userOrg
	Properties []interface{}
	CurrentOrg *userOrg
}

func orgsToUserOrgs(orgs []*dbgen.GetUserOrganizationsRow) []*userOrg {
	result := make([]*userOrg, 0, len(orgs))
	for _, org := range orgs {
		result = append(result, &userOrg{
			Name:  org.Organization.OrgName,
			ID:    strconv.Itoa(int(org.Organization.ID)),
			Level: org.Level,
		})
	}
	return result
}

func (s *Server) portal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok || (len(email) == 0) {
		slog.ErrorContext(ctx, "Failed to get user email from context")
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	orgs, err := s.Store.FindUserOrganizations(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user orgs", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &portalRenderContext{
		UserName: user.UserName,
		Orgs:     orgsToUserOrgs(orgs),
	}

	if len(renderCtx.Orgs) > 0 {
		// TODO: Select current org based on path
		renderCtx.CurrentOrg = renderCtx.Orgs[0]
	}

	s.render(r.Context(), w, r, "portal/portal.html", renderCtx)
}
