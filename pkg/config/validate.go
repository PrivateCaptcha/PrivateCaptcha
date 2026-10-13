package config

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/http/httpguts"
)

var errInvalidConfig = errors.New("invalid configuration")

func requiredConfigKey(key common.ConfigKey) bool {
	switch key {
	case common.PortalBaseURLKey,
		common.APIBaseURLKey,
		common.CDNBaseURLKey,
		common.AdminEmailKey,
		common.EmailFromKey,
		common.RateLimitHeaderKey,
		common.UserFingerprintIVKey,
		common.XSRFKeyKey,
		common.APISaltKey,
		common.IDHasherSaltKey,
		common.SmtpEndpointKey,
		common.SmtpUsernameKey,
		common.SmtpPasswordKey:
		return true
	default:
		return false
	}
}

// Validate checks common config values without starting services.
func Validate(ctx context.Context, cfg common.ConfigStore) error {
	var validationErr error
	for key := common.ConfigKey(0); key < common.COMMON_CONFIG_KEYS_COUNT; key++ {
		item := cfg.Get(key)
		if requiredConfigKey(key) && strings.TrimSpace(item.Value()) == "" {
			slog.ErrorContext(ctx, "Required configuration value is empty", "key", configKeyToEnvName[key])
			validationErr = errInvalidConfig
			continue
		}
		if item.Value() == "" {
			continue
		}
		if err := validateItem(item); err != nil {
			slog.ErrorContext(ctx, "Invalid configuration value", "key", configKeyToEnvName[key], common.ErrAttr(err))
			validationErr = errInvalidConfig
		}
	}
	if err := validateEdgeKeyPair(cfg); err != nil {
		slog.ErrorContext(ctx, "Invalid edge token signing key pair",
			"privateKeyConfig", configKeyToEnvName[common.EdgeTokenSigningPrivateKeyKey],
			"publicKeyConfig", configKeyToEnvName[common.EdgeTokenSigningPublicKeyKey], common.ErrAttr(err))
		validationErr = errInvalidConfig
	}
	return validationErr
}

func validateItem(item common.ConfigItem) error {
	value := item.Value()
	switch item.Key() {
	case common.VerboseKey, common.MaintenanceModeKey, common.RegistrationAllowedKey,
		common.ClickHouseOptionalKey, common.GradualDataCleanupKey:
		if AsBool(item) {
			return nil
		}
		switch value {
		case "0", "N", "n", "no", "false", "NO", "FALSE":
			return nil
		default:
			return errors.New("invalid boolean")
		}
	case common.HealthCheckIntervalKey:
		return validatePositiveInt(item, int(math.MaxInt64/int64(time.Second)))
	case common.MaintenanceJobConcurrencyKey:
		return validatePositiveInt(item, math.MaxInt)
	case common.EnterpriseAuditLogDaysKey:
		return validatePositiveInt(item, int(math.MaxInt64/int64(24*time.Hour)))
	case common.RateLimitBurstKey:
		if _, err := strconv.ParseUint(value, 10, 32); err != nil {
			return errors.New("invalid unsigned 32-bit integer")
		}
	case common.RateLimitRateKey:
		rate, err := strconv.ParseFloat(value, 64)
		interval := float64(time.Second) / rate
		if err != nil || math.IsNaN(rate) || math.IsInf(rate, 0) ||
			interval >= float64(math.MaxInt64) || leakybucket.Interval(value, 0) <= 0 {
			return errors.New("rate must produce a positive, representable leak interval")
		}
	case common.Argon2IDMemoryBudgetKey:
		_, err := parseArgon2IDMemoryBudget(value, int64(puzzle.Argon2IDMemoryKiB))
		return err
	case common.PortKey:
		if _, err := net.LookupPort("tcp", value); err != nil {
			return errors.New("invalid TCP port")
		}
	case common.ClickHousePortKey:
		return validatePositiveInt(item, 65535)
	case common.PortalBaseURLKey, common.APIBaseURLKey, common.CDNBaseURLKey:
		return validateBaseURL(item)
	case common.AdminEmailKey, common.EmailFromKey, common.ReplyToEmailKey:
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value || strings.ContainsAny(value, "\r\n") {
			return errors.New("invalid email address")
		}
	case common.RateLimitHeaderKey, common.CountryCodeHeaderKey, common.FingerprintHeaderKey:
		if !httpguts.ValidHeaderFieldName(value) {
			return errors.New("invalid HTTP header name")
		}
	case common.UserFingerprintIVKey:
		key, err := hex.DecodeString(value)
		if err != nil || len(key) > 64 {
			return errors.New("fingerprint key must be hex-encoded and at most 64 bytes")
		}
	case common.PostgresKey:
		if _, err := pgxpool.ParseConfig(value); err != nil {
			return errors.New("invalid Postgres connection configuration")
		}
	case common.PostgresHostKey:
		if strings.HasPrefix(value, "/") && !strings.ContainsRune(value, 0) {
			return nil
		}
		return ValidateHost(value)
	case common.HostKey:
		return ValidateHost(value)
	case common.ClickHouseHostKey:
		return validateAddress(value + ":9000")
	case common.LocalAddressKey:
		return validateAddress(value)
	case common.SmtpEndpointKey:
		endpoint, err := url.Parse(value)
		if err != nil || (endpoint.Scheme != "smtp" && endpoint.Scheme != "smtps") ||
			endpoint.Hostname() == "" || endpoint.User != nil || ValidateHost(endpoint.Hostname()) != nil {
			return errors.New("invalid SMTP URL; expected smtp://host[:port] or smtps://host[:port]")
		}
		if port := endpoint.Port(); port != "" {
			return validatePositiveInt(NewStaticValue(item.Key(), port), 65535)
		}
	case common.EdgeTokenSigningPrivateKeyKey:
		_, err := parseEdgePrivateKey(value)
		return err
	case common.EdgeTokenSigningPublicKeyKey, common.EdgeTokenSecondaryPublicKeyKey:
		_, err := parseEdgePublicKey(value)
		return err
	case common.APISaltKey:
		_ = puzzle.NewSalt([]byte(value))
	case common.IDHasherSaltKey:
		hasher := common.NewIDHasher(item)
		if _, err := hasher.Decrypt64(hasher.Encrypt64(1)); err != nil {
			return errors.New("invalid ID hash salt")
		}
	case common.CacheDirKey:
		if strings.ContainsRune(value, 0) {
			return errors.New("cache directory contains a NUL byte")
		}
	case common.StageKey, common.PostgresDBKey, common.PostgresUserKey,
		common.PostgresPasswordKey, common.PostgresAdminKey, common.PostgresAdminPasswordKey,
		common.ClickHouseDBKey, common.ClickHouseUserKey, common.ClickHousePasswordKey,
		common.ClickHouseAdminKey, common.ClickHouseAdminPasswordKey,
		common.SmtpUsernameKey, common.SmtpPasswordKey, common.XSRFKeyKey,
		common.EnterpriseLicenseKeyKey, common.CommunityLicenseKeyKey, common.LocalAPIKeyKey,
		common.WidgetNoticeKey:
		// These values are used as opaque strings at runtime.
		return nil
	default:
		return errors.New("config key has no validator")
	}
	return nil
}

