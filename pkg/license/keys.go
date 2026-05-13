package license

import (
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"encoding/json"
	"log/slog"
)

//go:embed *.keys
var embeddedKeys embed.FS

type hardcodedKey struct {
	KeyID   int    `json:"KeyID"`
	KeyData string `json:"KeyData"`
}

func activationKeys(hardcodedKeys []*hardcodedKey) ([]*ActivationKey, error) {
	keys := make([]*ActivationKey, 0, len(hardcodedKeys))
	for _, k := range hardcodedKeys {
		publicKey, err := base64.StdEncoding.DecodeString(k.KeyData)
		if err != nil {
			return nil, err
		}

		if len(publicKey) == ed25519.PublicKeySize {
			keys = append(keys, &ActivationKey{
				ID:   k.KeyID,
				Data: ed25519.PublicKey(publicKey),
			})
		} else {
			slog.Error("Skipping activation key with invalid size", "keyID", k.KeyID, "expected", ed25519.PublicKeySize, "actual", len(publicKey))
		}
	}
	return keys, nil
}

func ActivationKeys() ([]*ActivationKey, error) {
	var parsedKeys []*hardcodedKey

	files, err := embeddedKeys.ReadDir(".")
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		content, err := embeddedKeys.ReadFile(file.Name())
		if err != nil {
			return nil, err
		}

		var keysFromFile []*hardcodedKey
		if err := json.Unmarshal(content, &keysFromFile); err != nil {
			return nil, err
		}

		parsedKeys = append(parsedKeys, keysFromFile...)
	}

	return activationKeys(parsedKeys)
}
