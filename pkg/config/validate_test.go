package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func requiredConfigValues() map[common.ConfigKey]string {
	return map[common.ConfigKey]string{
		common.PortalBaseURLKey:     "portal.example.com/portal",
		common.APIBaseURLKey:        "api.example.com/api",
		common.CDNBaseURLKey:        "cdn.example.com/cdn",
		common.AdminEmailKey:        "admin@example.com",
		common.EmailFromKey:         "no-reply@example.com",
		common.RateLimitHeaderKey:   "X-Real-IP",
		common.UserFingerprintIVKey: strings.Repeat("ab", 64),
		common.XSRFKeyKey:           "xsrf-secret",
		common.APISaltKey:           "puzzle-salt",
		common.IDHasherSaltKey:      "identifier-salt",
		common.SmtpEndpointKey:      "smtps://smtp.example.com:465",
		common.SmtpUsernameKey:      "smtp-user",
		common.SmtpPasswordKey:      "smtp-password",
	}
}

func configWithValues(values map[common.ConfigKey]string) common.ConfigStore {
	cfg := NewBaseConfig(NewEnvConfig(func(string) string { return "" }))
	for key, value := range values {
		cfg.Add(NewStaticValue(key, value))
	}
	return cfg
}

func TestValidateConfigRequired(t *testing.T) {
	if err := Validate(t.Context(), configWithValues(requiredConfigValues())); err != nil {
		t.Fatalf("valid required config: %v", err)
	}

	for key := range requiredConfigValues() {
		t.Run(configKeyToEnvName[key], func(t *testing.T) {
			for _, value := range []string{"", " \t\n"} {
				values := requiredConfigValues()
				values[key] = value
				err := Validate(t.Context(), configWithValues(values))
				if err != errInvalidConfig {
					t.Fatalf("required key %v with blank value: got %v", key, err)
				}
			}
		})
	}
}

func TestValidateShutdownDelay(t *testing.T) {
	for _, tc := range []struct {
		name    string
		local   string
		delay   string
		invalid bool
	}{
		{name: "Unset"},
		{name: "LocalRequiresExplicitDelay", local: "localhost:9090", invalid: true},
		{name: "ExplicitZero", local: "localhost:9090", delay: "0s"},
		{name: "ReadinessDrain", local: "localhost:9090", delay: "5s"},
		{name: "FractionalDelay", delay: "250ms"},
		{name: "Negative", delay: "-1s", invalid: true},
		{name: "Invalid", delay: "5", invalid: true},
		{name: "Overflow", delay: "999999999999999999999h", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := requiredConfigValues()
			values[common.LocalAddressKey] = tc.local
			values[common.ShutdownDelayKey] = tc.delay
			err := Validate(t.Context(), configWithValues(values))
			if tc.invalid && err != errInvalidConfig {
				t.Fatalf("expected invalid configuration, got %v", err)
			}
			if !tc.invalid && err != nil {
				t.Fatalf("expected valid configuration, got %v", err)
			}
		})
	}
}

