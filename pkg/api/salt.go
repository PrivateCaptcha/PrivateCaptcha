package api

import (
	"encoding/hex"
	"errors"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errUAKeyTooLong = errors.New("user fingerprint key is too long")
)

type puzzleSalt struct {
	configItem common.ConfigItem
	value      []byte
}

func newPuzzleSalt(configItem common.ConfigItem) *puzzleSalt {
	return &puzzleSalt{
		configItem: configItem,
		value:      make([]byte, 0),
	}
}

func (ps *puzzleSalt) Update() error {
	ps.value = []byte(ps.configItem.Value())
	return nil
}

func (ps *puzzleSalt) Value() []byte {
	return ps.value
}

type userFingerprintKey struct {
	configItem common.ConfigItem
	key        []byte
}

func newUserFingerprintKey(configItem common.ConfigItem) *userFingerprintKey {
	return &userFingerprintKey{
		configItem: configItem,
		key:        make([]byte, 64),
	}
}

func (k *userFingerprintKey) Update() error {
	byteArray, err := hex.DecodeString(k.configItem.Value())
	if err != nil {
		return err
	}

	// this requirement comes from blake256 constructor
	if len(byteArray) > 64 {
		return errUAKeyTooLong
	}

	k.key = byteArray

	return nil
}

func (uf *userFingerprintKey) Value() []byte {
	return uf.key
}
