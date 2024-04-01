package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	errInvalidSession = errors.New("session contains invalid data")
)

type userOrg struct {
	Name  string
	ID    string
	Level string
}

type orgDashboardRenderContext struct {
	UserName   string
	Orgs       []*userOrg
	CurrentOrg *userOrg
	// shortened from CurrentOrgProperties for usability
	Properties []interface{}
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

func (s *Server) getOrgDashboard(w http.ResponseWriter, r *http.Request) {
	orgID, _ := strconv.Atoi(r.PathValue("org"))

	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)
	renderCtx, err := s.createOrgDashboardContext(ctx, int32(orgID), sess)
	if err != nil {
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	s.render(w, r, "portal/org-dashboard.html", renderCtx)
}

func (s *Server) createOrgDashboardContext(ctx context.Context, orgID int32, sess *common.Session) (*orgDashboardRenderContext, error) {
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok || (len(email) == 0) {
		slog.ErrorContext(ctx, "Failed to get user email from context")
		return nil, errInvalidSession
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		return nil, err
	}

	orgs, err := s.Store.FindUserOrganizations(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user orgs", common.ErrAttr(err))
		return nil, err
	}

	renderCtx := &orgDashboardRenderContext{
		UserName: user.UserName,
		Orgs:     orgsToUserOrgs(orgs),
	}

	idx := slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID })
	if idx >= 0 {
		renderCtx.CurrentOrg = renderCtx.Orgs[idx]
	} else if len(renderCtx.Orgs) > 0 {
		earliestIdx := 0
		earliestDate := time.Now()

		for i, o := range orgs {
			if (o.Level == string(dbgen.AccessLevelOwner)) && o.Organization.CreatedAt.Time.Before(earliestDate) {
				earliestIdx = i
				earliestDate = o.Organization.CreatedAt.Time
			}
		}

		renderCtx.CurrentOrg = renderCtx.Orgs[earliestIdx]
	}

	return renderCtx, nil
}
