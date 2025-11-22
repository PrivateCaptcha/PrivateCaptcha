package common

import (
	"errors"
	"strconv"

	"github.com/speps/go-hashids/v2"
)

type idHasher struct {
	hashIDData *hashids.HashIDData
}

var errUnexpectedIdentifierLen = errors.New("unexpected identifier length")

var _ IdentifierHasher = (*idHasher)(nil)

func NewIDHasher(salt ConfigItem) IdentifierHasher {
	saltValue := salt.Value()
	if len(saltValue) == 0 {
		return &idHasher{}
	}

	data := hashids.NewData()
	data.Salt = saltValue
	data.MinLength = 10

	return &idHasher{
		hashIDData: data,
	}
}

func (ih *idHasher) Encrypt(id int) string {
	if ih.hashIDData != nil {
		if h, err := hashids.NewWithData(ih.hashIDData); err == nil {
			if e, err := h.Encode([]int{id}); err == nil {
				return e
			}
		}
	}

	return strconv.Itoa(int(id))
}

func (ih *idHasher) Encrypt64(id int64) string {
	if ih.hashIDData != nil {
		if h, err := hashids.NewWithData(ih.hashIDData); err == nil {
			if e, err := h.EncodeInt64([]int64{id}); err == nil {
				return e
			}
		}
	}

	return strconv.FormatInt(id, 10)
}

func (ih *idHasher) Decrypt(hash string) (int, error) {
	if ih.hashIDData == nil {
		return strconv.Atoi(hash)
	}

	h, err := hashids.NewWithData(ih.hashIDData)
	if err != nil {
		return -1, err
	}

	d, err := h.DecodeWithError(hash)
	if err != nil {
		return -1, err
	}

	if len(d) != 1 {
		return -1, errUnexpectedIdentifierLen
	}

	return d[0], nil
}

func (ih *idHasher) Decrypt64(hash string) (int64, error) {
	if ih.hashIDData == nil {
		return strconv.ParseInt(hash, 10, 64)
	}

	h, err := hashids.NewWithData(ih.hashIDData)
	if err != nil {
		return -1, err
	}

	d, err := h.DecodeInt64WithError(hash)
	if err != nil {
		return -1, err
	}

	if len(d) != 1 {
		return -1, errUnexpectedIdentifierLen
	}

	return d[0], nil
}
