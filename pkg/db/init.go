package db

import (
	"encoding/gob"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func init() {
	gob.Register(dbgen.Property{})
}
