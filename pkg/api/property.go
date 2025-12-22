//go:build enterprise

package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
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
	deletePropertiesHandlerID = "api-delete-properties"
	updatePropertiesHandlerID = "api-update-properties"
)

type asyncTaskCreateProperties struct {
	Properties []*apiCreatePropertyInput `json:"properties"`
	OrgID      int32                     `json:"org_id"`
}

type asyncTaskDeleteProperties struct {
	PropertyIDs  []int32 `json:"property_ids"`
	AllowedOrgID int32   `json:"allowed_org_id,omitempty"`
}

type asyncTaskUpdateProperties struct {
	AllowedOrgID int32                     `json:"allowed_org_id,omitempty"`
	Properties   []*apiUpdatePropertyInput `json:"properties"`
}

func (p *apiPropertySettings) Normalize() {
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

func (s *Server) readCreatePropertiesRequest(ctx context.Context, r *http.Request, orgID int32) ([]*apiCreatePropertyInput, common.StatusCode, error) {
	if r.Header.Get(common.HeaderContentType) != common.ContentTypeJSON {
		return nil, 0, db.ErrInvalidInput
	}

	namesMap := make(map[string]struct{}, maxPropertiesBatchSize/2)

	// NOTE: by design those are (potentially) limited set (max first page) of org properties
	if properties, err := s.BusinessDB.Impl().GetCachedOrgProperties(ctx, orgID); err == nil {
		slog.DebugContext(ctx, "Fetched cached org properties", "count", len(properties))
		for _, property := range properties {
			namesMap[property.Name] = struct{}{}
		}
	}

	var inputs []*apiCreatePropertyInput
	decoder := json.NewDecoder(r.Body)

	if t, err := decoder.Token(); err != nil || t != json.Delim('[') {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to parse new properties request: expected '['", common.ErrAttr(err))
		}
		return nil, 0, db.ErrInvalidInput
	}

	for decoder.More() {
		if len(inputs) >= maxPropertiesBatchSize {
			slog.WarnContext(ctx, "Too many properties in a batch", "count", len(inputs), "max", maxPropertiesBatchSize)
			return nil, common.StatusPropertiesTooManyError, nil
		}

		var input apiCreatePropertyInput
		if err := decoder.Decode(&input); err != nil {
			if err != io.EOF {
				slog.WarnContext(ctx, "Failed to parse new properties request", common.ErrAttr(err))
			}
			return nil, 0, db.ErrInvalidInput
		}

		ilog := slog.With("index", len(inputs), "domain", input.Domain, "name", input.Name)

		name := strings.TrimSpace(input.Name)
		if _, ok := namesMap[name]; ok {
			ilog.WarnContext(ctx, "Property name duplicate found")
			return nil, common.StatusPropertyNameDuplicateError, nil
		}

		if nameStatus := s.BusinessDB.Impl().ValidatePropertyName(ctx, name, nil /*org*/); !nameStatus.Success() {
			ilog.WarnContext(ctx, "Property name failed validation", "reason", nameStatus.String())
			return nil, nameStatus, nil
		}

		namesMap[name] = struct{}{}

		if len(input.Domain) == 0 {
			ilog.WarnContext(ctx, "Property domain name is empty")
			return nil, common.StatusPropertyDomainEmptyError, nil
		}

		domain, err := common.ParseDomainName(input.Domain)
		if err != nil {
			ilog.WarnContext(ctx, "Failed to parse domain name", common.ErrAttr(err))
			return nil, common.StatusPropertyDomainFormatError, nil
		}

		if common.IsLocalhost(domain) {
			ilog.WarnContext(ctx, "Property domain name is localhost")
			return nil, common.StatusPropertyDomainLocalhostError, nil
		}

		if common.IsIPAddress(domain) {
			ilog.WarnContext(ctx, "Property domain name is IP")
			return nil, common.StatusPropertyDomainIPAddrError, nil
		}

		if _, err := idna.Lookup.ToASCII(domain); err != nil {
			ilog.WarnContext(ctx, "Failed to convert domain name to ASCII", common.ErrAttr(err))
			return nil, common.StatusPropertyDomainNameInvalidError, nil
		}

		inputs = append(inputs, &input)
	}

	if t, err := decoder.Token(); err != nil || t != json.Delim(']') {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to parse new properties request: expected ']'", common.ErrAttr(err))
		}
		return nil, 0, db.ErrInvalidInput
	}

	return inputs, common.StatusOK, nil
}

