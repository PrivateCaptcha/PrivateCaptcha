package common

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

func RelURL(prefix, url string) string {
	url = strings.TrimPrefix(url, "/")
	p := strings.Trim(prefix, "/")
	if len(p) == 0 {
		return "/" + url
	}
	return "/" + p + "/" + url
}

func OrgNameFromName(name string) string {
	parts := strings.FieldsFunc(name, func(c rune) bool {
		return c == ' '
	})
	for i, p := range parts {
		parts[i] = strings.ToLower(p)
	}

	orgName := strings.Join(parts, "-")
	return orgName
}

func MaskEmail(email string, mask rune) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return email
	}

	username := parts[0]
	length := len(username)

	var keep int
	switch length {
	case 0, 1:
		keep = length
	case 2, 3:
		keep = 1
	case 4, 5:
		keep = 2
	case 6, 7:
		keep = 3
	case 8, 9:
		keep = 4
	default:
		keep = 5
	}

	prefix := username[:keep]
	suffix := ""

	n := length - keep
	if n > 5 {
		n = 5
		suffix = ".."
	}

	xxx := strings.Repeat(string(mask), n)

	return prefix + xxx + suffix + "@" + parts[1]
}

func SendJSONResponse(ctx context.Context, w http.ResponseWriter, data interface{}, headers map[string]string) {
	response, err := json.Marshal(data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialise response", ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.Header().Set(HeaderContentLength, strconv.Itoa(len(response)))
	for key, value := range headers {
		w.Header().Set(key, value)
	}

	n, err := w.Write(response)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to send response", ErrAttr(err))
	} else {
		slog.DebugContext(ctx, "Sent response", "serialized", len(response), "sent", n)
	}
}

func ParseBoolean(value string) bool {
	switch value {
	case "1", "Y", "y", "yes", "Yes", "true":
		return true
	default:
		return false
	}
}
