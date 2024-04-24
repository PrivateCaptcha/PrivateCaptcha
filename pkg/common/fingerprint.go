package common

import "math/rand"

type TFingerprint = uint64

func RandomFingerprint() TFingerprint {
	return uint64(rand.Int63())
}
