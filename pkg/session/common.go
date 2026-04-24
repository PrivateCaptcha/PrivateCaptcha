package session

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	ErrSessionMissing = errors.New("session is missing")
)

func init() {
	// for two factor timestamp
	gob.Register(time.Time{})
}

type SessionKey int

const (
	KeyLoginStep SessionKey = iota
	KeyUserID
	KeyUserEmail
	KeyTwoFactorCode
	KeyUserName
	KeyPersistent
	KeyNotificationID
	KeyReturnURL
	KeyTwoFactorCodeTimestamp
	KeyOrgInviteID
	KeyFirstSession
	KeyAdhocNotification
	// Add new fields _above_
	SESSION_KEYS_COUNT
)

func (key SessionKey) String() string {
	switch key {
	case KeyLoginStep:
		return "LoginStep"
	case KeyUserID:
		return "UserID"
	case KeyUserEmail:
		return "UserEmail"
	case KeyTwoFactorCode:
		return "TwoFactorCode"
	case KeyTwoFactorCodeTimestamp:
		return "TwoFactorCodeTimestamp"
	case KeyUserName:
		return "UserName"
	case KeyPersistent:
		return "Persistent"
	case KeyNotificationID:
		return "NotificationID"
	case KeyReturnURL:
		return "ReturnURL"
	case KeyOrgInviteID:
		return "OrgInviteID"
	case KeyFirstSession:
		return "FirstSession"
	default:
		return "SessionKey"
	}
}

type SessionValue = interface{}

type SessionData struct {
	sid    string
	values map[SessionKey]SessionValue
	lock   sync.Mutex
}

func NewSessionData(sid string) *SessionData {
	return &SessionData{
		sid:    sid,
		values: make(map[SessionKey]SessionValue),
	}
}

func (sd *SessionData) Size() int {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	return len(sd.values)
}

func (sd *SessionData) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	sd.lock.Lock()
	defer sd.lock.Unlock()

	if err := encoder.Encode(sd.values); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (sd *SessionData) UnmarshalBinary(data []byte) error {
	values := make(map[SessionKey]SessionValue)

	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	if err := decoder.Decode(&values); err != nil {
		return err
	}

	sd.lock.Lock()
	sd.values = values
	sd.lock.Unlock()

	return nil
}

func (sd *SessionData) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	sd.lock.Lock()
	defer sd.lock.Unlock()

	if err := encoder.Encode(sd.sid); err != nil {
		return nil, err
	}
	if err := encoder.Encode(sd.values); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (sd *SessionData) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	var sid string
	if err := decoder.Decode(&sid); err != nil {
		return err
	}
	var values map[SessionKey]SessionValue
	if err := decoder.Decode(&values); err != nil {
		return err
	}

	sd.sid = sid
	sd.values = values

	return nil
}

func (sd *SessionData) Merge(from *SessionData) {
	// Acquire locks in consistent order to prevent deadlock
	first, second := sd, from
	if sd.sid > from.sid {
		first, second = from, sd
	}

	first.lock.Lock()
	defer first.lock.Unlock()

	second.lock.Lock()
	defer second.lock.Unlock()

	for key, value := range from.values {
		if _, ok := sd.values[key]; !ok {
			sd.values[key] = value
		}
	}
}

func (sd *SessionData) ID() string {
	return sd.sid
}

func (sd *SessionData) Has(key SessionKey) bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	_, ok := sd.values[key]
	return ok
}

func (sd *SessionData) set(key SessionKey, value SessionValue) {
	sd.lock.Lock()
	sd.values[key] = value
	sd.lock.Unlock()
}

func (sd *SessionData) get(key SessionKey) (any, bool) {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	v, ok := sd.values[key]
	return v, ok
}

func (sd *SessionData) delete(key SessionKey) {
	sd.lock.Lock()
	delete(sd.values, key)
	sd.lock.Unlock()
}

type Session struct {
	data  *SessionData
	store Store
}

func NewSession(data *SessionData, store Store) *Session {
	return &Session{
		data:  data,
		store: store,
	}
}

func (s *Session) Merge(from *Session) {
	s.data.Merge(from.data)
}

func (s *Session) Data() *SessionData {
	return s.data
}

func (s *Session) Set(ctx context.Context, key SessionKey, value SessionValue) error {
	s.data.set(key, value)

	return s.store.Update(ctx, s)
}

func (s *Session) ID() string {
	return s.data.ID()
}

func (s *Session) Get(ctx context.Context, key SessionKey) SessionValue {
	v, ok := s.data.get(key)
	if !ok {
		slog.Log(ctx, common.LevelTrace, "Access to missing key in session", common.SessionIDAttr(s.data.ID()), "key", key.String())
	}

	return v
}

func (s *Session) Delete(ctx context.Context, key SessionKey) error {
	s.data.delete(key)

	return s.store.Update(ctx, s)
}

type Store interface {
	Start(ctx context.Context, interval time.Duration)
	Init(ctx context.Context, session *Session) error
	Read(ctx context.Context, sid string, skipCache bool) (*Session, error)
	Update(ctx context.Context, session *Session) error
	Destroy(ctx context.Context, sid string) error
}