func (s *Server) postNewProperties(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, false /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	org, err := s.requestOrg(user, r, true /*only owner*/, &apiKey.OrgID)
	if err != nil {
		if err == db.ErrInvalidInput {
			s.sendAPIErrorResponse(ctx, common.StatusOrgIDInvalidError, r, w)
		} else {
			s.sendHTTPErrorResponse(err, w)
		}
		return
	}

	inputs, status, err := s.readCreatePropertiesRequest(ctx, r, org.ID)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}
	if status != common.StatusOK {
		s.sendAPIErrorResponse(ctx, status, r, w)
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

func (s *Server) doCreateProperty(ctx context.Context, tlog *slog.Logger, property *apiCreatePropertyInput, user *dbgen.User, org *dbgen.Organization) common.StatusCode {
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

func (s *Server) readDeletePropertiesRequest(ctx context.Context, r *http.Request) ([]int32, common.StatusCode, error) {
	if r.Header.Get(common.HeaderContentType) != common.ContentTypeJSON {
		return nil, 0, db.ErrInvalidInput
	}

	decoder := json.NewDecoder(r.Body)

	if t, err := decoder.Token(); err != nil || t != json.Delim('[') {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to parse delete properties request: expected '['", common.ErrAttr(err))
		}
		return nil, 0, db.ErrInvalidInput
	}

	idsToDelete := make(map[int]struct{}, maxPropertiesBatchSize/2)
	var propertyIDs []int32

	for decoder.More() {
		if len(propertyIDs) >= maxPropertiesBatchSize {
			slog.WarnContext(ctx, "Too many properties in a batch", "count", len(propertyIDs), "max", maxPropertiesBatchSize)
			return nil, common.StatusPropertiesTooManyError, nil
		}

		var encID string
		if err := decoder.Decode(&encID); err != nil {
			if err != io.EOF {
				slog.WarnContext(ctx, "Failed to parse delete properties request", common.ErrAttr(err))
			}
			return nil, 0, db.ErrInvalidInput
		}

		id, err := s.IDHasher.Decrypt(encID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to decode property ID", "id", encID, common.ErrAttr(err))
			return nil, 0, db.ErrInvalidInput
		}

		if _, ok := idsToDelete[id]; ok {
			slog.WarnContext(ctx, "Duplicate property ID found", "id", encID)
			continue
		}

		idsToDelete[id] = struct{}{}
		propertyIDs = append(propertyIDs, int32(id))
	}

	if t, err := decoder.Token(); err != nil || t != json.Delim(']') {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to parse delete properties request: expected ']'", common.ErrAttr(err))
		}
		return nil, 0, db.ErrInvalidInput
	}

	if len(propertyIDs) == 0 {
		slog.WarnContext(ctx, "Empty delete properties list")
		return nil, 0, db.ErrInvalidInput
	}

	return propertyIDs, common.StatusOK, nil
}

func (s *Server) deleteProperties(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, false /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	propertyIDs, status, err := s.readDeletePropertiesRequest(ctx, r)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}
	if status != common.StatusOK {
		s.sendAPIErrorResponse(ctx, status, r, w)
		return
	}

	referenceID := db.UUIDToSecret(apiKey.ExternalID)
	request := &asyncTaskDeleteProperties{
		PropertyIDs: propertyIDs,
	}

	if apiKey.OrgID.Valid {
		request.AllowedOrgID = apiKey.OrgID.Int32
	}

	buffer := 5 * time.Minute
	// we schedule it for later, making "room" for immediate attempt first
	scheduledAt := time.Now().UTC().Add(buffer)
	task, err := s.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, deletePropertiesHandlerID, user, scheduledAt, referenceID)
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

func (s *Server) handleDeleteProperties(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
	taskID := db.UUIDToString(task.ID)
	tlog := slog.With("taskID", taskID)

	tlog.DebugContext(ctx, "Processing delete properties task")

	params := &asyncTaskDeleteProperties{}
	if err := json.Unmarshal(task.Input, params); err != nil {
		tlog.ErrorContext(ctx, "Failed to unmarshal delete properties async task input", common.ErrAttr(err))
		return nil, err
	}

	user, err := s.BusinessDB.Impl().RetrieveUser(ctx, task.UserID.Int32)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to retrieve user", "userID", task.UserID.Int32, common.ErrAttr(err))
		return nil, err
	}

	results, err := s.doDeleteProperties(ctx, tlog, user, params)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to delete properties", common.ErrAttr(err))
		return nil, err
	}

	data, err := json.Marshal(results)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to serialize results", common.ErrAttr(err))
		data = nil
	}

	return data, nil
}

