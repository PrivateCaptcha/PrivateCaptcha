package db

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type AuditLog struct {
	querier       dbgen.Querier
	persistChan   chan *common.AuditLogEvent
	persistCancel context.CancelFunc
	batchSize     int
}

var _ common.AuditLog = (*AuditLog)(nil)

func NewAuditLog(querier dbgen.Querier, batchSize int) *AuditLog {
	return &AuditLog{
		querier:       querier,
		persistChan:   make(chan *common.AuditLogEvent, batchSize),
		persistCancel: func() {},
		batchSize:     batchSize,
	}
}

func (al *AuditLog) Start(ctx context.Context, interval time.Duration) {
	var cancelCtx context.Context
	cancelCtx, al.persistCancel = context.WithCancel(
		context.WithValue(ctx, common.TraceIDContextKey, "persist_auditlog"))
	go common.ProcessBatchArray(cancelCtx, al.persistChan, interval, al.batchSize, al.batchSize*10, al.persistAuditLog)
}

func (al *AuditLog) Shutdown() {
	slog.Debug("Shutting down persisting sessions")
	al.persistCancel()
	close(al.persistChan)
}

func (al *AuditLog) persistAuditLog(ctx context.Context, batch []*common.AuditLogEvent) error {
	dbBatch := make([]*dbgen.CreateAuditLogsParams, 0, len(batch))

	for _, e := range batch {
		action := dbgen.AuditLogActionUnknown
		switch e.Action {
		case common.AuditLogActionCreate:
			action = dbgen.AuditLogActionCreate
		case common.AuditLogActionUpdate:
			action = dbgen.AuditLogActionUpdate
		case common.AuditLogActionDelete:
			action = dbgen.AuditLogActionDelete
		case common.AuditLogActionSoftDelete:
			action = dbgen.AuditLogActionSoftDelete
		case common.AuditLogActionRecover:
			action = dbgen.AuditLogActionRecover
		case common.AuditLogActionLogin:
			action = dbgen.AuditLogActionLogin
		case common.AuditLogActionLogout:
			action = dbgen.AuditLogActionLogout
		case common.AuditLogActionAccess:
			action = dbgen.AuditLogActionAccess
		}

		event := &dbgen.CreateAuditLogsParams{
			UserID:      Int(e.UserID),
			Action:      action,
			EntityID:    Int8(e.EntityID),
			EntityTable: e.TableName,
			SessionID:   e.SessionID,
			OldValue:    nil,
			NewValue:    nil,
			CreatedAt:   Timestampz(e.Timestamp),
		}

		if e.OldValue != nil {
			if payload, err := json.Marshal(e.OldValue); err == nil {
				event.OldValue = payload
			} else {
				slog.ErrorContext(ctx, "Failed to serialize old value for audit log", "table", e.TableName, "entityID", e.EntityID, common.ErrAttr(err))
			}
		}

		if e.NewValue != nil {
			if payload, err := json.Marshal(e.NewValue); err == nil {
				event.NewValue = payload
			} else {
				slog.ErrorContext(ctx, "Failed to serialize new value for audit log", "table", e.TableName, "entityID", e.EntityID, common.ErrAttr(err))
			}
		}

		dbBatch = append(dbBatch, event)
	}

	return al.storeAuditLogEvents(ctx, dbBatch)
}

func (al *AuditLog) storeAuditLogEvents(ctx context.Context, batch []*dbgen.CreateAuditLogsParams) error {
	if len(batch) == 0 {
		return nil
	}

	count, err := al.querier.CreateAuditLogs(ctx, batch)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to store audit log events", "count", len(batch), common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Stored audit log events", "count", len(batch), "rows", count)

	return nil
}

func (al *AuditLog) RecordEvents(ctx context.Context, events []*common.AuditLogEvent) {
	for _, event := range events {
		al.RecordEvent(ctx, event)
	}
}

func (al *AuditLog) RecordEvent(ctx context.Context, event *common.AuditLogEvent) {
	if event == nil {
		slog.ErrorContext(ctx, "Discarding nil audit log event")
		return
	}

	if (event.OldValue == nil) && (event.NewValue == nil) &&
		(event.Action != common.AuditLogActionAccess) &&
		(event.Action != common.AuditLogActionLogin) &&
		(event.Action != common.AuditLogActionLogout) {
		slog.WarnContext(ctx, "Audit log event has no payload", "table", event.TableName, "entityID", event.EntityID, "action", event.Action.String())
	}

	if sid, ok := ctx.Value(common.SessionIDContextKey).(string); ok && (len(sid) > 0) {
		event.SessionID = sid
	}

	event.Timestamp = time.Now().UTC()

	slog.DebugContext(ctx, "Queueing audit log event", "action", event.Action.String(), "table", event.TableName, "userID", event.UserID)
	al.persistChan <- event
}

