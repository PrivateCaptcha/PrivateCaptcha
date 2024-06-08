package response

import (
	"fmt"
)

// Error contains the details of a non-expected error when making a request to
// the Paddle API. This is not to be confused with the public `paddleerr.Error`
// type. These are returned in cases of non-expected errors, such as decoding,
// timeouts, malformed response bodies, etc.
// The cause of the error can be inspected using errors.Is.
type Error struct {
	cause error

	Method string
	Path   string

	StatusCode   int
	ResponseBody []byte
}

// NewError returns an instance of *Error, which conforms to the
// error interface and allows Unwrap.
func NewError(cause error, method, path string, statusCode int, body []byte) *Error {
	return &Error{
		cause: cause,

		Method: method,
		Path:   path,

		StatusCode:   statusCode,
		ResponseBody: body,
	}
}

// Unwrap allows errors.Is on the underlying error to be performed.
func (de *Error) Unwrap() error {
	return de.cause
}

// Error conforms to the errors interface.
func (de *Error) Error() string {
	return fmt.Sprintf("Decoding issue in call to %s %s (HTTP status %d): %s",
		de.Method, de.Path, de.StatusCode, de.cause)
}
