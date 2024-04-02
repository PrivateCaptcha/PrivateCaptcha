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
	// shortened from CurrentOrgProperties for simplicity
	Properties []*userProperty
}

func orgToUserOrg(org *dbgen.Organization) *userOrg {
	return &userOrg{
		Name: org.Name,
		ID:   strconv.Itoa(int(org.ID)),
	}
}

func orgsToUserOrgs(orgs []*dbgen.GetUserOrganizationsRow) []*userOrg {
	result := make([]*userOrg, 0, len(orgs))
	for _, org := range orgs {
		result = append(result, &userOrg{
			Name:  org.Organization.Name,
			ID:    strconv.Itoa(int(org.Organization.ID)),
			Level: org.Level,
		})
	}
	return result
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

	idx := -1
	if orgID >= 0 {
		idx = slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID })
	}

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

		idx = earliestIdx
		renderCtx.CurrentOrg = renderCtx.Orgs[earliestIdx]
	} else {
		renderCtx.CurrentOrg = &userOrg{ID: "-1"}
	}

	if (0 <= idx) && (idx < len(orgs)) {
		properties, err := s.Store.RetrieveOrgProperties(ctx, orgs[idx].Organization.ID)
		if err == nil {
			renderCtx.Properties = propertiesToUserProperties(properties)
		}
	}

	return renderCtx, nil
}

func (s *Server) getOrgDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	org, ok := ctx.Value(common.OrgIDContextKey).(int)
	if !ok {
		org = -1
	}

	sess := s.Session.SessionStart(w, r)
	renderCtx, err := s.createOrgDashboardContext(ctx, int32(org), sess)
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	s.render(w, r, "portal/portal.html", renderCtx)
}