func TestValidateConfigNumbersAndBooleans(t *testing.T) {
	for _, key := range []common.ConfigKey{
		common.VerboseKey, common.MaintenanceModeKey, common.RegistrationAllowedKey,
		common.ClickHouseOptionalKey, common.GradualDataCleanupKey,
	} {
		t.Run(configKeyToEnvName[key], func(t *testing.T) {
			testConfigFormats(t, key,
				[]string{"1", "Y", "y", "yes", "true", "YES", "TRUE", "0", "N", "n", "no", "false", "NO", "FALSE"},
				[]string{"invalid", "2", "True", "on", " true "})
		})
	}

	maxInt := strconv.Itoa(math.MaxInt)
	maxDurationSeconds := strconv.FormatInt(math.MaxInt64/int64(time.Second), 10)
	maxDurationDays := strconv.FormatInt(math.MaxInt64/int64(24*time.Hour), 10)
	maxMemoryMiB := strconv.FormatInt(math.MaxInt64/1024, 10)
	testCases := map[common.ConfigKey]struct {
		valid   []string
		invalid []string
	}{
		common.HealthCheckIntervalKey: {
			valid:   []string{"1", "60", maxDurationSeconds},
			invalid: []string{"0", "-1", "1s", maxInt, "9223372036854775808"},
		},
		common.MaintenanceJobConcurrencyKey: {
			valid:   []string{"1", "2", maxInt},
			invalid: []string{"0", "-1", "1.5", "invalid", "9223372036854775808"},
		},
		common.EnterpriseAuditLogDaysKey: {
			valid:   []string{"1", "365", maxDurationDays},
			invalid: []string{"0", "-1", "14d", maxInt, "9223372036854775808"},
		},
		common.RateLimitBurstKey: {
			valid:   []string{"0", "20", "4294967295"},
			invalid: []string{"-1", "1.5", "4294967296", "invalid"},
		},
		common.RateLimitRateKey: {
			valid:   []string{"0.5", "20", "1e3"},
			invalid: []string{"0", "-1", "NaN", "+Inf", "-Inf", "1e-300", "1e300", "invalid"},
		},
		common.Argon2IDMemoryBudgetKey: {
			valid:   []string{"0", "00", "+0", "16", "256", maxMemoryMiB},
			invalid: []string{"-1", "8", "16MiB", "9007199254740992", "9223372036854775808"},
		},
		common.MinSolveTimeAtMediumKey: {
			valid:   []string{"1ms", "100.5ms", "2s"},
			invalid: []string{"0", "0s", "-1ms", "NaN", "+Inf", "-Inf", "100", "invalid", "2562047h47m16.854775808s"},
		},
		common.PortKey: {
			valid:   []string{"0", "8080", "65535", "http"},
			invalid: []string{"-1", "65536", "invalid", "8080/tcp"},
		},
		common.ClickHousePortKey: {
			valid:   []string{"1", "9000", "65535"},
			invalid: []string{"0", "-1", "65536", "http", "invalid"},
		},
	}
	for key, tc := range testCases {
		t.Run(configKeyToEnvName[key], func(t *testing.T) {
			testConfigFormats(t, key, tc.valid, tc.invalid)
		})
	}
}

func testConfigFormats(t *testing.T, key common.ConfigKey, valid, invalid []string) {
	t.Helper()
	for _, value := range valid {
		values := requiredConfigValues()
		values[common.ShutdownDelayKey] = "0s"
		values[key] = value
		if err := Validate(t.Context(), configWithValues(values)); err != nil {
			t.Errorf("key %v rejected valid value %q: %v", key, value, err)
		}
	}
	for _, value := range invalid {
		values := requiredConfigValues()
		values[common.ShutdownDelayKey] = "0s"
		values[key] = value
		err := Validate(t.Context(), configWithValues(values))
		if err != errInvalidConfig {
			t.Errorf("key %v accepted invalid value %q: %v", key, value, err)
		}
	}
}