type DiscardAuditLog struct{}

var _ common.AuditLog = (*DiscardAuditLog)(nil)

func (dal *DiscardAuditLog) RecordEvents(ctx context.Context, events []*common.AuditLogEvent) {
	for _, event := range events {
		dal.RecordEvent(ctx, event)
	}
}

func (dal *DiscardAuditLog) RecordEvent(ctx context.Context, event *common.AuditLogEvent) {
	slog.WarnContext(ctx, "Discarded audit log event", "table", event.TableName, "entityID", event.EntityID, "action", event.Action)
}

func ParseAuditLogPayloads[T any](ctx context.Context, log *dbgen.AuditLog) (*T, *T, error) {
	var tOld *T
	if len(log.OldValue) > 0 {
		tOld = new(T)
		if err := json.Unmarshal(log.OldValue, tOld); err != nil {
			slog.ErrorContext(ctx, "Failed to parse audit log old value", "table", log.EntityTable, "action", log.Action,
				"length", len(log.OldValue), common.ErrAttr(err))
			return nil, nil, err
		}
	}

	var tNew *T
	if len(log.NewValue) > 0 {
		tNew = new(T)
		if err := json.Unmarshal(log.NewValue, tNew); err != nil {
			slog.ErrorContext(ctx, "Failed to parse audit log new value", "table", log.EntityTable, "action", log.Action,
				"length", len(log.OldValue), common.ErrAttr(err))
			return nil, nil, err
		}
	}

	return tOld, tNew, nil
}

type AuditLogUser struct {
	Name                   string `json:"name,omitempty"`
	Email                  string `json:"email,omitempty"`
	SubscriptionID         int32  `json:"subscription_id,omitempty"`
	SubscriptionSource     string `json:"subscription_source,omitempty"`
	ExternalProductID      string `json:"external_product_id,omitempty"`
	ExternalSubscriptionID string `json:"external_subscription_id,omitempty"`
	ExternalPriceID        string `json:"external_price_id,omitempty"`
}

func newAuditLogUser(user *dbgen.User, subscription *dbgen.Subscription) *AuditLogUser {
	event := &AuditLogUser{
		Name:           user.Name,
		Email:          user.Email,
		SubscriptionID: user.SubscriptionID.Int32,
	}

	if subscription != nil {
		event.SubscriptionID = subscription.ID
		event.SubscriptionSource = string(subscription.Source)
		event.ExternalProductID = subscription.ExternalProductID
		event.ExternalSubscriptionID = subscription.ExternalSubscriptionID.String
		event.ExternalPriceID = subscription.ExternalPriceID
	}

	return event
}

func newUserAuditLogEvent(user *dbgen.User, subscription *dbgen.Subscription, action common.AuditLogAction) *common.AuditLogEvent {
	event := &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    action,
		EntityID:  int64(user.ID),
		TableName: TableNameUsers,
		OldValue:  nil,
		NewValue:  nil,
	}

	switch action {
	case common.AuditLogActionCreate, common.AuditLogActionRecover:
		event.NewValue = newAuditLogUser(user, subscription)
	case common.AuditLogActionDelete, common.AuditLogActionSoftDelete:
		event.OldValue = newAuditLogUser(user, subscription)
	}

	return event
}

func newUpdateUserSubscriptionEvent(user *dbgen.User, oldSubscription, newSubscription *dbgen.Subscription) *common.AuditLogEvent {
	if (oldSubscription == nil) && (newSubscription == nil) {
		return nil
	}

	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionUpdate,
		EntityID:  int64(user.ID),
		TableName: TableNameUsers,
		OldValue:  newAuditLogUser(user, oldSubscription),
		NewValue:  newAuditLogUser(user, newSubscription),
	}
}

func newUpdateUserAuditLogEvent(oldUser *dbgen.User, newUser *dbgen.User) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    oldUser.ID,
		Action:    common.AuditLogActionUpdate,
		EntityID:  int64(oldUser.ID),
		TableName: TableNameUsers,
		OldValue:  newAuditLogUser(oldUser, nil),
		NewValue:  newAuditLogUser(newUser, nil),
	}
}

