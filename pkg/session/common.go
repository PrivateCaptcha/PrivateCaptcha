package session

import "time"

type Key int

const (
	KeyLoginStep = iota
	KeyUserEmail
	KeyTwoFactorCode
	KeyUserName
)

type Session interface {
	Set(key Key, value interface{}) error
	Get(key Key) interface{}
	Delete(key Key) error
	SessionID() string
}

type Provider interface {
	SessionInit(sid string) (Session, error)
	SessionRead(sid string) (Session, error)
	SessionDestroy(sid string) error
	SessionGC(d time.Duration)
}
