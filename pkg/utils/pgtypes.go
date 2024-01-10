package utils

import (
	"encoding/hex"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	SitekeyLen = 32
)

var (
	invalidUUID = pgtype.UUID{Valid: false}
)

func UUIDToSiteKey(uuid pgtype.UUID) string {
	if !uuid.Valid {
		return ""
	}

	return hex.EncodeToString(uuid.Bytes[:])
}

func UUIDFromSiteKey(s string) pgtype.UUID {
	if len(s) != SitekeyLen {
		return invalidUUID
	}

	var result pgtype.UUID

	byteArray, err := hex.DecodeString(s)

	if (err == nil) && (len(byteArray) == len(result.Bytes)) {
		copy(result.Bytes[:], byteArray)
		result.Valid = true
		return result
	}

	return invalidUUID
}
