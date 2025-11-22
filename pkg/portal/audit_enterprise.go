//go:build enterprise

package portal

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func auditLogsDaysFromParam(ctx context.Context, param string) int {
	i, err := strconv.Atoi(param)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert days", "value", param, common.ErrAttr(err))
		return 14
	}

	switch i {
	case 14, 30, 90, 180, 365:
		return i
	default:
		return 14
	}
}

func maxAuditLogsForDays(days int) int {
	return days * 100
}

func (s *Server) getAuditLogEvents(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	days := auditLogsDaysFromParam(ctx, r.URL.Query().Get(common.ParamDays))

	pageParam := r.URL.Query().Get(common.ParamPage)
	page := 0
	if len(pageParam) > 0 {
		if page, err = strconv.Atoi(pageParam); err != nil {
			slog.ErrorContext(ctx, "Failed to convert page parameter", "page", pageParam, common.ErrAttr(err))
			page = 0
		}
	}

	renderCtx, err := s.CreateAuditLogsContext(ctx, user, days, page)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model: renderCtx,
		View:  auditLogsEventsTemplate,
	}, nil
}

func (s *Server) exportAuditLogsCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get session user for CSV export", common.ErrAttr(err))
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	days := auditLogsDaysFromParam(ctx, r.URL.Query().Get(common.ParamDays))

	logs, err := s.retrieveAuditLogs(ctx, user, days, days*1000 /*max logs*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve audit logs", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	// audit logs for UI are sorted from the newest, but in CSV we want to see from the oldest
	slices.Reverse(logs)

	// Set headers for CSV download
	after := time.Now().AddDate(0 /*years*/, 0 /*months*/, -days)
	filename := fmt.Sprintf("private-captcha-audit-logs-from-%s.csv", after.Format(time.DateOnly))
	w.Header().Set(common.HeaderContentType, common.ContentTypeCSV)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV header
	header := []string{"id", "user_id", "action", "entity_id", "entity_table", "old_value", "new_value", "created_at"}
	if err := writer.Write(header); err != nil {
		slog.ErrorContext(ctx, "Failed to write CSV header", common.ErrAttr(err))
		return
	}

	// Write data rows
	for i, userLog := range logs {
		log := &userLog.AuditLog
		row := []string{
			s.IDHasher.Encrypt64(log.ID),
			s.IDHasher.Encrypt(int(log.UserID.Int32)),
			string(log.Action),
			s.IDHasher.Encrypt64(log.EntityID.Int64),
			log.EntityTable,
			string(log.OldValue),
			string(log.NewValue),
			log.CreatedAt.Time.Format(time.RFC3339),
		}

		if err := writer.Write(row); err != nil {
			slog.ErrorContext(ctx, "Failed to write CSV row", "index", i, "auditLogID", log.ID, common.ErrAttr(err))
			return
		}
	}

	slog.InfoContext(ctx, "Successfully exported audit logs to CSV", "userID", user.ID, "days", days, "count", len(logs))
}

func (s *Server) CreateAuditLogsContext(ctx context.Context, user *dbgen.User, days int, page int) (*mainAuditLogsRenderContext, error) {
	slog.DebugContext(ctx, "Creating audit logs context", "userID", user.ID, "days", days, "page", page)
	maxLogs := maxAuditLogsForDays(days)

	allLogs, err := s.retrieveAuditLogs(ctx, user, days, maxLogs)
	if err != nil {
		return nil, err
	}

	logs := allLogs

	if count := len(allLogs); count > 0 {
		totalPages := (count + perPageEventLogs - 1) / perPageEventLogs
		page = max(0, min(page, totalPages-1))

		start := page * perPageEventLogs
		end := min(count, start+perPageEventLogs)
		logs = allLogs[start:end]
	}

	return &mainAuditLogsRenderContext{
		CsrfRenderContext:  s.CreateCsrfContext(user),
		AlertRenderContext: AlertRenderContext{},
		AuditLogsRenderContext: AuditLogsRenderContext{
			AuditLogs: s.newUserAuditLogs(ctx, user, logs),
			Count:     len(allLogs),
			PerPage:   perPageEventLogs,
			Page:      page,
		},
		Days: days,
		From: 1 + page*perPageEventLogs,
		To:   min((page+1)*perPageEventLogs, len(allLogs)),
	}, nil
}
