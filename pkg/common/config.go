package common

type ConfigKey int

const (
	StageKey ConfigKey = iota
	VerboseKey
	APIBaseURLKey
	PortalBaseURLKey
	CDNBaseURLKey
	LocalAddressKey
	RateLimitHeaderKey
	MaintenanceModeKey
	RegistrationAllowedKey
	HealthCheckIntervalKey
	AdminEmailKey
	PrivateAPIKeyKey
	PostgresKey
	PostgresHostKey
	PostgresDBKey
	PostgresUserKey
	PostgresPasswordKey
	PostgresAdminKey
	PostgresAdminPasswordKey
	ClickHouseHostKey
	ClickHouseDBKey
	ClickHouseUserKey
	ClickHousePasswordKey
	ClickHouseAdminKey
	ClickHouseAdminPasswordKey
	PuzzleLeakyBucketRateKey
	PuzzleLeakyBucketBurstKey
	DefaultLeakyBucketRateKey
	DefaultLeakyBucketBurstKey
	SmtpEndpointKey
	SmtpUsernameKey
	SmtpPasswordKey
	EmailFromKey
	HostKey
	PortKey
	UserFingerprintIVKey
	APISaltKey
	// Add new fields _above_
	COMMON_CONFIG_KEYS_COUNT
)
