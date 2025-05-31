package session

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

const (
	KeyLoginStep common.SessionKey = iota
	KeyUserID
	KeyUserEmail
	KeyTwoFactorCode
	KeyUserName
	KeyPersistent
	KeyNotificationID
	KeyReturnURL
)
