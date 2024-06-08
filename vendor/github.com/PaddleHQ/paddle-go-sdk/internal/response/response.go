// Package response provides the response handling logic for responses and any
// errors returned by the Paddle API.
package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"

	"github.com/PaddleHQ/paddle-go-sdk/pkg/paddleerr"
)

// Response is the wrapper response type returned by the Paddle API.
type Response[T any] struct {
	Data  T                `json:"data"`
	Error *paddleerr.Error `json:"error"`
	Meta  Meta             `json:"meta"`
}

// Meta represents the metadata returned by the Paddle API.
type Meta struct {
	RequestID  string      `json:"request_id"`
	Pagination *Pagination `json:"pagination"`
}

// Pagination represents the pagination information returned by the Paddle API.
type Pagination struct {
	PerPage        int    `json:"per_page"`
	Next           string `json:"next"`
	HasMore        bool   `json:"has_more"`
	EstimatedTotal int    `json:"estimated_total"`
}

// ErrErrorHandlerCallExpected should never be returned, it indicates that the error handling logic has failed.
var ErrErrorHandlerCallExpected = errors.New("error handler should be called, this response should not be handled here")

// Handle handles the response from the Paddle API. The dst field is a response
// which will be decoded from the response body. The dst field should be given as
// a pointer. If the dst field is nil, no response body will be decoded.
// It is expected that any error handling logic is done before calling this
// function, and that this is only for handling of successful responses.
func Handle(req *http.Request, res *http.Response, dst any) (err error) {
	if res.StatusCode >= 400 {
		return ErrErrorHandlerCallExpected
	}

	if res.StatusCode == http.StatusNoContent {
		return nil
	}

	r := &Response[any]{
		Data: &dst,
	}

	teedBytes := bytes.NewBuffer([]byte{})
	tee := io.TeeReader(res.Body, teedBytes)

	if dst != nil && reflect.TypeOf(dst).Elem().Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) {
		err = json.NewDecoder(tee).Decode(dst)
	} else {
		err = json.NewDecoder(tee).Decode(r)
	}

	if err != nil {
		return NewError(err, req.Method, req.URL.Path, res.StatusCode, teedBytes.Bytes())
	}

	return nil
}
