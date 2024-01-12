package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/utils"
)

const (
	// TODO: Adjust caching durations mindfully
	propertyCacheDuration = 1 * time.Minute
	apiKeyCacheDuration   = 1 * time.Minute
	negativeCacheDuration = 1 * time.Minute
)

func Logged(h http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("Processing API request", "path", r.URL.Path, "method", r.Method)
		h.ServeHTTP(w, r)
	})
}

func SafeFormPost(h http.Handler, maxSize int64) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			slog.ErrorContext(r.Context(), "Incorrect http method", "actual", r.Method, "expected", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxSize)

		err := r.ParseForm()
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to read request body", common.ErrAttr(err))
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}

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

func (am *AuthMiddleware) retrieveSecret(r *http.Request) string {
	if r.Method == http.MethodPost {
		return r.FormValue(common.ParamSecret)
	}

	return ""
}

func (am *AuthMiddleware) Public(next http.HandlerFunc) http.HandlerFunc {
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

		property := &dbgen.Property{}
		err := am.Cache.GetItem(ctx, sitekey, property)
		if err == db.ErrNegativeCacheHit {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		if err != nil {
			property, err = am.Store.GetPropertyBySitekey(ctx, sitekey)
			if err != nil {
				if err == db.ErrInvalidInput {
					http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				} else {
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}

			// TODO: Verify if user is an active subscriber
			// also not blacklisted etc.
			if property == nil {
				am.Cache.SetMissing(ctx, sitekey, negativeCacheDuration)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			_ = am.Cache.SetItem(ctx, sitekey, property, propertyCacheDuration)
		} else {
			_ = am.Cache.UpdateExpiration(ctx, sitekey, propertyCacheDuration)
		}

		ctx = context.WithValue(ctx, common.PropertyContextKey, property)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (am *AuthMiddleware) Private(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		secret := am.retrieveSecret(r)
		if len(secret) != utils.SecretLen {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		apiKey := &dbgen.APIKey{}
		err := am.Cache.GetItem(ctx, secret, apiKey)
		if err == db.ErrNegativeCacheHit {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		if err != nil {
			apiKey, err = am.Store.GetAPIKeyBySecret(ctx, secret)
			if err != nil {
				if err == db.ErrInvalidInput {
					http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				} else {
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}

			// TODO: Verify if API key is not blocked
			if apiKey == nil {
				am.Cache.SetMissing(ctx, secret, negativeCacheDuration)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			_ = am.Cache.SetItem(ctx, secret, apiKey, apiKeyCacheDuration)
		} else {
			_ = am.Cache.UpdateExpiration(ctx, secret, apiKeyCacheDuration)
		}

		ctx = context.WithValue(ctx, common.APIKeyContextKey, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
