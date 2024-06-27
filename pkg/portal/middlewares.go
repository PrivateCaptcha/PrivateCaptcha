package portal

import (
	"time"

	"golang.org/x/net/xsrftoken"
)

const (
	actionLogin            = "login"
	actionVerify           = "verify"
	actionRegister         = "register"
	actionNewProperty      = "property-new"
	actionNewOrg           = "org-new"
	actionPropertySettings = "property-settings"
	actionOrgSettings      = "org-settings"
	actionOrgMembers       = "org-members"
	actionUserSettings     = "user-settings"
	actionAPIKeysSettings  = "apikeys-settings"
	actionSupport          = "support"
	actionBillingSettings  = "billing-settings"
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
