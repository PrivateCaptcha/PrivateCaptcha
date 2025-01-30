package common

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jpillora/backoff"
)

func StartOfMonth() time.Time {
	tnow := time.Now().UTC()
	// NOTE: it does not correctly adjust time itself
	return tnow.AddDate(0 /*years*/, 0 /*months*/, -tnow.Day()+1)
}

func RelURL(prefix, url string) string {
	url = strings.TrimPrefix(url, "/")
	p := strings.Trim(prefix, "/")
	if len(p) == 0 {
		return "/" + url
	}
	return "/" + p + "/" + url
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

func SendJSONResponse(ctx context.Context, w http.ResponseWriter, data interface{}, headers map[string][]string) {
	response, err := json.Marshal(data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialise response", ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	wHeader := w.Header()
	wHeader.Set(HeaderContentType, ContentTypeJSON)
	for key, value := range headers {
		wHeader[key] = value
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

func ChunkedCleanup(ctx context.Context, minInterval, maxInterval time.Duration, defaultChunkSize int, deleter func(t time.Time, size int) int) {
	b := &backoff.Backoff{
		Min:    minInterval,
		Max:    maxInterval,
		Factor: 2,
		Jitter: true,
	}

	slog.DebugContext(ctx, "Starting chunked clean up", "maxInterval", maxInterval.String(), "size", defaultChunkSize)

	deleteChunk := defaultChunkSize

	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
		case <-time.After(b.Duration()):
			deleted := deleter(time.Now(), deleteChunk)
			if deleted == 0 {
				deleteChunk = defaultChunkSize
				continue
			}

			slog.DebugContext(ctx, "Deleted records", "count", deleted)

			// in case of any deletes, we want to go back to small interval first
			b.Reset()

			if deleted == deleteChunk {
				// 1.5 scaling factor
				deleteChunk += deleteChunk / 2
			}
		}
	}

	slog.DebugContext(ctx, "Finished cleaning up")
}

func ParseDomainName(input string) (string, error) {
	parsedURL, err := url.Parse(input)
	if err != nil {
		return "", err
	}

	domain := parsedURL.Host
	if domain == "" {
		domain = input
	}

	if slashIndex := strings.LastIndex(domain, "/"); slashIndex != -1 {
		domain = domain[:slashIndex]
	}

	if colonIndex := strings.LastIndex(domain, ":"); colonIndex != -1 {
		domain = domain[:colonIndex]
	}

	return domain, nil
}

func IsLocalhost(address string) bool {
	return (address == "localhost") ||
		(address == "127.0.0.1") ||
		(address == "::1") ||
		(address == "0:0:0:0:0:0:0:1")
}

func IsSubDomainOrDomain(subDomain, domain string) bool {
	if len(subDomain) == 0 || len(domain) == 0 {
		return false
	}

	if len(subDomain) < len(domain) {
		return false
	}

	if strings.HasSuffix(subDomain, domain) {
		if lenDiff := len(subDomain) - len(domain); lenDiff > 0 {
			prefix := subDomain[:lenDiff]
			return strings.HasSuffix(prefix, ".") && lenDiff > 1
		}

		return true
	}

	return false
}

func EnvToBool(value string) bool {
	switch value {
	case "1", "Y", "y", "yes", "true", "YES", "TRUE":
		return true
	default:
		return false
	}
}
