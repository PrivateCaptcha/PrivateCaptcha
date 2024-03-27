package common

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type SessionKey int

type SessionValue = interface{}

var (
	ErrSessionMissing = errors.New("session is missing")
)

type Session struct {
	sid          string
	timeAccessed time.Time
	lock         sync.Mutex
	values       map[SessionKey]SessionValue
	store        SessionStore
}

func NewSession(sid string, store SessionStore) *Session {
	v := make(map[SessionKey]SessionValue, 0)
	sd := &Session{sid: sid, timeAccessed: time.Now(), values: v, store: store}
	return sd
}

func (st *Session) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	st.lock.Lock()
	defer st.lock.Unlock()

	if err := encoder.Encode(st.values); err != nil {
		return nil, err
	}

	slog.Log(context.TODO(), LevelTrace, "Marshaled session to binary", "fields", len(st.values))

	return buf.Bytes(), nil
}

func (st *Session) UnmarshalBinary(data []byte) error {
	values := make(map[SessionKey]SessionValue, 0)

	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	if err := decoder.Decode(&values); err != nil {
		return err
	}

	slog.Log(context.TODO(), LevelTrace, "Unmarshaled session from binary", "fields", len(values))

	st.lock.Lock()
	st.values = values
	st.lock.Unlock()

	return nil
}

func (st *Session) ModifiedAt() time.Time {
	return st.timeAccessed
}

func (st *Session) Set(key SessionKey, value SessionValue) error {
	st.lock.Lock()
	st.timeAccessed = time.Now()
	st.values[key] = value
	st.lock.Unlock()

	return st.store.Update(st)
}

func (st *Session) Has(key SessionKey) bool {
	st.lock.Lock()
	defer st.lock.Unlock()

	_, ok := st.values[key]
	return ok
}

func (st *Session) Get(key SessionKey) SessionValue {
	st.timeAccessed = time.Now()
	_ = st.store.Update(st)

	st.lock.Lock()
	defer st.lock.Unlock()
	if v, ok := st.values[key]; ok {
		return v
	}

	slog.Log(context.TODO(), LevelTrace, "Access to missing key in session", "sid", st.sid, "key", key)
	return nil
}

func (st *Session) Delete(key SessionKey) error {
	st.lock.Lock()
	delete(st.values, key)
	st.timeAccessed = time.Now()
	st.lock.Unlock()

	return st.store.Update(st)
}

func (st *Session) SessionID() string {
	return st.sid
}
