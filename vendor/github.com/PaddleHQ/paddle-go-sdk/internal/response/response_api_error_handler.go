package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/PaddleHQ/paddle-go-sdk/pkg/paddleerr"
)

// ErrUnexpectedResponse is returned when an paddle.Error was expected, but instead received nil.
var ErrUnexpectedResponse = errors.New("found nil paddle.Error when one was expected")

// HandleError handles the error response from the Paddle API. This will handle
// cases where the response is the standard error response type from the API,
// and will return the decoded paddleerr.Error. If the response is not valid,
// a response.Error will be returned.
func HandleError(req *http.Request, res *http.Response, requestErr error) error {
	if requestErr != nil {
		return requestErr
	}

	if res.StatusCode < 400 {
		return nil
	}

	contentType := res.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		switch res.StatusCode {
		case http.StatusBadGateway:
			return &paddleerr.Error{
				Code: "bad_gateway",
				Type: "request_error",
			}
		default:
			return NewError(ErrUnexpectedResponse, req.Method, req.URL.Path, res.StatusCode, nil)
		}
	}

	var apiResponse Response[any]

	teedBytes := bytes.NewBuffer([]byte{})
	tee := io.TeeReader(res.Body, teedBytes)

	err := json.NewDecoder(tee).Decode(&apiResponse)
	if err != nil {
		return NewError(err, req.Method, req.URL.Path, res.StatusCode, teedBytes.Bytes())
	}

	if apiResponse.Error == nil {
		return NewError(ErrUnexpectedResponse, req.Method, req.URL.Path, res.StatusCode, teedBytes.Bytes())
	}

	return apiResponse.Error
}
