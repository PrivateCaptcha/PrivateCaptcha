package common

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"

	"maps"

	"github.com/jpillora/backoff"
	"github.com/mailru/easyjson"
)

var (
	ErrBackpressure            = errors.New("backpressure error")
	HeaderValueContentTypeJSON = []string{ContentTypeJSON}
	errEmptyDomain             = errors.New("domain name is empty")
)

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

func SendReponse(ctx context.Context, w http.ResponseWriter, response []byte, headers ...map[string][]string) {
	wHeader := w.Header()
	for _, hh := range headers {
		for key, value := range hh {
			wHeader[key] = value
		}
	}

	n, err := w.Write(response)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to send response", ErrAttr(err))
	} else {
		slog.Log(ctx, LevelTrace, "Sent response", "size", len(response), "sent", n)
	}
}

func SendJSONResponse(ctx context.Context, w http.ResponseWriter, data easyjson.Marshaler, headers ...map[string][]string) {
	response, err := easyjson.Marshal(data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialise response", ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	wHeader := w.Header()
	wHeader[HeaderContentType] = HeaderValueContentTypeJSON
	for _, hh := range headers {
		maps.Copy(wHeader, hh)
	}

	n, err := w.Write(response)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to send response", ErrAttr(err))
	} else {
		slog.Log(ctx, LevelTrace, "Sent response", "serialized", len(response), "sent", n)
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

func containsAlphabetic(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func onlyAlphabetic(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func isLowerCase(s string) bool {
	for _, r := range s {
		if !unicode.IsLower(r) {
			return false
		}
	}

	return true
}

func emailDomain(email string) string {
	atIdx := strings.LastIndex(email, "@")
	if atIdx < 0 || atIdx >= len(email)-1 {
		return ""
	}

	domain := email[atIdx+1:]
	dotIdx := strings.Index(domain, ".")
	if dotIdx > 0 {
		return domain[:dotIdx]
	}

	return domain
}

func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}

func isSkipName(p string) bool {
	switch {
	case strings.EqualFold(p, "web"):
		return true
	case strings.EqualFold(p, "admin"):
		return true
	case strings.EqualFold(p, "admins"):
		return true
	case strings.EqualFold(p, "team"):
		return true
	case strings.EqualFold(p, "it"):
		return true
	case strings.EqualFold(p, "development"):
		return true
	case strings.EqualFold(p, "informatika"):
		return true
	case strings.EqualFold(p, "captcha"):
		return true
	default:
		return false
	}
}

func shouldSkipPart(p string, domain string) bool {
	if len(p) <= 1 {
		return true
	}

	if isSkipName(p) {
		return true
	}

	if onlyAlphabetic(p) && isAllCaps(p) {
		return true
	}

	if len(domain) > 0 && strings.EqualFold(p, domain) {
		return true
	}

	return false
}

func GuessFirstName(username string, email string) string {
	parts := strings.Fields(username)
	domain := emailDomain(email)

	for _, p := range parts {
		if !containsAlphabetic(p) {
			continue
		}

		if shouldSkipPart(p, domain) {
			continue
		}

		if onlyAlphabetic(p) && isLowerCase(p) {
			runes := []rune(p)
			runes[0] = unicode.ToUpper(runes[0])
			return string(runes)
		}

		return p
	}

	return username
}

func ChunkedCleanup(ctx context.Context, minInterval, maxInterval time.Duration, defaultChunkSize int, deleter func(context.Context, time.Time, int) int) {
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
			deleted := deleter(ctx, time.Now(), deleteChunk)
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
	if len(input) == 0 {
		return "", errEmptyDomain
	}

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

func IsIPAddress(str string) bool {
	_, err := netip.ParseAddr(str)
	return err == nil
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

// RetriableError is a wrapper for errors that should be retried.
type RetriableError struct {
	err error
}

func NewRetriableError(err error) RetriableError {
	return RetriableError{err}
}

func (e RetriableError) Error() string {
	return e.err.Error()
}

func (e RetriableError) Unwrap() error {
	return e.err
}

func SafeString(s string, maxLen int) string {
	return s[:min(len(s), maxLen)]
}

// formatSuffix formats the number to one decimal place and appends the suffix.
func formatSuffix(val float64, suffix string) string {
	str := fmt.Sprintf("%.1f", val)
	str = strings.TrimSuffix(str, ".0")
	return str + suffix
}

// FormatMagnitude converts a number into a string with K, M, B, or T suffixes.
func FormatMagnitude(value float64) string {
	absVal := math.Abs(value)

	switch {
	case absVal >= 1_000_000_000_000:
		return formatSuffix(value/1_000_000_000_000, "T")
	case absVal >= 1_000_000_000:
		return formatSuffix(value/1_000_000_000, "B")
	case absVal >= 1_000_000:
		return formatSuffix(value/1_000_000, "M")
	case absVal >= 1_000:
		return formatSuffix(value/1_000, "K")
	default:
		// For numbers less than 1000, return the exact number without decimals
		return fmt.Sprintf("%.0f", value)
	}
}
