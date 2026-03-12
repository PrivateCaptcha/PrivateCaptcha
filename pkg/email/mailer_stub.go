package email

import (
	"context"
	"sync/atomic"
)

type StubSender struct {
	Count       int32
	LastMessage *Message
}

var _ Sender = (*StubSender)(nil)

func (sm *StubSender) SendEmail(ctx context.Context, msg *Message) error {
	atomic.AddInt32(&sm.Count, 1)
	sm.LastMessage = msg
	return nil
}