type AuditLogOrg struct {
	ID   int32  `json:"id"`
	Name string `json:"name"`
}

func NewAuditLogOrg(org *dbgen.Organization) *AuditLogOrg {
	return &AuditLogOrg{
		ID:   org.ID,
		Name: org.Name,
	}
}

func newOrgAuditLogEvent(userID int32, org *dbgen.Organization, action common.AuditLogAction) *common.AuditLogEvent {
	event := &common.AuditLogEvent{
		UserID:    userID,
		Action:    action,
		EntityID:  int64(org.ID),
		TableName: TableNameOrgs,
		OldValue:  nil,
		NewValue:  nil,
	}

	switch action {
	case common.AuditLogActionCreate, common.AuditLogActionRecover:
		event.NewValue = NewAuditLogOrg(org)
	case common.AuditLogActionDelete, common.AuditLogActionSoftDelete:
		event.OldValue = NewAuditLogOrg(org)
	}

	return event
}

type AuditLogProperty struct {
	Name                string `json:"name,omitempty"`
	OrgID               int32  `json:"org_id,omitempty"`
	OrgName             string `json:"org_name,omitempty"`
	OrgOwnerID          int32  `json:"org_owner_id,omitempty"`
	CreatorID           int32  `json:"creator_id,omitempty"`
	Domain              string `json:"domain,omitempty"`
	Level               int16  `json:"level,omitempty"`
	Growth              string `json:"growth,omitempty"`
	ValidityIntervalSec int    `json:"validity_interval_s,omitempty"`
	MaxReplayCount      int32  `json:"max_replay_count,omitempty"`
	AllowSubdomains     bool   `json:"allow_subdomains,omitempty"`
	AllowLocalhost      bool   `json:"allow_localhost,omitempty"`
}

func newAuditLogProperty(property *dbgen.Property, org *dbgen.Organization) *AuditLogProperty {
	if property == nil {
		return nil
	}

	event := &AuditLogProperty{
		Name:                property.Name,
		OrgID:               property.OrgID.Int32,
		OrgOwnerID:          property.OrgOwnerID.Int32,
		CreatorID:           property.CreatorID.Int32,
		Domain:              property.Domain,
		Level:               property.Level.Int16,
		Growth:              string(property.Growth),
		ValidityIntervalSec: int(property.ValidityInterval.Seconds()),
		MaxReplayCount:      property.MaxReplayCount,
		AllowSubdomains:     property.AllowSubdomains,
		AllowLocalhost:      property.AllowLocalhost,
	}

	if org != nil {
		event.OrgName = org.Name
	}

	return event
}

func newCreatePropertyAuditLogEvent(property *dbgen.Property, org *dbgen.Organization) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    property.CreatorID.Int32,
		Action:    common.AuditLogActionCreate,
		EntityID:  int64(property.ID),
		TableName: TableNameProperties,
		OldValue:  nil,
		NewValue:  newAuditLogProperty(property, org),
	}
}

func newUpdatePropertyAuditLogEvent(oldProperty, newProperty *dbgen.Property, org *dbgen.Organization) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    oldProperty.CreatorID.Int32,
		Action:    common.AuditLogActionUpdate,
		EntityID:  int64(oldProperty.ID),
		TableName: TableNameProperties,
		OldValue:  newAuditLogProperty(oldProperty, org),
		NewValue:  newAuditLogProperty(newProperty, org),
	}
}

func newDeletePropertyAuditLogEvent(property *dbgen.Property, org *dbgen.Organization, user *dbgen.User) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionSoftDelete,
		EntityID:  int64(property.ID),
		TableName: TableNameProperties,
		OldValue:  newAuditLogProperty(property, org),
		NewValue:  nil,
	}
}

func newUpdateOrgAuditLogEvent(user *dbgen.User, org *dbgen.Organization, oldName string) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionUpdate,
		EntityID:  int64(org.ID),
		TableName: TableNameOrgs,
		OldValue:  &AuditLogOrg{Name: oldName},
		NewValue:  &AuditLogOrg{Name: org.Name},
	}
}

type AuditLogOrgUser struct {
	OrgName string `json:"org_name,omitempty"`
	UserID  int32  `json:"user_id,omitempty"`
	Email   string `json:"email,omitempty"`
	Level   string `json:"level,omitempty"`
}