func (s *Server) doDeleteProperties(ctx context.Context, tlog *slog.Logger, user *dbgen.User, params *asyncTaskDeleteProperties) ([]*operationResult, error) {
	var org *dbgen.Organization
	if params.AllowedOrgID != 0 {
		slog.DebugContext(ctx, "Delete task is scoped for org", "orgID", params.AllowedOrgID)
		var err error
		if org, err = s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, params.AllowedOrgID); err != nil {
			return nil, err
		}
	}

	deletedIDs, auditEvents, err := s.BusinessDB.Impl().SoftDeleteProperties(ctx, params.PropertyIDs, user, org)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to soft delete properties", common.ErrAttr(err))
		return nil, err
	}

	s.BusinessDB.AuditLog().RecordEvents(ctx, auditEvents, common.AuditLogSourceAPI)

	results := make([]*operationResult, 0, len(params.PropertyIDs))

	for i, propertyID := range params.PropertyIDs {
		result := &operationResult{Code: common.StatusOK}
		if _, ok := deletedIDs[propertyID]; !ok {
			tlog.WarnContext(ctx, "Property was not deleted", "index", i, "propertyID", propertyID)
			result.Code = common.StatusFailure
		}
		results = append(results, result)
	}

	return results, nil
}

func (s *Server) readUpdatePropertiesRequest(ctx context.Context, r *http.Request) ([]*apiUpdatePropertyInput, common.StatusCode, error) {
	if r.Header.Get(common.HeaderContentType) != common.ContentTypeJSON {
		return nil, 0, db.ErrInvalidInput
	}

	decoder := json.NewDecoder(r.Body)

	if t, err := decoder.Token(); err != nil || t != json.Delim('[') {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to parse update properties request: expected '['", common.ErrAttr(err))
		}
		return nil, 0, db.ErrInvalidInput
	}

	var inputs []*apiUpdatePropertyInput
	idsMap := make(map[string]struct{}, maxPropertiesBatchSize/2)
	nameMap := make(map[string]struct{}, maxPropertiesBatchSize/2)

	for decoder.More() {
		if len(inputs) >= maxPropertiesBatchSize {
			slog.WarnContext(ctx, "Too many properties in a batch", "count", len(inputs), "max", maxPropertiesBatchSize)
			return nil, common.StatusPropertiesTooManyError, nil
		}

		var input apiUpdatePropertyInput
		if err := decoder.Decode(&input); err != nil {
			if err != io.EOF {
				slog.WarnContext(ctx, "Failed to parse update properties request", common.ErrAttr(err))
			}
			return nil, 0, db.ErrInvalidInput
		}

		ilog := slog.With("index", len(inputs), "id", input.ID, "name", input.Name)

		if len(input.ID) == 0 {
			ilog.WarnContext(ctx, "Property ID is empty")
			return nil, common.StatusPropertyIDEmptyError, nil
		}

		if _, ok := idsMap[input.ID]; ok {
			ilog.WarnContext(ctx, "Property ID duplicate found")
			return nil, common.StatusPropertyIDDuplicateError, nil
		}

		idsMap[input.ID] = struct{}{}

		name := strings.TrimSpace(input.Name)
		if _, ok := nameMap[name]; ok {
			ilog.WarnContext(ctx, "Property name duplicate found")
			return nil, common.StatusPropertyNameDuplicateError, nil
		}

		if nameStatus := s.BusinessDB.Impl().ValidatePropertyName(ctx, name, nil /*org*/); !nameStatus.Success() {
			ilog.WarnContext(ctx, "Property name failed validation", "reason", nameStatus.String())
			return nil, nameStatus, nil
		}

		nameMap[name] = struct{}{}

		inputs = append(inputs, &input)
	}

	if t, err := decoder.Token(); err != nil || t != json.Delim(']') {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to parse update properties request: expected ']'", common.ErrAttr(err))
		}
		return nil, 0, db.ErrInvalidInput
	}

	if len(inputs) == 0 {
		slog.WarnContext(ctx, "Empty update properties list")
		return nil, 0, db.ErrInvalidInput
	}

	return inputs, common.StatusOK, nil
}

