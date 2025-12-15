package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
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