func parseEdgePrivateKey(value string) (*ecdsa.PrivateKey, error) {
	key, err := jwt.ParseECPrivateKeyFromPEM([]byte(value))
	if err != nil {
		return nil, errors.New("invalid edge token private key PEM")
	}
	if key.Curve.Params().Name != "P-256" {
		return nil, errors.New("edge token keys must be P-256")
	}
	return key, nil
}

func parseEdgePublicKey(value string) (*ecdsa.PublicKey, error) {
	key, err := jwt.ParseECPublicKeyFromPEM([]byte(value))
	if err != nil {
		return nil, errors.New("invalid edge token public key PEM")
	}
	if key.Curve.Params().Name != "P-256" {
		return nil, errors.New("edge token keys must be P-256")
	}
	return key, nil
}

func validateEdgeKeyPair(cfg common.ConfigStore) error {
	privateValue := cfg.Get(common.EdgeTokenSigningPrivateKeyKey).Value()
	publicValue := cfg.Get(common.EdgeTokenSigningPublicKeyKey).Value()
	secondaryValue := cfg.Get(common.EdgeTokenSecondaryPublicKeyKey).Value()
	if privateValue == "" && publicValue == "" && secondaryValue == "" {
		return nil
	}
	if privateValue == "" || publicValue == "" {
		return errors.New("edge token signing keys must be configured together")
	}
	private, privateErr := parseEdgePrivateKey(privateValue)
	public, publicErr := parseEdgePublicKey(publicValue)
	if privateErr != nil || publicErr != nil {
		return nil // Individual key errors are reported by validateItem.
	}
	if !private.PublicKey.Equal(public) {
		return errors.New("edge token public key does not match private key")
	}
	return nil
}

func ValidateHost(value string) error {
	if net.ParseIP(value) != nil {
		return nil
	}
	if strings.ContainsAny(value, ":/[]?#@\\ \t\r\n\x00") {
		return errors.New("invalid host")
	}
	if _, err := url.Parse("//" + value); err != nil {
		return errors.New("invalid host")
	}
	return nil
}

func validateAddress(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil || port == "" || ValidateHost(host) != nil {
		return errors.New("invalid TCP address; expected host:port")
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return errors.New("invalid TCP port")
	}
	return nil
}

func validateBaseURL(item common.ConfigItem) error {
	parsed, parseErr := url.Parse("//" + strings.TrimRight(item.Value(), "/"))
	if parseErr != nil || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		strings.HasSuffix(parsed.Host, ":") || ValidateHost(parsed.Hostname()) != nil {
		return errors.New("invalid base URL; expected host[:port][/path] without a scheme")
	}
	if _, _, err := splitHostPort(parsed.Host); err != nil {
		return errors.New("invalid base URL host")
	}
	if port := parsed.Port(); port != "" {
		if err := validatePositiveInt(NewStaticValue(item.Key(), port), 65535); err != nil {
			return err
		}
	}
	return nil
}

func ValidateInt(raw string, minimum, maximum int) error {
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return fmt.Errorf("integer must be between %d and %d", minimum, maximum)
	}
	return nil
}

func validatePositiveInt(item common.ConfigItem, maximum int) error {
	return ValidateInt(item.Value(), 1, maximum)
}
