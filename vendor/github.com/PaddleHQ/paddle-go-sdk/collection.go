package paddle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"

	"github.com/PaddleHQ/paddle-go-sdk/internal/client"
	"github.com/PaddleHQ/paddle-go-sdk/internal/response"
)

// Collection is the response from a listing endpoint in the Paddle API.
// It is used as a container to iterate over many pages of results.
type Collection[T any] struct {
	client Doer

	m       sync.RWMutex
	pos     uint32
	results []*Res[T]

	nextURL        *url.URL
	itemsPerPage   int
	estimatedTotal int
	hasMore        bool
}

// PerPage returns the number of items per page in the collection.
func (c *Collection[T]) PerPage() int {
	return c.itemsPerPage
}

// EstimatedTotal returns the estimated number of items in the collection.
func (c *Collection[T]) EstimatedTotal() int {
	return c.estimatedTotal
}

// HasMore returns true if there are more pages of results to be fetched.
func (c *Collection[T]) HasMore() bool {
	return c.hasMore
}

// Res is a single result returned from an iteration of a collection.
type Res[T any] struct {
	value T
	err   error
	end   bool
}

// Ok returns true if the result is not an error and not the end of the
// collection.
func (r *Res[T]) Ok() bool {
	return !r.end
}

// Err returns the error from the result, if it exists. This should be checked
// on each iteration.
func (r *Res[T]) Err() error {
	return r.err
}

// Value returns the value contained from the result.
func (r *Res[T]) Value() T {
	return r.value
}

// Next returns the next result from the collection.
// If there are no more results, the result will response to Ok() with false.
func (c *Collection[T]) Next(ctx context.Context) *Res[T] {
	// We need to lock, as we mutate the underlying position and read values in
	// sequence. It is unlikely that multiple consumers will be reading from the
	// same collection as it provides no real performance benefit, however this
	// prevents a possible data-race with misuse.
	c.m.Lock()
	defer c.m.Unlock()

	// If we're about to seek past the end of the results, we need to fetch the
	// next page of results.
	if c.pos >= uint32(len(c.results)) {
		if !c.hasMore {
			return &Res[T]{end: true}
		}

		err := c.client.Do(ctx, http.MethodGet, c.nextURL.String(), nil, &c)
		if err != nil {
			return &Res[T]{end: true, err: err}
		}

		if len(c.results) == 0 {
			return &Res[T]{end: true}
		}
	}

	// Copy the result, as the next iteration may reset results.
	ptrCopy := *c.results[c.pos]
	c.pos++

	return &ptrCopy
}

// Ensure that Collection[T] implements the json.Unmarshaler and the
// client.Wanter interfaces.
var (
	_ json.Unmarshaler = &Collection[any]{}
	_ client.Wanter    = &Collection[any]{}
)

// Wants sets the client to be used for making requests.
func (c *Collection[T]) Wants(d client.Doer) {
	c.client = d
}

// Iter iterates over the collection, calling the given function for each result.
// If the function returns false, the iteration will stop.
// You should check the error given to your callback function on each iteration.
func (c *Collection[T]) Iter(ctx context.Context, fn func(v T) (bool, error)) error {
	for {
		r := c.Next(ctx)
		if !r.Ok() {
			return r.Err()
		}

		cont, err := fn(r.Value())
		if err != nil {
			return err
		}

		if !cont {
			return nil
		}
	}
}

// ErrStopIteration is used as a return from the iteration function to stop the
// iteration from proceeding. It will ensure that IterErr returns nil.
var ErrStopIteration = errors.New("stop iteration")

// IterErr iterates over the collection, calling the given function for each
// result.
// If the function returns false, the iteration will stop.
// You should check the error given to the function on each iteration.
func (c *Collection[T]) IterErr(ctx context.Context, fn func(v T) error) error {
	for {
		r := c.Next(ctx)
		if !r.Ok() {
			return r.Err()
		}

		err := fn(r.Value())
		if err != nil && errors.Is(err, ErrStopIteration) {
			return nil
		} else if err != nil {
			return err
		}
	}
}

// UnmarshalJSON unmarshals the collection from a JSON response.
func (c *Collection[T]) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}

	// Unmarshal into an intermediary struct with delayed decoding
	var res response.Response[[]json.RawMessage]

	if err := json.Unmarshal(b, &res); err != nil {
		return err
	}

	// Reset results.
	c.results = nil
	c.pos = 0

	for _, item := range res.Data {
		switch any(c).(type) {
		case *Collection[Event]:
			e, err := unmarshalEvent(item)
			if err != nil {
				return err
			}

			c.results = append(c.results, &Res[T]{value: any(e).(T)}) //nolint:forcetypeassert // we know the type is correct.
		default:
			var t T
			if err := json.Unmarshal(item, &t); err != nil {
				return err
			}

			c.results = append(c.results, &Res[T]{value: t}) //nolint:forcetypeassert // we know the type is correct.
		}
	}

	nextURL, err := url.Parse(res.Meta.Pagination.Next)
	if err != nil {
		return err
	}

	c.itemsPerPage = res.Meta.Pagination.PerPage
	c.nextURL = nextURL
	c.estimatedTotal = res.Meta.Pagination.EstimatedTotal
	c.hasMore = res.Meta.Pagination.HasMore

	return nil
}
