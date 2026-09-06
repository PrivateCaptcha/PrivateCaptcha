package common

import (
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/blake2b"
)

type SessionHash struct {
	value string
}

func HashSessionID(sid string) SessionHash {
	digest := blake2b.Sum256([]byte(sid))
	return SessionHash{value: hex.EncodeToString(digest[:8])}
}

func ParseSessionHash(value string) (SessionHash, bool) {
	if len(value) != 16 || value != strings.ToLower(value) {
		return SessionHash{}, false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return SessionHash{}, false
	}
	return SessionHash{value: value}, true
}

func (h SessionHash) String() string {
	return h.value
}

func (h SessionHash) IsZero() bool {
	return h.value == ""
}
