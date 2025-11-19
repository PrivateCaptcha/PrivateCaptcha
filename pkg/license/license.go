package license

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	maxUserIDLen    = 64
	maxProductIDLen = 100
)

var (
	errInvalidLicenseMessageFormat = errors.New("invalid license message format")
	errActivationSignature         = errors.New("failed to verify the signature")
	errKeyNotFound                 = errors.New("activation key not found")
	errActivationExpired           = errors.New("activation expired")
)

type ActivationKey struct {
	ID   int
	Data ed25519.PublicKey
}

type LicenseMessage struct {
	KeyID      uint32
	UserID     string
	ProductID  string
	Expiration time.Time
}

func (lm *LicenseMessage) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := lm.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (lm *LicenseMessage) WriteTo(w io.Writer) (int64, error) {
	var n int64

	const version = 0
	if err := binary.Write(w, binary.LittleEndian, byte(version)); err != nil {
		return n, err
	}
	n++

	if err := binary.Write(w, binary.LittleEndian, lm.KeyID); err != nil {
		return n, err
	}
	n += 4

	// UserId {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(lm.UserID))); err != nil {
		return n, err
	}
	n += 4

	if nn, err := w.Write([]byte(lm.UserID)); err != nil {
		return n + int64(nn), err
	}
	n += int64(len(lm.UserID))
	// }

	// ProductID {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(lm.ProductID))); err != nil {
		return n, err
	}
	n += 4

	if nn, err := w.Write([]byte(lm.ProductID)); err != nil {
		return n + int64(nn), err
	}
	n += int64(len(lm.ProductID))
	// }

	var expiration uint32
	if !lm.Expiration.IsZero() {
		expiration = uint32(lm.Expiration.Unix())
	}
	if err := binary.Write(w, binary.LittleEndian, expiration); err != nil {
		return n, err
	}
	n += 4

	return n, nil
}

func (lm *LicenseMessage) UnmarshalBinary(data []byte) error {
	if len(data) < (1 + 4 + 4 + 4 + 4) {
		return io.ErrShortBuffer
	}

	var offset int

	// version field is currently not used
	_ = data[0]
	offset++

	lm.KeyID = binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// userID {
	userIDLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	if userIDLen > maxUserIDLen {
		return errInvalidLicenseMessageFormat
	}
	offset += 4

	userIDEnd := offset + int(userIDLen)
	if userIDEnd >= len(data) {
		return io.ErrShortBuffer
	}
	userIDBytes := make([]byte, userIDLen)
	copy(userIDBytes[:], data[offset:userIDEnd])
	offset += int(userIDLen)

	lm.UserID = string(userIDBytes)
	// }

	// productID {
	productIDLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	if productIDLen > maxProductIDLen {
		return errInvalidLicenseMessageFormat
	}
	offset += 4

	productIDEnd := offset + int(productIDLen)
	if productIDEnd >= len(data) {
		return io.ErrShortBuffer
	}
	productIDBytes := make([]byte, productIDLen)
	copy(productIDBytes[:], data[offset:productIDEnd])
	offset += int(productIDLen)

	lm.ProductID = string(productIDBytes)
	// }

	unixExpiration := int64(binary.LittleEndian.Uint32(data[offset : offset+4]))
	if unixExpiration != 0 {
		lm.Expiration = time.Unix(unixExpiration, 0)
	}
	// nolint:ineffassign
	offset += 4

	return nil
}

type SignedMessage struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

func findKeyByID(keys []*ActivationKey, id int) (*ActivationKey, error) {
	for _, key := range keys {
		if key.ID == id {
			return key, nil
		}
	}

	return nil, errKeyNotFound
}

func VerifyActivation(ctx context.Context, data []byte, keys []*ActivationKey, tnow time.Time) (*LicenseMessage, error) {
	sm := &SignedMessage{}
	err := json.Unmarshal(data, &sm)
	if err != nil {
		slog.WarnContext(ctx, "Failed to unmarshal signed message", common.ErrAttr(err))
		return nil, err
	}

	messageBytes, err := base64.StdEncoding.DecodeString(sm.Message)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64-decode message", common.ErrAttr(err))
		return nil, err
	}

	message := &LicenseMessage{}
	if err := message.UnmarshalBinary(messageBytes); err != nil {
		slog.WarnContext(ctx, "Failed to parse license message from binary", common.ErrAttr(err))
		return nil, err
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(sm.Signature)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64-decode signature", common.ErrAttr(err))
		return nil, err
	}

	key, err := findKeyByID(keys, int(message.KeyID))
	if err != nil {
		slog.WarnContext(ctx, "Failed to find activation key", "keyID", message.KeyID)
		return nil, err
	}

	if !ed25519.Verify(key.Data, messageBytes, signatureBytes) {
		return nil, errActivationSignature
	}

	if message.Expiration.Before(tnow) {
		slog.WarnContext(ctx, "Activation is expired", "expiration", message.Expiration)
		return nil, errActivationExpired
	}

	return message, nil
}
