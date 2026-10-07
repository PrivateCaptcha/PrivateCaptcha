package portal

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const DeleteAccountHandlerID = "portal-delete-account"

type AsyncTaskDeleteAccount struct {
	RequesterIP string `json:"requester_ip,omitempty"`
	SessionHash string `json:"session_hash,omitempty"`
}

// HandleDeleteAccount cancels the subscription and offboards the user identified by task.UserID.
func (s *Server) HandleDeleteAccount(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
	if task == nil || !task.UserID.Valid || task.UserID.Int32 <= 0 {
		return nil, db.ErrInvalidInput
	}

	params := &AsyncTaskDeleteAccount{}
	if len(task.Input) > 0 {
		if err := json.Unmarshal(task.Input, params); err != nil {
			slog.ErrorContext(ctx, "Failed to unmarshal account deletion task input", common.ErrAttr(err))
			return nil, err
		}
	}
	if ip, err := netip.ParseAddr(params.RequesterIP); err == nil {
		ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, ip)
	}
	if hash, ok := common.ParseSessionHash(params.SessionHash); ok {
		ctx = context.WithValue(ctx, common.SessionHashContextKey, hash)
	}

	user, err := s.Store.Impl().RetrieveUser(ctx, task.UserID.Int32)
	if errors.Is(err, db.ErrSoftDeleted) {
		return nil, nil
	}
	if err != nil && !errors.Is(err, db.ErrDisabled) {
		slog.ErrorContext(ctx, "Failed to retrieve user for account deletion", "userID", task.UserID.Int32, common.ErrAttr(err))
		return nil, err
	}
	if s.AdminEmail != nil && strings.EqualFold(s.AdminEmail.Value(), user.Email) {
		slog.WarnContext(ctx, "Cannot delete admin user", "userID", user.ID, "email", user.Email)
		return nil, db.ErrPermissions
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32, true /*skip cache*/)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve a subscription", "userID", user.ID, common.ErrAttr(err))
			return nil, err
		}
		if s.PlanService.IsSubscriptionActive(subscription.Status) && subscription.ExternalSubscriptionID.Valid && !HasScheduledCancellation(subscription) {
			if err := s.PlanService.CancelSubscription(ctx, subscription.ExternalSubscriptionID.String); err != nil {
				slog.ErrorContext(ctx, "Failed to cancel external subscription", "userID", user.ID, common.ErrAttr(err))
				return nil, err
			}
		}
	}

	auditEvents, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		auditEvent, err := impl.SoftDeleteUser(ctx, user)
		if err != nil {
			return nil, err
		}
		if _, err := impl.RevokeUserSessions(ctx, user.ID); err != nil {
			return nil, err
		}
		return []*common.AuditLogEvent{auditEvent}, nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete user", "userID", user.ID, common.ErrAttr(err))
		return nil, err
	}

	job := s.Jobs.OffboardUser(user)
	common.RunOneOffJob(ctx, job, job.NewParams())
	s.Store.AuditLog().RecordEvents(ctx, auditEvents, common.AuditLogSourcePortal)

	return nil, nil
}
