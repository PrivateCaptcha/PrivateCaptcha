package db

import (
	"context"
	"sort"

	"github.com/maypok86/otter/v2"
)

type sessionOperationGate struct {
	// In-flight owners must not be evicted, so this cache deliberately has no size or expiry policy.
	operations *otter.Cache[string, chan struct{}]
}

func newSessionOperationGate() *sessionOperationGate {
	return &sessionOperationGate{
		operations: otter.Must(&otter.Options[string, chan struct{}]{
			InitialCapacity: 1024,
		}),
	}
}

func (g *sessionOperationGate) acquire(ctx context.Context, sid string) (func(), error) {
	operation := make(chan struct{})
	for {
		current, acquired := g.operations.SetIfAbsent(sid, operation)
		if acquired {
			if err := ctx.Err(); err != nil {
				g.release(sid, operation)
				return nil, err
			}
			return func() { g.release(sid, operation) }, nil
		}

		select {
		case <-current:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (g *sessionOperationGate) acquireSession(ctx context.Context, sid string) (func(), error) {
	// Session operations are non-reentrant; internal helpers run under their caller's ownership.
	return g.acquire(ctx, sid)
}

func (g *sessionOperationGate) acquireBatch(ctx context.Context, sids []string) (func(), error) {
	ordered := append([]string(nil), sids...)
	sort.Strings(ordered)
	releases := make([]func(), 0, len(ordered))
	for i, sid := range ordered {
		if i > 0 && sid == ordered[i-1] {
			continue
		}
		release, err := g.acquireSession(ctx, sid)
		if err != nil {
			releaseSessionOperations(releases)
			return nil, err
		}
		releases = append(releases, release)
	}

	return func() { releaseSessionOperations(releases) }, nil
}

func (g *sessionOperationGate) release(sid string, operation chan struct{}) {
	removed := false
	g.operations.ComputeIfPresent(sid, func(current chan struct{}) (chan struct{}, otter.ComputeOp) {
		if current == operation {
			removed = true
			return nil, otter.InvalidateOp
		}
		return current, otter.CancelOp
	})
	if !removed {
		panic("session operation gate released by a non-owner")
	}
	close(operation)
}

func releaseSessionOperations(releases []func()) {
	for i := len(releases) - 1; i >= 0; i-- {
		releases[i]()
	}
}
