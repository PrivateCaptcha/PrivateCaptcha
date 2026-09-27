package common

import (
	"errors"
	"strconv"

	"github.com/speps/go-hashids/v2"
)

type idHasher struct {
	// HashID copies its mutable alphabet for each call, so it can be shared by goroutines.
	hashID    *hashids.HashID
	hashIDErr error
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
	h, err := hashids.NewWithData(data)

	return &idHasher{
		hashID:    h,
		hashIDErr: err,
	}
}

func (ih *idHasher) Encrypt(id int) string {
	if ih.hashID != nil {
		if e, err := ih.hashID.Encode([]int{id}); err == nil {
			return e
		}
	}

	return strconv.Itoa(int(id))
}

func (ih *idHasher) Encrypt64(id int64) string {
	if ih.hashID != nil {
		if e, err := ih.hashID.EncodeInt64([]int64{id}); err == nil {
			return e
		}
	}

	return strconv.FormatInt(id, 10)
}

func (ih *idHasher) Decrypt(hash string) (int, error) {
	if ih.hashIDErr != nil {
		return -1, ih.hashIDErr
	}
	if ih.hashID == nil {
		return strconv.Atoi(hash)
	}

	d, err := ih.hashID.DecodeWithError(hash)
	if err != nil {
		return -1, err
	}

	if len(d) != 1 {
		return -1, errUnexpectedIdentifierLen
	}

	return d[0], nil
}

func (ih *idHasher) Decrypt64(hash string) (int64, error) {
	if ih.hashIDErr != nil {
		return -1, ih.hashIDErr
	}
	if ih.hashID == nil {
		return strconv.ParseInt(hash, 10, 64)
	}

	d, err := ih.hashID.DecodeInt64WithError(hash)
	if err != nil {
		return -1, err
	}

	if len(d) != 1 {
		return -1, errUnexpectedIdentifierLen
	}

	return d[0], nil
}
