package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func TestLicenseActivation(t *testing.T) {
	t.Parallel()

	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = 123

	tnow := time.Now()

	lm := &LicenseMessage{
		KeyID:      keyID,
		UserID:     "userID",
		ProductID:  "productID",
		Expiration: tnow.Add(1 * time.Hour),
	}

	message, err := lm.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	signature := ed25519.Sign(privKey, message)

	license := &SignedMessage{
		Message:   base64.StdEncoding.EncodeToString(message),
		Signature: base64.StdEncoding.EncodeToString(signature),
	}

	js, err := json.Marshal(&license)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := VerifyActivation(t.Context(), js, []*ActivationKey{{keyID, pubKey}}, tnow)
	if err != nil {
		t.Fatal(err)
	}

	if (msg.UserID != lm.UserID) || (msg.ProductID != lm.ProductID) {
		t.Error("userID or productID do not match in the parsed license message")
	}
}

func TestLicenseMessageRejectsTruncatedVariableLengthFields(t *testing.T) {
	tests := map[string][]byte{
		"user ID":           make([]byte, 17),
		"product ID length": make([]byte, 17),
		"product ID":        make([]byte, 17),
		"expiration":        make([]byte, 17),
	}
	binary.LittleEndian.PutUint32(tests["user ID"][5:9], 64)
	binary.LittleEndian.PutUint32(tests["product ID length"][5:9], 8)
	binary.LittleEndian.PutUint32(tests["product ID"][9:13], 100)
	binary.LittleEndian.PutUint32(tests["expiration"][9:13], 1)

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			message := &LicenseMessage{}
			if err := message.UnmarshalBinary(data); !errors.Is(err, io.ErrShortBuffer) {
				t.Fatalf("expected io.ErrShortBuffer, got %v", err)
			}
		})
	}
}
