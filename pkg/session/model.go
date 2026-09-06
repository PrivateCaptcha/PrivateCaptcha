package session

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var ErrInvalidPayloadKey = errors.New("session key is not Payload data")

type Authority struct {
	State              State
	Version            int32
	UserID             int32
	ChallengeKind      ChallengeKind
	ChallengeEmail     string
	ExpiresAt          time.Time
	LeaseUntil         time.Time
	VerifyRegistration bool
}

func (a Authority) IsRevoked() bool {
	return a.State == StateRevoked
}

// PayloadStore schedules persistence after local Payload mutations.
type PayloadStore interface {
	UpdatePayload(context.Context, string)
}

type Payload struct {
	lock sync.RWMutex
	// sid is immutable after construction.
	sid    string
	hash   common.SessionHash
	store  PayloadStore
	values map[SessionKey]SessionValue
}

func NewPayload(sid string, store PayloadStore) *Payload {
	if store == nil {
		panic("session PayloadStore is nil")
	}
	return &Payload{
		sid:    sid,
		hash:   common.HashSessionID(sid),
		store:  store,
		values: make(map[SessionKey]SessionValue),
	}
}

func (p *Payload) Get(key SessionKey) SessionValue {
	if !isPayloadKey(key) {
		return nil
	}
	p.lock.RLock()
	value := p.values[key]
	p.lock.RUnlock()
	return value
}

func (p *Payload) Set(ctx context.Context, key SessionKey, value SessionValue) error {
	if !isPayloadKey(key) {
		return ErrInvalidPayloadKey
	}
	p.lock.Lock()
	p.values[key] = value
	p.lock.Unlock()
	p.store.UpdatePayload(ctx, p.sid)
	return nil
}

func (p *Payload) Delete(ctx context.Context, key SessionKey) error {
	if !isPayloadKey(key) {
		return ErrInvalidPayloadKey
	}
	p.lock.Lock()
	delete(p.values, key)
	p.lock.Unlock()
	p.store.UpdatePayload(ctx, p.sid)
	return nil
}

func (p *Payload) Snapshot() ([]byte, error) {
	p.lock.RLock()
	values := make(map[SessionKey]SessionValue, len(p.values))
	for key, value := range p.values {
		values[key] = value
	}
	p.lock.RUnlock()

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(values); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *Payload) Replace(data []byte) error {
	values, err := decodePayload(data)
	if err != nil {
		return err
	}
	p.lock.Lock()
	p.values = values
	p.lock.Unlock()
	return nil
}

func decodePayload(data []byte) (map[SessionKey]SessionValue, error) {
	values := make(map[SessionKey]SessionValue)
	if len(data) > 0 {
		if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&values); err != nil {
			return nil, err
		}
	}
	for key := range values {
		if !isPayloadKey(key) {
			return nil, ErrInvalidPayloadKey
		}
	}
	return values, nil
}

func isPayloadKey(key SessionKey) bool {
	switch key {
	case KeyUserEmail, KeyUserName, KeyNotificationID, KeyReturnURL, KeyOrgInviteID, KeyFirstSession, KeyAdhocNotification:
		return true
	default:
		return false
	}
}

func NewSessionWithAuthority(authority Authority, payload *Payload) *Session {
	if payload == nil {
		panic("session Payload is nil")
	}
	sess := &Session{sid: payload.sid, hash: payload.hash, payload: payload}
	sess.authority.Store(&authority)
	return sess
}

func NewAnonymousSession(sid string, store PayloadStore) *Session {
	payload := NewPayload(sid, store)
	return &Session{sid: payload.sid, hash: payload.hash, payload: payload}
}

func (s *Session) Authority() (Authority, bool) {
	authority := s.authority.Load()
	if authority == nil {
		return Authority{}, false
	}
	return *authority, true
}

func (s *Session) IsRevoked() bool {
	authority, ok := s.Authority()
	return ok && authority.IsRevoked()
}

func (s *Session) Payload() *Payload {
	return s.payload
}
