//go:build enterprise

package portal

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	errorMessageOrgRulesLimit             = "Organization rules limit reached on your current plan, please upgrade to create more."
	errorMessageOrgRulesSubscription      = "You need an active subscription to create organization rules."
	errorMessagePropertyRulesLimit        = "Property rules limit reached on your current plan, please upgrade to create more."
	errorMessagePropertyRulesSubscription = "You need an active subscription to create property rules."
	errorMessagePropertyRulesOwnerLimit   = "Property rules limit reached for this organization's owner, contact them to upgrade."
)

func (s *Server) validateOrgRulesLimit(ctx context.Context, org *dbgen.Organization, user *dbgen.User) string {
	var subscr *dbgen.Subscription
	var err error

	if user.SubscriptionID.Valid {
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	ok, extra, err := s.SubscriptionLimits.CheckOrgRulesLimit(ctx, org.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return errorMessageOrgRulesSubscription
		}
		return ""
	}

	if !ok {
		slog.WarnContext(ctx, "Organization rules limit check failed", "extra", extra, "orgID", org.ID, "subscriptionID", subscr.ID,
			"internal", db.IsInternalSubscription(subscr.Source))
		return errorMessageOrgRulesLimit
	}

	return ""
}

func (s *Server) validatePropertyRulesLimit(ctx context.Context, property *dbgen.Property, org *dbgen.Organization, sessUser *dbgen.User) string {
	_, subscr, err := s.Store.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, sessUser)
	if err != nil {
		return ""
	}

	isOrgOwner := org.UserID.Int32 == sessUser.ID

	ok, extra, err := s.SubscriptionLimits.CheckPropertyRulesLimit(ctx, property.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			if isOrgOwner {
				return errorMessagePropertyRulesSubscription
			}

			return "Organization owner needs an active subscription to create property rules."
		}
		return ""
	}

	if !ok {
		slog.WarnContext(ctx, "Property rules limit check failed", "extra", extra, "propertyID", property.ID, "subscriptionID", subscr.ID,
			"orgOwner", isOrgOwner, "internal", db.IsInternalSubscription(subscr.Source))

		if isOrgOwner {
			return errorMessagePropertyRulesLimit
		}

		return errorMessagePropertyRulesOwnerLimit
	}

	return ""
}

func (s *Server) postOrgNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	if limitError := s.validateOrgRulesLimit(ctx, org, user); len(limitError) > 0 {
		slog.WarnContext(ctx, "Org rules limit validation failed", "orgID", org.ID, "userID", user.ID, "error", limitError)
		http.Error(w, limitError, http.StatusForbidden)
		return
	}

	// TODO: Implement actual rule creation logic once rules table is created
	slog.InfoContext(ctx, "Org rule creation placeholder", "orgID", org.ID, "userID", user.ID)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) postPropertyNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	property, err := s.Property(org, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if limitError := s.validatePropertyRulesLimit(ctx, property, org, user); len(limitError) > 0 {
		slog.WarnContext(ctx, "Property rules limit validation failed", "propertyID", property.ID, "orgID", org.ID, "userID", user.ID, "error", limitError)
		http.Error(w, limitError, http.StatusForbidden)
		return
	}

	// TODO: Implement actual rule creation logic once rules table is created
	slog.InfoContext(ctx, "Property rule creation placeholder", "propertyID", property.ID, "orgID", org.ID, "userID", user.ID)

	w.WriteHeader(http.StatusOK)
}
