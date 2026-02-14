package config

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func AsInt(item common.ConfigItem, fallback int) int {
	s := item.Value()
	if len(s) == 0 {
		return fallback
	}

	if i, err := strconv.Atoi(s); err != nil {
		return fallback
	} else {
		return i
	}
}

func AsBool(item common.ConfigItem) bool {
	return common.EnvToBool(item.Value())
}

func splitHostPort(s string) (domain string, port string, err error) {
	if len(s) == 0 {
		return
	}

	domain, port, err = net.SplitHostPort(s)
	if err != nil {
		lastColonIndex := strings.LastIndex(s, ":")
		// no port, "s" is the full domain
		if lastColonIndex == -1 {
			return s, "", nil
		}

		// no port, but has weird format
		if lastColonIndex == len(s)-1 {
			return "", "", err
		}

		// suffix has to be the port only
		suffix := s[lastColonIndex+1:]

		anyError := false
		for _, ch := range suffix {
			if !unicode.IsDigit(ch) {
				anyError = true
				break
			}
		}

		if anyError {
			return "", "", err
		}

		return s[:lastColonIndex], suffix, nil
	}

	return
}

type urlConfig struct {
	baseURL string
	domain  string
	path    string
}

func (uc *urlConfig) Domain() string {
	return uc.domain
}

func (uc *urlConfig) URL() string {
	return fmt.Sprintf("//%s", uc.baseURL)
}

func (uc *urlConfig) Path() string {
	return uc.path
}

func AsURL(ctx context.Context, item common.ConfigItem) *urlConfig {
	baseURL := strings.TrimRight(item.Value(), "/")

	// Split host:port from path
	// e.g. "localhost:8080/portal" → hostPort="localhost:8080", path="/portal"
	hostPort := baseURL
	path := ""
	if idx := strings.Index(baseURL, "/"); idx >= 0 {
		hostPort = baseURL[:idx]
		path = strings.TrimRight(baseURL[idx:], "/") // e.g. "/portal"
	}

	domain, _, err := splitHostPort(hostPort)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse domain from baseURL", common.ErrAttr(err))
	}

	return &urlConfig{baseURL: baseURL, domain: domain, path: path}
}
