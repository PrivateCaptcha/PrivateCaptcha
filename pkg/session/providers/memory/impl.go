package memory

import (
	"container/list"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func New() *Provider {
	return &Provider{
		list:     list.New(),
		sessions: make(map[string]*list.Element, 0),
	}
}

type SessionStore struct {
	sid          string                      // unique session id
	timeAccessed time.Time                   // last access time
	value        map[session.Key]interface{} // session value stored inside
	provider     *Provider
}

var _ session.Session = (*SessionStore)(nil)

func (st *SessionStore) Set(key session.Key, value interface{}) error {
	st.value[key] = value
	st.provider.SessionUpdate(st.sid)

	return nil
}

func (st *SessionStore) Get(key session.Key) interface{} {
	st.provider.SessionUpdate(st.sid)

	if v, ok := st.value[key]; ok {
		return v
	}

	return nil
}

func (st *SessionStore) Delete(key session.Key) error {
	delete(st.value, key)
	st.provider.SessionUpdate(st.sid)

	return nil
}

func (st *SessionStore) SessionID() string {
	return st.sid
}

type Provider struct {
	lock     sync.Mutex
	sessions map[string]*list.Element
	list     *list.List
}

var _ session.Provider = (*Provider)(nil)

func (p *Provider) SessionInit(sid string) (session.Session, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	v := make(map[session.Key]interface{}, 0)
	newsess := &SessionStore{sid: sid, timeAccessed: time.Now(), value: v, provider: p}
	element := p.list.PushBack(newsess)
	p.sessions[sid] = element
	return newsess, nil
}

func (pder *Provider) SessionRead(sid string) (session.Session, error) {
	if element, ok := pder.sessions[sid]; ok {
		return element.Value.(*SessionStore), nil
	}

	return pder.SessionInit(sid)
}

func (p *Provider) SessionDestroy(sid string) error {
	if element, ok := p.sessions[sid]; ok {
		delete(p.sessions, sid)
		p.list.Remove(element)
		return nil
	}

	return nil
}

func (p *Provider) SessionGC(maxLifetime time.Duration) {
	p.lock.Lock()
	defer p.lock.Unlock()

	for {
		element := p.list.Back()
		if element == nil {
			break
		}
		if element.Value.(*SessionStore).timeAccessed.Add(maxLifetime).Before(time.Now()) {
			p.list.Remove(element)
			delete(p.sessions, element.Value.(*SessionStore).sid)
		} else {
			break
		}
	}
}

func (p *Provider) SessionUpdate(sid string) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if element, ok := p.sessions[sid]; ok {
		element.Value.(*SessionStore).timeAccessed = time.Now()
		p.list.MoveToFront(element)
		return nil
	}

	return nil
}
