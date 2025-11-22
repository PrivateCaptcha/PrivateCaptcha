package common

import "time"

type AuditLogAction int

const (
	AuditLogActionUnknown AuditLogAction = iota
	AuditLogActionCreate
	AuditLogActionUpdate
	AuditLogActionDelete
	AuditLogActionSoftDelete
	AuditLogActionRecover
	AuditLogActionLogin
	AuditLogActionLogout
	AuditLogActionAccess
	// Add new fields _above_
	AUDIT_LOG_ACTIONS_COUNT
)

func (ala AuditLogAction) String() string {
	switch ala {
	case AuditLogActionCreate:
		return "create"
	case AuditLogActionUpdate:
		return "update"
	case AuditLogActionDelete:
		return "delete"
	case AuditLogActionSoftDelete:
		return "softdelte"
	case AuditLogActionRecover:
		return "recover"
	case AuditLogActionLogin:
		return "login"
	case AuditLogActionLogout:
		return "logout"
	case AuditLogActionAccess:
		return "access"
	default:
		return "unknown"
	}
}

// NOTE: alternative design could have been an interface. It will yield a smaller memory footprint for DTOs inside the
// channel/buffers of AuditLog in DB. However, current "flat" struct is much more convenient and has smaller total loc.
// Also the total memory benefit is not projected to be so huge because these are relatively rare one-off events.
type AuditLogEvent struct {
	UserID    int32
	Action    AuditLogAction
	EntityID  int64
	TableName string
	SessionID string
	OldValue  interface{}
	NewValue  interface{}
	Timestamp time.Time
}
