package config

import (
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type Config struct {
	getenv        func(string) string
	stage         string
	apiBaseURL    string
	apiDomain     string
	portalBaseURL string
	portalDomain  string
	scheme        string
	verbose       bool
}

func New(getenv func(string) string) (*Config, error) {
	c := &Config{getenv: getenv}
	if err := c.init(); err != nil {
		return nil, err
	}
	return c, nil
}

func splitHostPort(s string) (domain string, port string, err error) {
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

// TODO: Remove this API and only use typed methods
func (c *Config) Getenv(s string) string {
	return c.getenv(s)
}

func (c *Config) init() error {
	var err error

	c.stage = c.getenv("STAGE")
	c.verbose = c.getenv("VERBOSE") == "1"

	c.apiBaseURL = strings.TrimRight(c.getenv("PC_API_BASE_URL"), "/")
	c.apiDomain, _, err = splitHostPort(c.apiBaseURL)
	if err != nil {
		return err
	}

	c.portalBaseURL = strings.TrimRight(c.getenv("PC_PORTAL_BASE_URL"), "/")
	c.portalDomain, _, err = splitHostPort(c.portalBaseURL)
	if err != nil {
		return err
	}

	c.scheme = c.getenv("PC_SCHEME")
	switch c.scheme {
	case "http", "https":
		break
	case "":
		c.scheme = "http"
	default:
		return fmt.Errorf("unknown scheme: %s", c.scheme)
	}

	return nil
}

func (c *Config) Stage() string {
	return c.stage
}

func (c *Config) Verbose() bool {
	return c.verbose
}

func (c *Config) APIDomain() string {
	return c.apiDomain
}

func (c *Config) PortalDomain() string {
	return c.portalDomain
}

func (c *Config) ListenAddress() string {
	host := c.getenv("PC_HOST")
	if host == "" {
		host = "localhost"
	}

	port := c.getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}
	address := net.JoinHostPort(host, port)
	return address
}

func (c *Config) APIURL() string {
	return fmt.Sprintf("%s://%s", c.scheme, c.apiBaseURL)
}

func (c *Config) PortalURL() string {
	return fmt.Sprintf("%s://%s", c.scheme, c.portalBaseURL)
}

func (c *Config) RateLimiterHeader() string {
	return c.getenv(common.ConfigRateLimitHeader)
}

func (c *Config) MaintenanceMode() bool {
	return common.EnvToBool(c.getenv("PC_MAINTENANCE_MODE"))
}

func (c *Config) HealthCheckInterval() time.Duration {
	if "slow" == c.getenv("HEALTHCHECK") {
		return 1 * time.Minute
	}

	return 5 * time.Second
}

func (c *Config) AdminEmail() string {
	return c.getenv("PC_ADMIN_EMAIL")
}
