package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func (s *Server) createSystemNotificationContext(ctx context.Context, sess *common.Session) systemNotificationContext {
	renderCtx := systemNotificationContext{}

	if notificationID, ok := sess.Get(session.KeyNotificationID).(int32); ok {
		if notification, err := s.Store.RetrieveNotification(ctx, notificationID); err == nil {
			renderCtx.Notification = notification.Message
			renderCtx.NotificationID = strconv.Itoa(int(notification.ID))
		}
	}

	return renderCtx
}

func (s *Server) dismissNotification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)

	value := r.PathValue(common.ParamID)
	id, err := strconv.Atoi(value)
	if err == nil {
		if notificationID, ok := sess.Get(session.KeyNotificationID).(int32); ok {
			if notificationID != int32(id) {
				slog.ErrorContext(ctx, "Mismatch between notification ID in session", "session", notificationID, "param", id)
			}
		}
		sess.Delete(session.KeyNotificationID)
		slog.DebugContext(r.Context(), "Dismissed notification", "id", id)
		w.WriteHeader(http.StatusOK)
	} else {
		slog.ErrorContext(r.Context(), "Failed to parse notification ID", "id", value[:10], "length", len(value), common.ErrAttr(err))
		http.Error(w, "", http.StatusBadRequest)
	}
}