func (s *Server) updateProperties(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, false /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	inputs, status, err := s.readUpdatePropertiesRequest(ctx, r)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}
	if status != common.StatusOK {
		s.sendAPIErrorResponse(ctx, status, r, w)
		return
	}

	referenceID := db.UUIDToSecret(apiKey.ExternalID)
	request := &asyncTaskUpdateProperties{
		Properties: inputs,
	}

	if apiKey.OrgID.Valid {
		request.AllowedOrgID = apiKey.OrgID.Int32
	}

	buffer := 5 * time.Minute
	// we schedule it for later, making "room" for immediate attempt first
	scheduledAt := time.Now().UTC().Add(buffer)
	task, err := s.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, updatePropertiesHandlerID, user, scheduledAt, referenceID)
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

func (s *Server) handleUpdateProperties(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
	taskID := db.UUIDToString(task.ID)
	tlog := slog.With("taskID", taskID)

	tlog.DebugContext(ctx, "Processing update properties task")

	params := &asyncTaskUpdateProperties{}
	if err := json.Unmarshal(task.Input, params); err != nil {
		tlog.ErrorContext(ctx, "Failed to unmarshal update properties async task input", common.ErrAttr(err))
		return nil, err
	}

	user, err := s.BusinessDB.Impl().RetrieveUser(ctx, task.UserID.Int32)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to retrieve user", "userID", task.UserID.Int32, common.ErrAttr(err))
		return nil, err
	}

	results, err := s.doUpdateProperties(ctx, tlog, user, params)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to update properties", common.ErrAttr(err))
		return nil, err
	}

	data, err := json.Marshal(results)
	if err != nil {
		tlog.ErrorContext(ctx, "Failed to serialize results", common.ErrAttr(err))
		data = nil
	}

	return data, nil
}

func (s *Server) doUpdateProperties(ctx context.Context, tlog *slog.Logger, user *dbgen.User, params *asyncTaskUpdateProperties) ([]*operationResult, error) {
	b := &backoff.Backoff{
		Min:    200 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 1.1,
		Jitter: true,
	}

	results := make([]*operationResult, 0, len(params.Properties))

	var org *dbgen.Organization
	if params.AllowedOrgID != 0 {
		slog.DebugContext(ctx, "Update task is scoped for org", "orgID", params.AllowedOrgID)
		var err error
		if org, err = s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, params.AllowedOrgID); err != nil {
			return nil, err
		}
	}

	for i, property := range params.Properties {
		if i > 0 {
			time.Sleep(b.Duration())
		}

		status := s.doUpdateProperty(ctx, tlog.With("index", i), property, user, org)
		results = append(results, &operationResult{Code: status})
	}

	return results, nil
}

func (s *Server) doUpdateProperty(ctx context.Context, tlog *slog.Logger, propertyInput *apiUpdatePropertyInput, user *dbgen.User, org *dbgen.Organization) common.StatusCode {
	propertyID, err := s.IDHasher.Decrypt(propertyInput.ID)
	if err != nil {
		tlog.WarnContext(ctx, "Failed to decrypt property ID", "id", propertyInput.ID, common.ErrAttr(err))
		return common.StatusPropertyIDInvalidError
	}

	propertyInput.Normalize()

	params := &dbgen.UpdatePropertyParams{
		ID:               int32(propertyID),
		Name:             propertyInput.Name,
		Level:            db.Int2(int16(propertyInput.Level)),
		Growth:           dbgen.DifficultyGrowth(propertyInput.Growth),
		ValidityInterval: time.Duration(propertyInput.ValiditySeconds) * time.Second,
		AllowSubdomains:  propertyInput.AllowSubdomains,
		AllowLocalhost:   propertyInput.AllowLocalhost,
		MaxReplayCount:   int32(propertyInput.MaxReplayCount),
	}

	_, auditEvent, err := s.BusinessDB.Impl().UpdateProperty(ctx, org, user, params)
	if err != nil {
		if err == db.ErrPermissions {
			return common.StatusOrgPermissionsError
		}
		tlog.ErrorContext(ctx, "Failed to update the property", common.ErrAttr(err))
		return common.StatusFailure
	}

	s.BusinessDB.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourceAPI)

	return common.StatusOK
}

func propertyToApiOrgProperty(property *dbgen.Property, hasher common.IdentifierHasher) *apiOrgPropertyOutput {
	return &apiOrgPropertyOutput{
		ID:      hasher.Encrypt(int(property.ID)),
		Name:    property.Name,
		Sitekey: db.UUIDToSiteKey(property.ExternalID),
	}
}

