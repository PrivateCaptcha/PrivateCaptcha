package memory

import (
	"container/list"
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func New() *Store {
	return &Store{
		list:     list.New(),
		sessions: make(map[string]*list.Element, 0),
	}
}

type Store struct {
	lock     sync.Mutex
	sessions map[string]*list.Element
	list     *list.List
}

var _ common.SessionStore = (*Store)(nil)

func (p *Store) Init(ctx context.Context, sess *common.Session) error {
	sid := sess.SessionID()
	slog.DebugContext(ctx, "Registering new session", common.SessionIDAttr(sid))

	p.lock.Lock()
	defer p.lock.Unlock()

	element := p.list.PushBack(sess)
	p.sessions[sid] = element
	return nil
}

func (p *Store) Read(ctx context.Context, sid string) (*common.Session, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if element, ok := p.sessions[sid]; ok {
		return element.Value.(*common.Session), nil
	}

	return nil, common.ErrSessionMissing
}

func (p *Store) Destroy(ctx context.Context, sid string) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if element, ok := p.sessions[sid]; ok {
		delete(p.sessions, sid)
		p.list.Remove(element)
		return nil
	}

	return nil
}

func (p *Store) GC(ctx context.Context, maxLifetime time.Duration) {
	slog.DebugContext(ctx, "About to GC session memory store")

	deleted := 0

	p.lock.Lock()
	defer p.lock.Unlock()

	for {
		element := p.list.Back()
		if element == nil {
			break
		}
		if element.Value.(*common.Session).ModifiedAt().Add(maxLifetime).Before(time.Now()) {
			p.list.Remove(element)
			delete(p.sessions, element.Value.(*common.Session).SessionID())
			deleted++
		} else {
			break
		}
	}

	slog.DebugContext(ctx, "Finished GC memory store", "deleted", deleted)
}

func (p *Store) Update(sess *common.Session) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if element, ok := p.sessions[sess.SessionID()]; ok {
		p.list.MoveToFront(element)
		return nil
	}

	return common.ErrSessionMissing
}
