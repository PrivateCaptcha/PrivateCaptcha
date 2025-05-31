package config

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errEmptyEnvVar = errors.New("environment variable is empty")
)

type envConfigValue struct {
	key   common.ConfigKey
	value string
}

var _ common.ConfigItem = (*envConfigValue)(nil)

type ConfigMapper func(common.ConfigKey) string

func DefaultMapper(c common.ConfigKey) string {
	switch c {
	case common.APIBaseURLKey:
		return "PC_API_BASE_URL"
	case common.PortalBaseURLKey:
		return "PC_PORTAL_BASE_URL"
	case common.CDNBaseURLKey:
		return "PC_CDN_BASE_URL"
	case common.StageKey:
		return "STAGE"
	case common.VerboseKey:
		return "PC_VERBOSE"
	case common.SmtpEndpointKey:
		return "SMTP_ENDPOINT"
	case common.SmtpUsernameKey:
		return "SMTP_USERNAME"
	case common.SmtpPasswordKey:
		return "SMTP_PASSWORD"
	case common.ClickHouseHostKey:
		return "PC_CLICKHOUSE_HOST"
	case common.ClickHouseDBKey:
		return "PC_CLICKHOUSE_DB"
	case common.ClickHouseAdminKey:
		return "PC_CLICKHOUSE_ADMIN"
	case common.ClickHouseUserKey:
		return "PC_CLICKHOUSE_USER"
	case common.ClickHouseAdminPasswordKey:
		return "PC_CLICKHOUSE_ADMIN_PASSWORD"
	case common.ClickHousePasswordKey:
		return "PC_CLICKHOUSE_PASSWORD"
	case common.PostgresKey:
		return "PC_POSTGRES"
	case common.PostgresHostKey:
		return "PC_POSTGRES_HOST"
	case common.PostgresDBKey:
		return "PC_POSTGRES_DB"
	case common.PostgresUserKey:
		return "PC_POSTGRES_USER"
	case common.PostgresAdminKey:
		return "PC_POSTGRES_ADMIN"
	case common.PostgresAdminPasswordKey:
		return "PC_POSTGRES_ADMIN_PASSWORD"
	case common.PostgresPasswordKey:
		return "PC_POSTGRES_PASSWORD"
	case common.AdminEmailKey:
		return "PC_ADMIN_EMAIL"
	case common.EmailFromKey:
		return "PC_EMAIL_FROM"
	case common.LocalAddressKey:
		return "PC_LOCAL_ADDRESS"
	case common.MaintenanceModeKey:
		return "PC_MAINTENANCE_MODE"
	case common.RegistrationAllowedKey:
		return "PC_REGISTRATION_ALLOWED"
	case common.HealthCheckIntervalKey:
		return "PC_HEALTHCHECK_INTERVAL"
	case common.PuzzleLeakyBucketRateKey:
		return "PC_PUZZLE_LEAKY_BUCKET_RPS"
	case common.PuzzleLeakyBucketBurstKey:
		return "PC_PUZZLE_LEAKY_BUCKET_BURST"
	case common.DefaultLeakyBucketRateKey:
		return "PC_DEFAULT_LEAKY_BUCKET_RPS"
	case common.DefaultLeakyBucketBurstKey:
		return "PC_DEFAULT_LEAKY_BUCKET_BURST"
	case common.RateLimitHeaderKey:
		return "PC_RATE_LIMIT_HEADER"
	case common.HostKey:
		return "PC_HOST"
	case common.PortKey:
		return "PC_PORT"
	case common.UserFingerprintIVKey:
		return "PC_USER_FINGERPRINT_KEY"
	case common.APISaltKey:
		return "PC_API_SALT"
	default:
		return ""
	}
}

func (v *envConfigValue) Key() common.ConfigKey {
	return v.key
}

func (v *envConfigValue) Value() string {
	return v.value
}

func (v *envConfigValue) Update(mapper ConfigMapper, getenv func(string) string) error {
	// NOTE: there's still a kind of a race condition here as we don't protect access
	value := getenv(mapper(v.key))
	v.value = value
	if len(value) == 0 {
		return errEmptyEnvVar
	}

	return nil
}

type envConfig struct {
	lock   sync.Mutex
	items  map[common.ConfigKey]*envConfigValue
	getenv func(string) string
	mapper ConfigMapper
}

var _ common.ConfigStore = (*envConfig)(nil)

func NewEnvConfig(mapper ConfigMapper, getenv func(string) string) *envConfig {
	return &envConfig{
		items:  make(map[common.ConfigKey]*envConfigValue),
		getenv: getenv,
		mapper: mapper,
	}
}

func (c *envConfig) Get(key common.ConfigKey) common.ConfigItem {
	c.lock.Lock()
	defer c.lock.Unlock()

	item, ok := c.items[key]
	if ok {
		return item
	}

	// NOTE: not optimal to read under the lock, but it's not too bad here
	item = &envConfigValue{
		key:   key,
		value: c.getenv(c.mapper(key)),
	}
	c.items[key] = item

	return item
}

func (c *envConfig) Update(ctx context.Context) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for key, cfg := range c.items {
		if err := cfg.Update(c.mapper, c.getenv); err != nil {
			slog.WarnContext(ctx, "Cannot update environment config", "key", c.mapper(key), common.ErrAttr(err))
		}
	}
}