func TestValidateConfigURLsAndAddresses(t *testing.T) {
	for _, key := range []common.ConfigKey{common.PortalBaseURLKey, common.APIBaseURLKey, common.CDNBaseURLKey} {
		t.Run(configKeyToEnvName[key], func(t *testing.T) {
			testConfigFormats(
				t,
				key,
				[]string{"example.com", "localhost:8080/prefix/", "[::1]:8080/prefix", "[::1]/prefix"},
				[]string{
					"https://example.com",
					"/prefix",
					"bad host/prefix",
					"::1",
					"example.com:abc",
					"example.com:65536",
					"example.com:",
					"example.com?query=1",
					"example.com#fragment",
					"user@example.com",
				},
			)
		})
	}
	testCases := map[common.ConfigKey]struct {
		valid   []string
		invalid []string
	}{
		common.SmtpEndpointKey: {
			valid:   []string{"smtp://smtp.example.com", "smtps://smtp.example.com", "smtp://localhost:2525", "smtp://[::1]:25"},
			invalid: []string{"smtp.example.com", "https://smtp.example.com", "smtp:///", "smtp://host:abc", "smtp://host:65536", "smtp://host:0"},
		},
		common.LocalAddressKey: {
			valid:   []string{"localhost:9090", "[::1]:9090", ":9090", "127.0.0.1:0"},
			invalid: []string{"localhost", "localhost:", "::1:9090", "host:65536", "http://host:9090", "bad host:9090"},
		},
		common.HostKey: {
			valid:   []string{"localhost", "0.0.0.0", "::1"},
			invalid: []string{"https://host", "host:8080", "bad host", "[::1]"},
		},
		common.PostgresHostKey: {
			valid:   []string{"postgres", "127.0.0.1", "::1", "/var/run/postgresql"},
			invalid: []string{"postgres:5432", "bad host", "host/path"},
		},
		common.ClickHouseHostKey: {
			valid:   []string{"clickhouse", "127.0.0.1", "[::1]"},
			invalid: []string{"clickhouse:9000", "bad host", "::1"},
		},
		common.PostgresKey: {
			valid:   []string{"postgres://user:password@localhost:5432/database?sslmode=disable", "host=localhost user=user password=password dbname=database pool_max_conns=4"},
			invalid: []string{"not-a-dsn", "postgres://host:abc/db", "postgres://host/db?pool_max_conns=0", "postgres://host/db?sslmode=invalid"},
		},
	}
	for key, tc := range testCases {
		t.Run(configKeyToEnvName[key], func(t *testing.T) {
			testConfigFormats(t, key, tc.valid, tc.invalid)
		})
	}
}

func TestValidateConfigEmailsHeadersAndFingerprint(t *testing.T) {
	for _, key := range []common.ConfigKey{common.AdminEmailKey, common.EmailFromKey, common.ReplyToEmailKey} {
		t.Run(configKeyToEnvName[key], func(t *testing.T) {
			testConfigFormats(t, key,
				[]string{"user@example.com", "user+tag@example.com"},
				[]string{"invalid", "@example.com", "user@", "User <user@example.com>", "user@example.com\r\nInjected: header"})
		})
	}
	for _, key := range []common.ConfigKey{common.RateLimitHeaderKey, common.CountryCodeHeaderKey, common.FingerprintHeaderKey} {
		t.Run(configKeyToEnvName[key], func(t *testing.T) {
			testConfigFormats(t, key,
				[]string{"X-Real-IP", "CF-IPCountry", "X-Fingerprint"},
				[]string{"invalid header", "Header:", "Header\r\nInjected", " Header"})
		})
	}
	testConfigFormats(t, common.UserFingerprintIVKey,
		[]string{"ab", strings.Repeat("AB", 64)},
		[]string{"not-hex", "a", strings.Repeat("ab", 65)})
}

func edgeConfigKeys(t *testing.T, curve elliptic.Curve) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: private,
		})), string(pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: public,
		}))
}

func TestValidateConfigEdgeKeys(t *testing.T) {
	private, public := edgeConfigKeys(t, elliptic.P256())
	otherPrivate, otherPublic := edgeConfigKeys(t, elliptic.P256())
	wrongCurvePrivate, wrongCurvePublic := edgeConfigKeys(t, elliptic.P384())
	testCases := []struct {
		name      string
		private   string
		public    string
		secondary string
		valid     bool
	}{
		{
			name:  "disabled",
			valid: true,
		},
		{
			name:    "signing pair",
			private: private,
			public:  public,
			valid:   true,
		},
		{
			name:      "secondary key",
			private:   private,
			public:    public,
			secondary: otherPublic,
			valid:     true,
		},
		{
			name:      "duplicate secondary key",
			private:   private,
			public:    public,
			secondary: public,
			valid:     true,
		},
		{
			name:    "private only",
			private: private,
		},
		{
			name:   "public only",
			public: public,
		},
		{
			name:      "secondary only",
			secondary: otherPublic,
		},
		{
			name:    "mismatched pair",
			private: otherPrivate,
			public:  public,
		},
		{
			name:    "invalid private",
			private: "invalid",
			public:  public,
		},
		{
			name:    "invalid public",
			private: private,
			public:  "invalid",
		},
		{
			name:      "invalid secondary",
			private:   private,
			public:    public,
			secondary: "invalid",
		},
		{
			name:    "wrong curve",
			private: wrongCurvePrivate,
			public:  wrongCurvePublic,
		},
		{
			name:      "wrong secondary curve",
			private:   private,
			public:    public,
			secondary: wrongCurvePublic,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			values := requiredConfigValues()
			values[common.EdgeTokenSigningPrivateKeyKey] = tc.private
			values[common.EdgeTokenSigningPublicKeyKey] = tc.public
			values[common.EdgeTokenSecondaryPublicKeyKey] = tc.secondary
			err := Validate(t.Context(), configWithValues(values))
			if tc.valid && err != nil {
				t.Fatalf("valid edge keys: %v", err)
			}
			if !tc.valid && err != errInvalidConfig {
				t.Fatalf("invalid edge keys: got %v, want the validation error", err)
			}
		})
	}
}

