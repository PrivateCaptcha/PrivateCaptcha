package portal

import (
	"time"

	"golang.org/x/net/xsrftoken"
)

const (
	actionLogin  = "login"
	actionVerify = "verify"
)

type XSRFMiddleware struct {
	Key     string
	Timeout time.Duration
}

func (xm *XSRFMiddleware) Token(userID, actionID string) string {
	return xsrftoken.Generate(xm.Key, userID, actionID)
}

func (xm *XSRFMiddleware) VerifyToken(token, userID, actionID string) bool {
	return xsrftoken.ValidFor(token, xm.Key, userID, actionID, xm.Timeout)
}
