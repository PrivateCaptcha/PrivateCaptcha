package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func SendJSONResponse(ctx context.Context, w http.ResponseWriter, data interface{}, headers map[string]string) {
	response, err := json.Marshal(data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialise response", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set(common.HeaderContentType, common.ContentTypeJSON)
	w.Header().Set(common.HeaderContentLength, strconv.Itoa(len(response)))
	for key, value := range headers {
		w.Header().Set(key, value)
	}

	n, err := w.Write(response)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to send response", common.ErrAttr(err))
	} else {
		slog.DebugContext(ctx, "Sent response", "serialized", len(response), "sent", n)
	}
}