func TestValidateConfigAllKeys(t *testing.T) {
	values := requiredConfigValues()
	optional := map[common.ConfigKey]string{
		common.StageKey:                     "prod",
		common.VerboseKey:                   "1",
		common.LocalAddressKey:              "localhost:9090",
		common.ShutdownDelayKey:             "0s",
		common.MaintenanceModeKey:           "false",
		common.RegistrationAllowedKey:       "true",
		common.HealthCheckIntervalKey:       "60",
		common.PostgresKey:                  "postgres://user:password@localhost/database?sslmode=disable",
		common.PostgresHostKey:              "localhost",
		common.PostgresDBKey:                "database",
		common.PostgresUserKey:              "user",
		common.PostgresPasswordKey:          "password",
		common.PostgresAdminKey:             "admin",
		common.PostgresAdminPasswordKey:     "admin-password",
		common.ClickHouseHostKey:            "localhost",
		common.ClickHouseDBKey:              "database",
		common.ClickHouseUserKey:            "user",
		common.ClickHousePasswordKey:        "password",
		common.ClickHouseAdminKey:           "admin",
		common.ClickHouseAdminPasswordKey:   "admin-password",
		common.RateLimitRateKey:             "2.5",
		common.RateLimitBurstKey:            "20",
		common.ReplyToEmailKey:              "reply@example.com",
		common.HostKey:                      "0.0.0.0",
		common.PortKey:                      "8080",
		common.EnterpriseLicenseKeyKey:      "opaque-enterprise-license",
		common.LocalAPIKeyKey:               "local-api-secret",
		common.CountryCodeHeaderKey:         "X-Country-Code",
		common.EnterpriseAuditLogDaysKey:    "365",
		common.CommunityLicenseKeyKey:       "opaque-community-license",
		common.ClickHouseOptionalKey:        "true",
		common.MaintenanceJobConcurrencyKey: "2",
		common.FingerprintHeaderKey:         "X-Fingerprint",
		common.WidgetNoticeKey:              "Example widget notice",
		common.CacheDirKey:                  "./cache",
		common.ClickHousePortKey:            "9000",
		common.GradualDataCleanupKey:        "yes",
		common.Argon2IDMemoryBudgetKey:      "256",
		common.MinSolveTimeAtMediumKey:      "100ms",
	}
	for key, value := range optional {
		values[key] = value
	}
	values[common.EdgeTokenSigningPrivateKeyKey], values[common.EdgeTokenSigningPublicKeyKey] = edgeConfigKeys(t, elliptic.P256())
	_, values[common.EdgeTokenSecondaryPublicKeyKey] = edgeConfigKeys(t, elliptic.P256())
	for key := common.ConfigKey(0); key < common.COMMON_CONFIG_KEYS_COUNT; key++ {
		if _, ok := values[key]; !ok {
			t.Fatalf("missing validation test value for config key %v", key)
		}
	}
	values[common.COMMON_CONFIG_KEYS_COUNT+1] = "unrelated config is ignored"
	if err := Validate(t.Context(), configWithValues(values)); err != nil {
		t.Fatalf("valid config with all keys: %v", err)
	}
	testConfigFormats(t, common.CacheDirKey, []string{"./cache", "/var/cache/privatecaptcha"}, []string{"cache\x00directory"})
}
