package client

import (
	"errors"
	"net/url"
	"reflect"

	"github.com/ggicci/httpin/core"
)

// DirectiveInclude is a directive that adds the custom 'include' query
// parameter to the request, based on the value of the directive's struct field.
type DirectiveInclude struct{}

// Decode is not implemented. This is because we only deal with encoding
// concerns in the client.
func (*DirectiveInclude) Decode(_ *core.DirectiveRuntime) error {
	return errors.New("not implemented") //nolint:goerr113 // typed error not needed here.
}

// ErrDirectiveRequiresBool is returned when the directive's struct field is not
// a boolean.
var ErrDirectiveRequiresBool = errors.New("include directive requires a boolean value")

// Encode adds/appends the custom 'include' query parameter to the request.
// The value of the directive's struct field is expected to be a boolean.
// If the value is true, the directive's value query parameter 'include'.
// e.g.
//
//	type Request struct {
//	 IncludeCustomer bool `in:"paddle_include=customer"`
//	}
//
//	Req{IncludeCustomer: true} // ?paddle_include=customer
func (*DirectiveInclude) Encode(rtm *core.DirectiveRuntime) error {
	if len(rtm.Directive.Argv) == 0 {
		return nil
	}

	if rtm.Value.Kind() != reflect.Bool {
		return ErrDirectiveRequiresBool
	}

	if !rtm.Value.Bool() {
		return nil
	}

	encoder := &core.FormEncoder{
		Setter: func(_ string, _ []string) {
			rtm.GetRequestBuilder().Query["include"] = append(
				rtm.GetRequestBuilder().Query["include"], url.QueryEscape(rtm.Directive.Argv[0]),
			)
		},
	}

	return encoder.Execute(rtm)
}
