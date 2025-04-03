package portal

import (
	"time"

	"golang.org/x/net/xsrftoken"
)

type XSRFMiddleware struct {
	Key     string
	Timeout time.Duration
}

func (xm *XSRFMiddleware) Token(userID string) string {
	return xsrftoken.Generate(xm.Key, userID, "-")
}

func (xm *XSRFMiddleware) VerifyToken(token, userID string) bool {
	return xsrftoken.ValidFor(token, xm.Key, userID, "-", xm.Timeout)
}
