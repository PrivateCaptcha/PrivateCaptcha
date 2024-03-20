package portal

import (
	"math/rand"
)

// NOTE: this will eventually be replaced by proper OTP
func twoFactorCode() int {
	return rand.Intn(900000) + 100000
}
