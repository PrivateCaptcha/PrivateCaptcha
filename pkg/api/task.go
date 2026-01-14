//go:build enterprise

package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

func (s *Server) getAsyncTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _, err := s.requestUser(ctx, true /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	id, err := common.StrPathArg(r, common.ParamID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse request ID from URL", common.ErrAttr(err))
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	uuid := db.UUIDFromString(id)
	if !uuid.Valid {
		slog.WarnContext(ctx, "Failed to parse id arg from URL", "id", id)
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	task, err := s.BusinessDB.Impl().RetrieveAsyncTask(ctx, uuid, user)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	response := &apiAsyncTaskResultOutput{ID: id}

	if task.ProcessedAt.Valid {
		response.Finished = true

		var output interface{}
		if err := json.Unmarshal(task.Output, &output); err == nil {
			response.Result = output
		} else {
			slog.ErrorContext(ctx, "Failed to unmarshal async request outputs", common.ErrAttr(err))
			response.Result = task.Output
		}
	}

	s.sendAPISuccessResponse(ctx, response, w)
}