func propertiesToApiOrgProperties(properties []*dbgen.Property, hasher common.IdentifierHasher) []*apiOrgPropertyOutput {
	result := make([]*apiOrgPropertyOutput, 0, len(properties))
	for _, property := range properties {
		result = append(result, propertyToApiOrgProperty(property, hasher))
	}
	return result
}

func (s *Server) getOrgProperties(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, true /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	org, err := s.requestOrg(user, r, true /*only owner*/, &apiKey.OrgID)
	if err != nil {
		if err == db.ErrInvalidInput {
			s.sendAPIErrorResponse(ctx, common.StatusOrgIDInvalidError, r, w)
		} else {
			s.sendHTTPErrorResponse(err, w)
		}
		return
	}

	pageParam := r.URL.Query().Get(common.ParamPage)
	page := 0
	if len(pageParam) > 0 {
		if page, err = strconv.Atoi(pageParam); err != nil {
			slog.ErrorContext(ctx, "Failed to convert page parameter", "page", pageParam, common.ErrAttr(err))
			page = 0
		}
		if page < 0 {
			s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
			return
		}
	}

	perPageParam := r.URL.Query().Get(common.ParamPerPage)
	perPage := db.MaxOrgPropertiesPageSize
	if len(perPageParam) > 0 {
		if perPage, err = strconv.Atoi(perPageParam); err != nil {
			slog.ErrorContext(ctx, "Failed to convert per_page parameter", "perPage", perPageParam, common.ErrAttr(err))
			perPage = 0
		}
		if perPage <= 0 {
			s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
			return
		}
	}

	validatedPerPage := min(db.MaxOrgPropertiesPageSize, max(perPage, 0))
	offset := max(page, 0) * validatedPerPage

	// NOTE: we might need to add more things to etag like org.updated_at later
	etag := common.GenerateETag(strconv.Itoa(int(user.ID)), strconv.Itoa(int(org.ID)),
		strconv.Itoa(offset), strconv.Itoa(validatedPerPage))
	if etagHeader := r.Header.Get(common.HeaderIfNoneMatch); len(etagHeader) > 0 && (etagHeader == etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	properties, hasMore, err := s.BusinessDB.Impl().RetrieveOrgProperties(ctx, org, offset, validatedPerPage)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org properties", common.ErrAttr(err))
		s.sendHTTPErrorResponse(err, w)
		return
	}

	slog.DebugContext(ctx, "Retrieved org properties", "count", len(properties), "more", hasMore, "page", page, "perPage", validatedPerPage)

	response := &APIResponse{
		Data: propertiesToApiOrgProperties(properties, s.IDHasher),
		Pagination: &Pagination{
			Page:    page,
			PerPage: validatedPerPage,
			HasMore: hasMore,
		},
	}
	cacheHeaders := map[string][]string{
		common.HeaderETag:         []string{etag},
		common.HeaderCacheControl: common.PrivateCacheControl15s,
	}
	s.sendAPISuccessResponseEx(ctx, response, w, cacheHeaders)
}

func (s *Server) getOrgProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, true /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	org, err := s.requestOrg(user, r, false /*only owner*/, &apiKey.OrgID)
	if err != nil {
		if err == db.ErrInvalidInput {
			s.sendAPIErrorResponse(ctx, common.StatusOrgIDInvalidError, r, w)
		} else {
			s.sendHTTPErrorResponse(err, w)
		}
		return
	}

	property, err := s.requestProperty(org, r)
	if err != nil {
		if err == db.ErrSoftDeleted {
			s.sendAPIErrorResponse(ctx, common.StatusPropertyIDInvalidError, r, w)
		} else {
			s.sendHTTPErrorResponse(err, w)
		}
		return
	}

	data := &apiPropertyOutput{
		ID:              s.IDHasher.Encrypt(int(property.ID)),
		Name:            property.Name,
		Domain:          property.Domain,
		Sitekey:         db.UUIDToSiteKey(property.ExternalID),
		Level:           int(property.Level.Int16),
		Growth:          string(property.Growth),
		ValiditySeconds: int(property.ValidityInterval.Seconds()),
		AllowSubdomains: property.AllowSubdomains,
		AllowLocalhost:  property.AllowLocalhost,
		MaxReplayCount:  int(property.MaxReplayCount),
	}

	s.sendAPISuccessResponse(ctx, data, w)
}
