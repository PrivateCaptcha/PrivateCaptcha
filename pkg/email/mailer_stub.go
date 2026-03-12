package email

import (
	"context"
	"sync"
	"sync/atomic"
)

type StubSender struct {
	Count       int32
	LastMessage *Message
	mu          sync.Mutex
}

var _ Sender = (*StubSender)(nil)

func (sm *StubSender) SendEmail(ctx context.Context, msg *Message) error {
	atomic.AddInt32(&sm.Count, 1)
	sm.mu.Lock()
	sm.LastMessage = msg
	sm.mu.Unlock()
	return nil
}
