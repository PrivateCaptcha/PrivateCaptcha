package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
)

func Logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("Processing file request", "path", r.URL.Path, "method", r.Method)
		h.ServeHTTP(w, r)
	})
}

func Method(m string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			slog.ErrorContext(r.Context(), "Incorrect http method", "actual", r.Method, "expected", m)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		slog.Debug("Processing API request", "path", r.URL.Path, "method", r.Method)
		next.ServeHTTP(w, r)
	}
}

type AuthMiddleware struct {
	Queries *dbgen.Queries
}

func (am *AuthMiddleware) retrieveSiteKey(r *http.Request) string {
	if r.Method == http.MethodGet {
		return r.URL.Query().Get(common.ParamSiteKey)
	} else if r.Method == http.MethodPost {
		return r.FormValue(common.ParamSiteKey)
	}

	return ""
}

func (am *AuthMiddleware) Authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		sitekey := am.retrieveSiteKey(r)
		eid := &pgtype.UUID{}

		if err := eid.UnmarshalJSON([]byte(sitekey)); err != nil || !eid.Valid {
			slog.ErrorContext(ctx, "Cannot parse sitekey", "sitekey", sitekey, common.ErrAttr(err))
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)

			return
		}

		// TODO: Add property caching in Redis
		property, err := am.Queries.GetPropertyByExternalID(ctx, *eid)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve property by external ID", "eid", sitekey, common.ErrAttr(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		}

		ctx = context.WithValue(ctx, common.PropertyContextKey, property)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
