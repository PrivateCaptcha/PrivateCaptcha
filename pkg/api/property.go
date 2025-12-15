//go:build enterprise

package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"

	"github.com/jpillora/backoff"
	"golang.org/x/net/idna"
)

const (
	maxPropertiesBatchSize    = 128
	createPropertiesHandlerID = "api-create-properties"
)

type asyncTaskCreateProperties struct {
	Properties []*apiPropertyInput `json:"properties"`
	OrgID      int32               `json:"org_id"`
}

func (p *apiPropertyInput) Normalize() {
	p.Name = strings.TrimSpace(p.Name)

	const (
		minDifficultyLevel = 1
		maxDifficultyLevel = int(common.MaxDifficultyLevel)
	)
	p.Level = max(minDifficultyLevel, min(maxDifficultyLevel, p.Level))

	const (
		maxMaxReplayValue = 1_000_000
		minMaxReplayValue = 1
	)
	p.MaxReplayCount = max(minMaxReplayValue, min(p.MaxReplayCount, maxMaxReplayValue))

	switch p.Growth {
	case string(dbgen.DifficultyGrowthConstant),
		string(dbgen.DifficultyGrowthFast),
		string(dbgen.DifficultyGrowthMedium),
		string(dbgen.DifficultyGrowthSlow):
	default:
		p.Growth = string(dbgen.DifficultyGrowthMedium)
	}

	if p.ValiditySeconds > 0 {
		validityIndex := puzzle.ValidityIntervalToIndex(time.Duration(p.ValiditySeconds) * time.Second)
		p.ValiditySeconds = int(puzzle.ValidityDurations[validityIndex].Seconds())
	} else {
		const defaultValidityPeriod = 6 * time.Hour
		p.ValiditySeconds = int(defaultValidityPeriod.Seconds())
	}
}

func (s *Server) validateApiProperties(ctx context.Context, inputs []*apiPropertyInput, orgID int32) common.StatusCode {
	if len(inputs) > maxPropertiesBatchSize {
		slog.WarnContext(ctx, "Too many properties in a batch", "count", len(inputs), "max", maxPropertiesBatchSize)
		return common.StatusPropertiesTooManyError
	}

	namesMap := make(map[string]struct{})

	if properties, err := s.BusinessDB.Impl().GetCachedOrgProperties(ctx, orgID); err == nil {
		slog.DebugContext(ctx, "Fetched cached org properties", "count", len(properties))
		for _, property := range properties {
			namesMap[property.Name] = struct{}{}
		}
	}

	for i, input := range inputs {
		ilog := slog.With("index", i, "domain", input.Domain, "name", input.Name)

		name := strings.TrimSpace(input.Name)
		if _, ok := namesMap[name]; ok {
			ilog.WarnContext(ctx, "Property name duplicate found")
			return common.StatusPropertyNameDuplicateError
		}

		if nameStatus := s.BusinessDB.Impl().ValidatePropertyName(ctx, name, nil /*org*/); !nameStatus.Success() {
			ilog.WarnContext(ctx, "Property name failed validation", "reason", nameStatus.String())
			return nameStatus
		}

		namesMap[name] = struct{}{}

		if len(input.Domain) == 0 {
			ilog.WarnContext(ctx, "Property domain name is empty")
			return common.StatusPropertyDomainEmptyError
		}

		domain, err := common.ParseDomainName(input.Domain)
		if err != nil {
			ilog.WarnContext(ctx, "Failed to parse domain name", common.ErrAttr(err))
			return common.StatusPropertyDomainFormatError
		}

		if common.IsLocalhost(domain) {
			ilog.WarnContext(ctx, "Property domain name is localhost")
			return common.StatusPropertyDomainLocalhostError
		}

		if common.IsIPAddress(domain) {
			ilog.WarnContext(ctx, "Property domain name is IP")
			return common.StatusPropertyDomainIPAddrError
		}

		if _, err := idna.Lookup.ToASCII(domain); err != nil {
			ilog.WarnContext(ctx, "Failed to convert domain name to ASCII", common.ErrAttr(err))
			return common.StatusPropertyDomainNameInvalidError
		}
	}

	return common.StatusOK
}

