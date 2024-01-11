package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/utils"
)

const (
	// TODO: Adjust caching durations mindfully
	cacheDuration         = 1 * time.Minute
	negativeCacheDuration = 1 * time.Minute
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
	Store *db.Store
	Cache *db.Cache
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
		if len(sitekey) != utils.SitekeyLen {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		property, err := am.Cache.GetProperty(ctx, sitekey)
		if err == db.ErrNegativeCacheHit {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		if property == nil || err != nil {
			property, err = am.Store.GetPropertyBySitekey(ctx, sitekey)
			if err != nil {
				if err == db.ErrInvalidInput {
					http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				} else {
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}

			if property == nil {
				am.Cache.SetMissing(ctx, sitekey, negativeCacheDuration)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			_ = am.Cache.SetProperty(ctx, property, cacheDuration)
		} else {
			_ = am.Cache.UpdateExpiration(ctx, sitekey, cacheDuration)
		}

		ctx = context.WithValue(ctx, common.PropertyContextKey, property)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
