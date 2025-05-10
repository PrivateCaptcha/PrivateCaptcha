package api

import (
	"encoding/hex"
	"errors"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

var (
	errUAKeyTooLong = errors.New("user fingerprint key is too long")
)

type puzzleSalt struct {
	configItem common.ConfigItem
	value      *puzzle.Salt
}

func NewPuzzleSalt(configItem common.ConfigItem) *puzzleSalt {
	return &puzzleSalt{
		configItem: configItem,
	}
}

func (ps *puzzleSalt) Update() error {
	ps.value = puzzle.NewSalt([]byte(ps.configItem.Value()))
	return nil
}

func (ps *puzzleSalt) Value() *puzzle.Salt {
	return ps.value
}

type userFingerprintKey struct {
	configItem common.ConfigItem
	key        []byte
}

func NewUserFingerprintKey(configItem common.ConfigItem) *userFingerprintKey {
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
