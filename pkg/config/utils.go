package config

import (
	"context"
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
	defaultArgon2IDMemoryBudgetMiB = int64(512)
	kiBPerMiB                      = int64(1024)
)

// Argon2IDMemoryBudgetKiB validates the process-wide capacity from a config item.
func Argon2IDMemoryBudgetKiB(ctx context.Context, item common.ConfigItem, minimumKiB int64) int64 {
	value := item.Value()
	budgetMiB, err := strconv.ParseInt(value, 10, 64)
	reason := ""

	switch {
	case value == "":
		reason = "value is missing"
	case err != nil:
		reason = "value is not a valid integer"
	case budgetMiB <= 0:
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
