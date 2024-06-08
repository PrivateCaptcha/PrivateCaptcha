package paddle

import (
	"github.com/PaddleHQ/paddle-go-sdk/internal/client"
)

// Option is a function that configures the Paddle SDK.
type Option func(*options)

// options contains the configuration for the Paddle SDK.
type options struct {
	// APIKey is the Paddle API key.
	APIKey string

	// BaseURL is the base URL for the Paddle API.
	BaseURL string

	// Client is the HTTP client used to make requests to the Paddle API.
	Client client.HTTPDoer
}

// WithAPIKey returns an option that sets the Paddle API key.
func WithAPIKey(apiKey string) Option {
	return func(o *options) {
		o.APIKey = apiKey
	}
}

// WithBaseURL returns an option that sets the base URL for the Paddle API.
func WithBaseURL(baseURL string) Option {
	return func(o *options) {
		o.BaseURL = baseURL
	}
}

// WithClient returns an option that sets the HTTP client used to make requests
// to the Paddle API.
func WithClient(c client.HTTPDoer) Option {
	return func(o *options) {
		o.Client = c
	}
}
