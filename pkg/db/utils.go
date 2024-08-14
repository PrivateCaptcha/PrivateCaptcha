package db

import (
	"encoding/hex"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	SitekeyLen   = 32
	APIKeyPrefix = "pc_"
	SecretLen    = len(APIKeyPrefix) + SitekeyLen
)

var (
	invalidUUID = pgtype.UUID{Valid: false}
)

func Text(text string) pgtype.Text {
	return pgtype.Text{
		String: text,
		Valid:  true,
	}
}

func Int(i int32) pgtype.Int4 {
	return pgtype.Int4{Int32: i, Valid: true}
}

func Bool(b bool) pgtype.Bool {
	return pgtype.Bool{
		Bool:  b,
		Valid: true,
	}
}

func Timestampz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{Valid: false}
	}

	return pgtype.Timestamptz{
		Time:             t,
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}
}

func Date(t time.Time) pgtype.Date {
	return pgtype.Date{
		Time:             t,
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}
}

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

func UUIDToSecret(uuid pgtype.UUID) string {
	if !uuid.Valid {
		return ""
	}

	return APIKeyPrefix + hex.EncodeToString(uuid.Bytes[:])
}

func UUIDFromSecret(s string) pgtype.UUID {
	if !strings.HasPrefix(s, APIKeyPrefix) {
		return invalidUUID
	}

	s = strings.TrimPrefix(s, APIKeyPrefix)

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
