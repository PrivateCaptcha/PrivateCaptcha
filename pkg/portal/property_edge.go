package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type edgePropertySettingsRenderContext struct {
	propertyDashboardRenderContext
	EdgeWidgetStartMode string
}

func (s *Server) retrieveEdgeWidgetStartMode(ctx context.Context, property *dbgen.Property) (string, error) {
	if property.EdgeTokenValidityInterval > 0 {
		settings, err := s.Store.Impl().RetrieveEdgeSettingsBySitekey(ctx, db.UUIDToSiteKey(property.ExternalID), false /*skip cache*/)
		if err == nil {
			return string(settings.EdgeWidgetStartMode), nil
		}
		if !errors.Is(err, db.ErrRecordNotFound) {
			return "", err
		}
		slog.DebugContext(ctx, "Using default edge widget start mode", "propID", property.ID)
	}
	return string(dbgen.EdgeWidgetStartModeClick), nil
}

func (s *Server) putPropertyEdgeSettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read edge settings form", common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}
	dashboardCtx, property, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, err
	}
	mode, err := s.retrieveEdgeWidgetStartMode(ctx, property)
	if err != nil {
		return nil, err
	}
	renderCtx := &edgePropertySettingsRenderContext{
		propertyDashboardRenderContext: *dashboardCtx,
		EdgeWidgetStartMode:            mode,
	}
	view := &ViewModel{Model: renderCtx, View: propertySettingsEdgeFormTemplate, IsNew: false}
	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = common.StatusPropertyPermissionsError.String()
		return view, nil
	}
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}
	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}
	validity, err := edgeTokenValidityFromIndex(r.FormValue(common.ParamEdgeTokenValidityInterval))
	if err != nil {
		renderCtx.ErrorMessage = common.StatusPropertyEdgeIntervalError.String()
		return view, nil
	}
	if validity > 0 && !s.FeatureFlags.Enabled(ctx, common.FeatureEdgeTokens, &user.ID, &org.ID) {
		renderCtx.ErrorMessage = common.StatusPropertyPermissionsError.String()
		return view, nil
	}
	startMode := dbgen.EdgeWidgetStartMode(r.FormValue(common.ParamEdgeWidgetStartMode))
	if validity > 0 && startMode != dbgen.EdgeWidgetStartModeClick && startMode != dbgen.EdgeWidgetStartModeLoad {
		renderCtx.ErrorMessage = "Invalid edge widget start mode."
		return view, nil
	}
	if err := s.EdgeTokens.ValidateLifetime(ctx, validity); err != nil {
		renderCtx.ErrorMessage = "Edge protection is not configured on this server. Please contact your operator."
		return view, nil
	}
	if err := s.EdgeTokens.ValidateProperty(ctx, property.Domain, validity); err != nil {
		renderCtx.ErrorMessage = common.StatusPropertyEdgeDomainError.String()
		return view, nil
	}
	if validity != property.EdgeTokenValidityInterval {
		var event *common.AuditLogEvent
		params := db.NewUpdatePropertyParams(property)
		params.EdgeTokenValidityInterval = validity
		property, event, err = s.Store.Impl().UpdateProperty(ctx, org, user, params)
		if err != nil {
			renderCtx.ErrorMessage = "Failed to update edge protection. Please try again."
			return view, nil
		}
		view.AuditEvents = singleAuditEvents(event)
		renderCtx.Property = propertyToUserProperty(property, s.IDHasher)
	}
	if property.EdgeTokenValidityInterval > 0 {
		settings, event, err := s.Store.Impl().UpdateEdgeSettings(ctx, org, user, property, startMode)
		if err != nil {
			renderCtx.ErrorMessage = "Failed to update edge settings. Please try again."
			return view, nil
		}
		if event != nil {
			view.AuditEvents = append(view.AuditEvents, event)
		}
		renderCtx.EdgeWidgetStartMode = string(settings.EdgeWidgetStartMode)
	} else {
		renderCtx.EdgeWidgetStartMode = string(dbgen.EdgeWidgetStartModeClick)
	}

	slog.InfoContext(ctx, "Edited edge protection", "propID", property.ID, "orgID", org.ID, "enabled", property.EdgeTokenValidityInterval > 0)
	renderCtx.SuccessMessage = "Edge protection was updated"

	return view, nil
}
