package db

import (
	"context"
	"errors"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/utils"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidInput = errors.New("invalid input")
)

type Store struct {
	Queries *dbgen.Queries
}

func (s *Store) GetPropertyBySitekey(ctx context.Context, sitekey string) (*dbgen.Property, error) {
	eid := utils.UUIDFromSiteKey(sitekey)

	if !eid.Valid {
		return nil, ErrInvalidInput
	}

	property, err := s.Queries.GetPropertyByExternalID(ctx, eid)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		slog.ErrorContext(ctx, "Failed to retrieve property by external ID", "sitekey", sitekey, common.ErrAttr(err))

		return nil, err
	}

	return property, nil
}
