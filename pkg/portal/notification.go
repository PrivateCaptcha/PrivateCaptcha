package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func (s *Server) createSystemNotificationContext(ctx context.Context, sess *session.Session) systemNotificationContext {
	renderCtx := systemNotificationContext{}

	if notificationID, ok := sess.Get(ctx, session.KeyNotificationID).(int32); ok {
		if notification, err := s.Store.Impl().RetrieveSystemNotification(ctx, notificationID); err == nil {
			renderCtx.Notification = notification.Message
			renderCtx.NotificationID = strconv.Itoa(int(notification.ID))
		}
	}

	return renderCtx
}

func (s *Server) dismissNotification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Sessions.SessionStart(w, r)

	value := r.PathValue(common.ParamID)
	id, err := strconv.Atoi(value)
	if err == nil {
		if notificationID, ok := sess.Get(ctx, session.KeyNotificationID).(int32); ok {
			if notificationID != int32(id) {
				slog.ErrorContext(ctx, "Mismatch between notification ID in session", "session", notificationID, "param", id)
			}
		}
		if derr := sess.Delete(session.KeyNotificationID); derr != nil {
			slog.ErrorContext(ctx, "Failed to dismiss notification", "id", id, common.ErrAttr(derr))
		} else {
			slog.InfoContext(ctx, "Dismissed notification", "id", id)
		}
		w.WriteHeader(http.StatusOK)
	} else {
		logID := value
		if len(value) > 10 {
			logID = value[:10]
		}
		slog.ErrorContext(ctx, "Failed to parse notification ID", "id", logID, "length", len(value), common.ErrAttr(err))
		http.Error(w, "", http.StatusBadRequest)
	}
}
