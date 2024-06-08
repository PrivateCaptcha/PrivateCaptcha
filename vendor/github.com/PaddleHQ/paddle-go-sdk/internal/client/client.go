// Package client provides the base for all requests and responses to the
// Paddle API.
package client

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/PaddleHQ/paddle-go-sdk/internal/response"

	"github.com/ggicci/httpin"
	"github.com/ggicci/httpin/core"
)

// Client provides the base for all requests and responses to the Paddle API.
type Client struct {
	client HTTPDoer

	apiKey  string
	baseURL *url.URL
	version string
}

// HTTPDoer is an interface that expects the Do method for making HTTP requests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

//go:embed version.txt
var versionFs embed.FS

func init() {
	core.RegisterDirective("paddle_include", &DirectiveInclude{})
}

// New returns a new Client with the provided http.Client, apiKey, and baseURL.
func New(httpClient HTTPDoer, apiKey string, baseURL *url.URL) (*Client, error) {
	version, err := versionFs.ReadFile("version.txt")
	if err != nil {
		return nil, err
	}

	return &Client{
		client: httpClient,

		apiKey:  apiKey,
		baseURL: baseURL,
		version: strings.TrimSuffix(string(version), "\n"),
	}, nil
}

// Wanter is an interface that can be implemented by a response struct.
// It is used to inject the client into to the response struct after a request
// has been successfully made.
type Wanter interface {
	Wants(Doer)
}

// Doer is an interface that expects the Do method for making API requests.
type Doer interface {
	Do(ctx context.Context, method, path string, src, dst any) (err error)
}

// Do sends an API request and returns the API response. The src field is a
// request which contains `json` or `in` struct tags (see github.com/ggicci/httpin)
// and will be encoded as the request body. The dst field is a response which will
// be decoded from the response body. The dst field should be given as a pointer.
// If the src field is nil, no request body will be sent. If the dst field is nil,
// no response body will be decoded.
func (c *Client) Do(ctx context.Context, method, path string, src, dst any) (err error) { //nolint:cyclop // flat function
	if ctx == nil {
		ctx = context.Background()
	}

	u, err := url.Parse(path)
	if err != nil {
		return err
	}

	u.Host = c.baseURL.Host
	u.Scheme = c.baseURL.Scheme

	req, err := encodeRequest(ctx, method, u, src)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "PaddleSDK/go "+c.version)

	if transitID := TransitIDFromContext(ctx); transitID != "" {
		req.Header.Set("X-Transaction-Id", transitID) // Deprecated.
		req.Header.Set("X-Transit-Id", transitID)
	}

	if err := attachRequestBody(req, method, src); err != nil {
		return err
	}

	res, requestErr := c.client.Do(req)
	defer func() {
		if requestErr != nil {
			return
		}

		if dErr := res.Body.Close(); dErr != nil {
			err = dErr
		}
	}()

	if err := response.HandleError(req, res, requestErr); err != nil {
		return err
	}

	// We don't return the error from response.Handle here, as we want to return
	// the error from the response body closing in the defer func above.
	err = response.Handle(req, res, dst)

	if dst != nil {
		v := reflect.ValueOf(dst).Elem().Interface()
		if wanter, ok := v.(Wanter); ok {
			wanter.Wants(c)
		}
	}

	return
}

// encodeRequest encodes a request with the given method, URL, and source.
// If the source is nil, then we completely skip `httpin`'s encoder. This is
// because the `httpin` package does not support nil bodies, and empty request
// payloads will cause URL to be completely rewritten.
func encodeRequest(ctx context.Context, method string, u *url.URL, src any) (*http.Request, error) {
	if src == nil {
		return http.NewRequestWithContext(ctx, method, u.String(), http.NoBody)
	}

	return httpin.NewRequestWithContext(ctx, method, u.String(), src)
}

func attachRequestBody(req *http.Request, method string, src any) error {
	if src == nil {
		return nil
	}

	body := bytes.NewBuffer([]byte{})

	if err := json.NewEncoder(body).Encode(src); err != nil {
		return err
	}

	if method == http.MethodGet || method == http.MethodDelete {
		return nil
	}

	if strings.TrimSpace(body.String()) == "{}" {
		return nil
	}

	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")

	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(body), nil
	}

	return nil
}
