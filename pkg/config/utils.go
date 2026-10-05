package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strconv"
	"strings"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	defaultArgon2IDMemoryBudgetMiB = int64(256)
	kiBPerMiB                      = int64(1024)
)

var (
	errBareIPURLLiteral = errors.New("bare IPv6 literal must be bracketed in a URL")
)

// Argon2IDMemoryBudgetKiB validates the process-wide capacity from a config item.
func Argon2IDMemoryBudgetKiB(ctx context.Context, item common.ConfigItem, minimumKiB int64) int64 {
	if item == nil {
		return 0
	}
	value := item.Value()
	if value == "" {
		return 0
	}
	budgetMiB, err := strconv.ParseInt(value, 10, 64)
	if err == nil && budgetMiB == 0 {
		return 0
	}
	reason := ""

	switch {
	case err != nil:
		reason = "value is not a valid integer"
	case budgetMiB < 0:
		reason = "value must be positive"
	case budgetMiB > math.MaxInt64/kiBPerMiB:
		reason = "value overflows the semaphore capacity"
	case budgetMiB*kiBPerMiB < minimumKiB:
		reason = "value is below the selected profile minimum"
	default:
		return budgetMiB * kiBPerMiB
	}

	slog.WarnContext(ctx, "Invalid Argon2id memory budget; using fallback",
		"environment", "PC_ARGON2_MEMORY_BUDGET_MIB",
		"value", value,
		"reason", reason,
		"minimumKiB", minimumKiB,
		"fallbackMiB", defaultArgon2IDMemoryBudgetMiB)
	return defaultArgon2IDMemoryBudgetMiB * kiBPerMiB
}

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
		// bracketed IPv6 without a port, e.g. "[::1]" — valid per RFC 3986 §3.2.2.
		// Strip the brackets so the resulting ServeMux pattern ("GET ::1/portal/") matches
		// the form Go's stripHostPort produces from "Host: [::1]:8080" (which strips both
		// port and brackets via net.SplitHostPort). The bracket-preserving form "[::1]"
		// would only match "Host: [::1]:8080" if stripHostPort left brackets intact — it
		// does not — so the unbracketed form is the one consistent with the existing
		// success branch "[::1]:8080" -> "::1".
		if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
			return s[1 : len(s)-1], "", nil
		}
		// bare IPv6 without a port, e.g. "::1" — invalid URL host; flag it explicitly
		// so AsURL's existing slog.ErrorContext reports the misconfiguration.
		if strings.Count(s, ":") >= 2 && net.ParseIP(s) != nil {
			return "", "", errBareIPURLLiteral
		}

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
	baseURL  string
	domain   string
	hostPort string
	path     string
}

func (uc *urlConfig) Domain() string {
	return uc.domain
}

func (uc *urlConfig) HostPort() string {
	return uc.hostPort
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

	return &urlConfig{baseURL: baseURL, domain: domain, hostPort: hostPort, path: path}
}