func (s *Server) postNewProperties(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	var inputs []*apiPropertyInput
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&inputs); err != nil {
		slog.WarnContext(ctx, "Failed to parse new properties request", common.ErrAttr(err))
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	org, err := s.requestOrg(user, r, true /*only owner*/)
	if err != nil {
		if err == db.ErrInvalidInput {
			s.sendAPIErrorResponse(ctx, common.StatusOrgIDInvalidError, r, w)
		} else {
			s.sendHTTPErrorResponse(err, w)
		}
		return
	}

	if statusCode := s.validateApiProperties(ctx, inputs, org.ID); !statusCode.Success() {
		s.sendAPIErrorResponse(ctx, statusCode, r, w)
		return
	}

	owner, subscr, err := s.BusinessDB.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, user)
	if err != nil {
		s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		return
	}

	// extra == (count - plan.limit()) so negative "extra" means we have left (-extra) space for new properties
	if ok, extra, err := s.SubscriptionLimits.CheckPropertiesLimit(ctx, owner.ID, subscr); (err != nil) || !ok || (len(inputs) > (-extra)) {
		slog.WarnContext(ctx, "User hit subscription limits", "count", len(inputs), "ok", ok, "extra", extra, common.ErrAttr(err))
		s.sendAPIErrorResponse(ctx, common.StatusSubscriptionPropertyLimitError, r, w)
		return
	}

	referenceID := db.UUIDToSecret(apiKey.ExternalID)
	request := &asyncTaskCreateProperties{
		Properties: inputs,
		OrgID:      org.ID,
	}

	buffer := 5 * time.Minute
	// we schedule it for later, making "room" for immediate attempt first
	scheduledAt := time.Now().UTC().Add(buffer)
	task, err := s.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, createPropertiesHandlerID, user, scheduledAt, referenceID)
	if err != nil {
		s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		return
	}

	output := &apiAsyncTaskOutput{
		ID: db.UUIDToString(task.ID),
	}

	s.sendAPISuccessResponse(ctx, output, w)

	go func(bctx context.Context) {
		handlerCtx, cancel := context.WithTimeout(bctx, buffer)
		defer cancel()
		if err := s.AsyncTasks.Execute(handlerCtx, task); err != nil {
			slog.ErrorContext(bctx, "Failed to execute async task", "taskID", output.ID, common.ErrAttr(err))
		}
	}(common.CopyTraceID(ctx, context.Background()))
}

func (s *Server) handleCreateProperties(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
	taskID := db.UUIDToString(task.ID)
	tlog := slog.With("taskID", taskID)

	tlog.DebugContext(ctx, "Processing create properties task")

	params := &asyncTaskCreateProperties{}
	if err := json.Unmarshal(task.Input, params); err != nil {

		tlog.ErrorContext(ctx, "Failed to unmarshal create properties async task input", common.ErrAttr(err))
		return nil, err
	}

	user, err := s.BusinessDB.Impl().RetrieveUser(ctx, task.UserID.Int32)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to retrieve user", "userID", task.UserID.Int32, common.ErrAttr(err))
		return nil, err
	}

	results, err := s.doCreateProperties(ctx, tlog, user, params)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to create properties", common.ErrAttr(err))
		return nil, err
	}

	data, err := json.Marshal(results)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to serialize results", common.ErrAttr(err))
		data = nil
	}

	return data, nil
}

func (s *Server) doCreateProperties(ctx context.Context, tlog *slog.Logger, user *dbgen.User, params *asyncTaskCreateProperties) ([]*operationResult, error) {
	org, err := s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, params.OrgID)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to retrieve org", common.ErrAttr(err))
		return nil, err
	}

	owner, subscr, err := s.BusinessDB.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, user)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to retrieve org owner with subscription", common.ErrAttr(err))
		return nil, err
	}

	b := &backoff.Backoff{
		Min:    200 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 1.1,
		Jitter: true,
	}

	results := make([]*operationResult, 0, len(params.Properties))
	limitCheckIndex := 1

	for i, property := range params.Properties {
		if i > 0 {
			time.Sleep(b.Duration())
		}

		// TODO: Create properties in batches instead of one by one
		// the only reason why it's not done is that it's not clear if this is a bottleneck right now AND
		// maybe it will not be the most popular API
		status := s.doCreateProperty(ctx, tlog.With("index", i), property, user, org)
		results = append(results, &operationResult{Code: status})

		// check user limits with a logarithmic step to make less DB round trips
		if i == limitCheckIndex {
			limitCheckIndex *= 2

			if ok, _, err := s.SubscriptionLimits.CheckPropertiesLimit(ctx, owner.ID, subscr); (err != nil) || !ok {
				tlog.WarnContext(ctx, "Skipping property creation due to subscription limit", "subscrID", subscr.ID)
				break
			}
		}
	}

	for len(results) < len(params.Properties) {
		results = append(results, &operationResult{Code: common.StatusSubscriptionPropertyLimitError})
	}

	return results, nil
}

func (s *Server) doCreateProperty(ctx context.Context, tlog *slog.Logger, property *apiPropertyInput, user *dbgen.User, org *dbgen.Organization) common.StatusCode {
	// this should have been filtered out when we validated user request
	// but we repeat this here because we save to DB _exact_ user request
	domain, err := common.ParseDomainName(property.Domain)
	if err != nil {
		tlog.WarnContext(ctx, "Failed to parse domain name", "domain", property.Domain, common.ErrAttr(err))
		return common.StatusPropertyDomainFormatError
	}

	// NOTE: we do NOT validate property name "for real" (against other org properties) due to too many DB roundtrips.
	// This will be validated anyways during INSERT query below the only user impact is returning StatusFailure
	// instead of StatusPropertyNameDuplicateError

	property.Normalize()

	_, auditEvent, err := s.BusinessDB.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:             property.Name,
		CreatorID:        db.Int(user.ID),
		Domain:           domain,
		Level:            db.Int2(int16(property.Level)),
		Growth:           dbgen.DifficultyGrowth(property.Growth),
		ValidityInterval: time.Duration(property.ValiditySeconds) * time.Second,
		AllowSubdomains:  property.AllowSubdomains,
		AllowLocalhost:   property.AllowLocalhost,
		MaxReplayCount:   int32(property.MaxReplayCount),
	}, org)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to create the property", common.ErrAttr(err))
		return common.StatusFailure
	}

	s.BusinessDB.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourceAPI)

	return common.StatusOK
}
