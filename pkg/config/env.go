package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errEmptyEnvVar  = errors.New("environment variable is empty")
	errEmptyEnvName = errors.New("environment variable name is empty")
)

type envConfigValue struct {
	key   common.ConfigKey
	value string
}

var _ common.ConfigItem = (*envConfigValue)(nil)

var (
	configKeyToEnvName []string
	configKeyStrMux    sync.Mutex
)

func init() {
	configKeyStrMux.Lock()
	defer configKeyStrMux.Unlock()

	if len(configKeyToEnvName) < int(common.COMMON_CONFIG_KEYS_COUNT) {
		configKeyToEnvName = make([]string, common.COMMON_CONFIG_KEYS_COUNT)
	}

	configKeyToEnvName[common.APIBaseURLKey] = "PC_API_BASE_URL"
	configKeyToEnvName[common.PortalBaseURLKey] = "PC_PORTAL_BASE_URL"
	configKeyToEnvName[common.CDNBaseURLKey] = "PC_CDN_BASE_URL"
	configKeyToEnvName[common.StageKey] = "STAGE"
	configKeyToEnvName[common.VerboseKey] = "PC_VERBOSE"
	configKeyToEnvName[common.SmtpEndpointKey] = "SMTP_ENDPOINT"
	configKeyToEnvName[common.SmtpUsernameKey] = "SMTP_USERNAME"
	configKeyToEnvName[common.SmtpPasswordKey] = "SMTP_PASSWORD"
	configKeyToEnvName[common.ClickHouseHostKey] = "PC_CLICKHOUSE_HOST"
	configKeyToEnvName[common.ClickHouseDBKey] = "PC_CLICKHOUSE_DB"
	configKeyToEnvName[common.ClickHouseAdminKey] = "PC_CLICKHOUSE_ADMIN"
	configKeyToEnvName[common.ClickHouseUserKey] = "PC_CLICKHOUSE_USER"
	configKeyToEnvName[common.ClickHouseAdminPasswordKey] = "PC_CLICKHOUSE_ADMIN_PASSWORD"
	configKeyToEnvName[common.ClickHousePasswordKey] = "PC_CLICKHOUSE_PASSWORD"
	configKeyToEnvName[common.PostgresKey] = "PC_POSTGRES"
	configKeyToEnvName[common.PostgresHostKey] = "PC_POSTGRES_HOST"
	configKeyToEnvName[common.PostgresDBKey] = "PC_POSTGRES_DB"
	configKeyToEnvName[common.PostgresUserKey] = "PC_POSTGRES_USER"
	configKeyToEnvName[common.PostgresAdminKey] = "PC_POSTGRES_ADMIN"
	configKeyToEnvName[common.PostgresAdminPasswordKey] = "PC_POSTGRES_ADMIN_PASSWORD"
	configKeyToEnvName[common.PostgresPasswordKey] = "PC_POSTGRES_PASSWORD"
	configKeyToEnvName[common.AdminEmailKey] = "PC_ADMIN_EMAIL"
	configKeyToEnvName[common.EmailFromKey] = "PC_EMAIL_FROM"
	configKeyToEnvName[common.ReplyToEmailKey] = "PC_REPLY_TO_EMAIL"
	configKeyToEnvName[common.LocalAddressKey] = "PC_LOCAL_ADDRESS"
	configKeyToEnvName[common.LocalAPIKeyKey] = "PC_LOCAL_API_KEY"
	configKeyToEnvName[common.MaintenanceModeKey] = "PC_MAINTENANCE_MODE"
	configKeyToEnvName[common.RegistrationAllowedKey] = "PC_REGISTRATION_ALLOWED"
	configKeyToEnvName[common.HealthCheckIntervalKey] = "PC_HEALTHCHECK_INTERVAL"
	configKeyToEnvName[common.RateLimitRateKey] = "PC_RATE_LIMIT_RPS"
	configKeyToEnvName[common.RateLimitBurstKey] = "PC_RATE_LIMIT_BURST"
	configKeyToEnvName[common.RateLimitHeaderKey] = "PC_RATE_LIMIT_HEADER"
	configKeyToEnvName[common.HostKey] = "PC_HOST"
	configKeyToEnvName[common.PortKey] = "PC_PORT"
	configKeyToEnvName[common.UserFingerprintIVKey] = "PC_USER_FINGERPRINT_KEY"
	configKeyToEnvName[common.APISaltKey] = "PC_API_SALT"
	configKeyToEnvName[common.EnterpriseLicenseKeyKey] = "EE_LICENSE_KEY"
	configKeyToEnvName[common.XSRFKeyKey] = "PC_XSRF_KEY"

	for i, v := range configKeyToEnvName {
		if len(v) == 0 {
			panic(fmt.Sprintf("found unconfigured value for key: %v", i))
		}
	}
}

func RegisterEnvNameForConfigKey(key common.ConfigKey, s string) error {
	if len(s) == 0 {
		return errEmptyEnvName
	}

	configKeyStrMux.Lock()
	defer configKeyStrMux.Unlock()

	if int(key) >= len(configKeyToEnvName) {
		newSlice := make([]string, int(key)+1)
		copy(newSlice, configKeyToEnvName)
		configKeyToEnvName = newSlice
	}

	if configKeyToEnvName[key] != "" {
		return fmt.Errorf("config: duplicate env name registration for config key %v", key)
	}

	configKeyToEnvName[key] = s
	return nil
}

func (v *envConfigValue) Key() common.ConfigKey {
	return v.key
}

func (v *envConfigValue) Value() string {
	return v.value
}

func (v *envConfigValue) Update(getenv func(string) string) error {
	var name string
	if int(v.key) < len(configKeyToEnvName) {
		name = configKeyToEnvName[v.key]
	}
	if len(name) == 0 {
		return errEmptyEnvName
	}

	// NOTE: there's still a kind of a race condition here as we don't protect access
	value := getenv(name)
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
}

var _ common.ConfigStore = (*envConfig)(nil)

func NewEnvConfig(getenv func(string) string) *envConfig {
	return &envConfig{
		items:  make(map[common.ConfigKey]*envConfigValue),
		getenv: getenv,
	}
}

func (c *envConfig) Get(key common.ConfigKey) common.ConfigItem {
	c.lock.Lock()
	defer c.lock.Unlock()

	item, ok := c.items[key]
	if ok {
		return item
	}

	var name string
	if int(key) < len(configKeyToEnvName) {
		name = configKeyToEnvName[key]
	}

	// NOTE: not optimal to read under the lock, but it's not _too_ bad here
	item = &envConfigValue{
		key:   key,
		value: c.getenv(name),
	}
	c.items[key] = item

	return item
}

func (c *envConfig) Update(ctx context.Context) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for key, cfg := range c.items {
		if err := cfg.Update(c.getenv); err != nil {
			slog.WarnContext(ctx, "Cannot update environment config", "key", configKeyToEnvName[key], common.ErrAttr(err))
		}
	}
}