func newAuditLogOrgUser(user *dbgen.User, orgName string, level string) *AuditLogOrgUser {
	return &AuditLogOrgUser{
		OrgName: orgName,
		UserID:  user.ID,
		Email:   user.Email,
		Level:   level,
	}
}

func newOrgInviteAuditLogEvent(user *dbgen.User, org *dbgen.Organization, inviteUser *dbgen.User) *common.AuditLogEvent {
	// this one is bit tough (we kind of need 1 more "entity id" field), but our logic is that for audit log we record
	// "who did what with what" so org owner invited to org <user>
	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionCreate,
		EntityID:  int64(org.ID),
		TableName: TableNameOrgUsers,
		OldValue:  nil,
		NewValue:  newAuditLogOrgUser(inviteUser, org.Name, string(dbgen.AccessLevelInvited)),
	}
}

func newOrgMemberDeleteAuditLogEvent(user *dbgen.User, org *dbgen.Organization, userID int32, email string) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionDelete,
		EntityID:  int64(org.ID),
		TableName: TableNameOrgUsers,
		OldValue:  &AuditLogOrgUser{OrgName: org.Name, UserID: userID, Email: email},
		NewValue:  nil,
	}
}

func newOrgMemberAuditLogEvent(orgID int32, orgName string, user *dbgen.User, action common.AuditLogAction, level string) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    action,
		EntityID:  int64(orgID),
		TableName: TableNameOrgUsers,
		OldValue:  nil,
		NewValue:  newAuditLogOrgUser(user, orgName, level),
	}
}

type AuditLogAPIKey struct {
	Name              string          `json:"name,omitempty"`
	ExternalID        string          `json:"external_id,omitempty"`
	Enabled           bool            `json:"enabled,omitempty"`
	RequestsPerSecond float64         `json:"requests_per_second,omitempty"`
	RequestsBurst     int32           `json:"requests_burst,omitempty"`
	ExpiresAt         common.JSONTime `json:"expires_at,omitempty"`
	Notes             string          `json:"notes,omitempty"`
	Period            time.Duration   `json:"period,omitempty"`
}

func newAuditLogAPIKey(key *dbgen.APIKey) *AuditLogAPIKey {
	if key == nil {
		return nil
	}

	return &AuditLogAPIKey{
		Name:              key.Name,
		ExternalID:        UUIDToSecret(key.ExternalID),
		Enabled:           key.Enabled.Bool,
		RequestsPerSecond: key.RequestsPerSecond,
		RequestsBurst:     key.RequestsBurst,
		ExpiresAt:         common.JSONTime(key.ExpiresAt.Time),
		Notes:             key.Notes.String,
		Period:            key.Period,
	}
}

func newAPIKeyAuditLogEvent(user *dbgen.User, apiKey *dbgen.APIKey, action common.AuditLogAction) *common.AuditLogEvent {
	event := &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    action,
		EntityID:  int64(apiKey.ID),
		TableName: TableNameAPIKeys,
		OldValue:  nil,
		NewValue:  nil,
	}

	switch action {
	case common.AuditLogActionCreate, common.AuditLogActionRecover:
		event.NewValue = newAuditLogAPIKey(apiKey)
	case common.AuditLogActionDelete, common.AuditLogActionSoftDelete:
		event.OldValue = newAuditLogAPIKey(apiKey)
	}

	return event
}

func newUpdateAPIKeyAuditLogEvent(user *dbgen.User, oldAPIKey, newAPIKey *dbgen.APIKey) *common.AuditLogEvent {
	if (oldAPIKey == nil) && (newAPIKey == nil) {
		return nil
	}

	entityKey := oldAPIKey
	if entityKey == nil {
		entityKey = newAPIKey
	}

	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionUpdate,
		EntityID:  int64(entityKey.ID),
		TableName: TableNameAPIKeys,
		OldValue:  newAuditLogAPIKey(oldAPIKey),
		NewValue:  newAuditLogAPIKey(newAPIKey),
	}
}

func newMovePropertyAuditLogEvent(user *dbgen.User, property *dbgen.Property, oldOrgID, newOrgID int32) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionUpdate,
		EntityID:  int64(property.ID),
		TableName: TableNameProperties,
		OldValue:  &AuditLogProperty{OrgID: oldOrgID},
		NewValue:  &AuditLogProperty{OrgID: newOrgID},
	}
}

type AuditLogAccess struct {
	View       string `json:"view,omitempty"`
	EntityName string `json:"name,omitempty"`
}
