package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	GateCompletePath = "/__privatecaptcha/complete"
	gatePageScript   = "function onCaptchaSolved() { document.getElementById('gate-form').requestSubmit(); }"
)

var GatePageTemplate = template.Must(template.New("gate").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="icon" href="data:,"><title>Verify your request</title>
<style>
  body { display: flex; flex-direction: column; min-height: 100vh; margin: 0; background: #faf8f5; color: #303030; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Arial, sans-serif; }
  main { flex: 1; width: 100%; max-width: 38rem; box-sizing: border-box; margin: 0 auto; padding: clamp(5rem, 17vh, 9rem) 1.5rem 3rem; }
  h1 { margin: 0; font-size: clamp(1.75rem, 5vw, 2rem); font-weight: 600; line-height: 1.25; letter-spacing: -0.02em; }
  p { margin: 1rem 0 0; color: #555; font-size: 1rem; line-height: 1.5; }
  .site-domain { margin: 0 0 1rem; color: inherit; font-size: clamp(2rem, 6vw, 2.5rem); font-weight: 600; line-height: 1.2; overflow-wrap: anywhere; }
  form { margin-top: 2rem; }
  footer { width: calc(100% - 3rem); max-width: 38rem; box-sizing: border-box; margin: 0 auto; padding: 1.25rem 0 2rem; border-top: 1px dashed #bfbebc; text-align: center; font-size: .875rem; line-height: 1.5; }
  footer a { color: inherit; text-underline-offset: .15em; }
  footer a:hover { color: #2f522d; }
  .footer-divider { display: inline-block; height: 1em; margin: 0 .75rem; border-left: 1px solid #bfbebc; vertical-align: middle; }
  @media (max-width: 30rem) {
    main { padding-top: 4rem; }
    .footer-divider { display: none; }
    footer a:last-child { display: block; }
  }
  @media (prefers-color-scheme: dark) {
    body { background: #1f1f1f; color: #f2f2f2; }
    p { color: #c4c4c4; }
  }
  .widget-wrapper {
    position: relative;
    z-index: 0;
    display: block;
    padding: 3px 6px 8px 2px;
    width: 368px;
    min-width: 368px;
    height: 106px;
    border: 0;
    background: url("data:image/svg+xml,%3Csvg width='365' height='113' viewBox='0 0 365 113' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M357.102 0.675781C357.869 0.581559 358.636 0.847062 359.181 1.39551L363.194 5.43945C364.031 6.28233 364.5 7.4219 364.5 8.60938V109C364.5 110.933 362.933 112.5 361 112.5H10.3828C9.87691 112.5 9.38265 112.346 8.96582 112.06L1.61816 107.006C0.924082 106.528 0.516754 105.734 0.535156 104.892L2.4375 18.1211C2.46684 16.7927 3.5303 15.7195 4.8584 15.6777L313.484 6.03125L313.507 6.03027L313.529 6.02734L357.102 0.675781Z' fill='%23709ECC'/%3E%3Cpath d='M357.102 0.675781C357.869 0.581559 358.636 0.847062 359.181 1.39551L363.194 5.43945C364.031 6.28233 364.5 7.4219 364.5 8.60938V109C364.5 110.933 362.933 112.5 361 112.5H10.3828C9.87691 112.5 9.38265 112.346 8.96582 112.06L1.61816 107.006C0.924082 106.528 0.516754 105.734 0.535156 104.892L2.4375 18.1211C2.46684 16.7927 3.5303 15.7195 4.8584 15.6777L313.484 6.03125L313.507 6.03027L313.529 6.02734L357.102 0.675781Z' fill='black' fill-opacity='0.2'/%3E%3Cpath d='M357.102 0.675781C357.869 0.581559 358.636 0.847062 359.181 1.39551L363.194 5.43945C364.031 6.28233 364.5 7.4219 364.5 8.60938V109C364.5 110.933 362.933 112.5 361 112.5H10.3828C9.87691 112.5 9.38265 112.346 8.96582 112.06L1.61816 107.006C0.924082 106.528 0.516754 105.734 0.535156 104.892L2.4375 18.1211C2.46684 16.7927 3.5303 15.7195 4.8584 15.6777L313.484 6.03125L313.507 6.03027L313.529 6.02734L357.102 0.675781Z' stroke='black'/%3E%3Crect x='0.5' y='0.5' width='359' height='107' rx='2.5' fill='white'/%3E%3Crect x='0.5' y='0.5' width='359' height='107' rx='2.5' stroke='black'/%3E%3C/svg%3E") center / contain no-repeat;
    color: #000;
    border-radius: 3px;
    box-shadow: 12px 18px 10px rgba(172, 171, 169, 0.4);
  }
  .widget-wrapper > * {
    position: relative;
    z-index: 2;
  }
  .widget-wrapper .private-captcha {
     height: 100%;
  }
  @media (max-width: 992px) {
    .widget-wrapper {
        width: 244px;
        min-width: 244px;
        height: 72px;
        min-height: 72px;
        padding: 1px 4px 4px 1px;
        box-shadow: 9px 14px 7.5px rgba(172, 171, 169, 0.4);
    }
    .widget-wrapper .private-captcha {
        height: 100%;
    }
  }
</style></head>
<body><main>{{if .Domain}}<p class="site-domain">{{.Domain}}</p>{{end}}<h1>Verify your request</h1><p>Complete the challenge to continue to the website.</p>
<form id="gate-form" method="POST" action="{{.CompletePath}}" class="widget-wrapper"><div class="private-captcha" data-sitekey="{{.Sitekey}}" data-puzzle-endpoint="{{.PuzzleURL}}" data-start-mode="{{.StartMode}}" data-finished-callback="onCaptchaSolved" data-styles="display: block; min-width: 0; height: 100%; font-size: clamp(12px, 1vw + 6px, 18px); --border: 0"></div></form>
</main><footer>Protected by <a href="https://privatecaptcha.com" target="_blank">Private Captcha</a><span class="footer-divider" aria-hidden="true"></span><a href="https://privatecaptcha.com/legal/privacy-end-user/" target="_blank">Privacy policy</a></footer>
<script defer src="{{.ScriptURL}}" crossorigin="anonymous"></script><script>` + gatePageScript + `</script></body></html>`))

var defaultGatePage = NewGatePage("//cdn.privatecaptcha.com", "//api.privatecaptcha.com", "cdn.privatecaptcha.com", "api.privatecaptcha.com", false)

type GatePage struct {
	cdnURL    string
	puzzleURL string
	csp       string
}

func NewGatePage(cdnURL, apiURL, cdnDomain, apiDomain string, allowHTTP bool) *GatePage {
	cdnSource := "https://" + cdnDomain
	apiSource := "https://" + apiDomain
	scriptHash := sha256.Sum256([]byte(gatePageScript))
	if allowHTTP {
		cdnSource += " http://" + cdnDomain
		apiSource += " http://" + apiDomain
	}
	return &GatePage{
		cdnURL:    cdnURL + "/widget/js/",
		puzzleURL: apiURL + "/" + common.PuzzleEndpoint,
		csp: "default-src 'none'; base-uri 'none'; object-src 'none'; form-action 'self'; script-src " + cdnSource + " 'sha256-" + base64.StdEncoding.EncodeToString(
			scriptHash[:],
		) + "' 'wasm-unsafe-eval'; connect-src " + apiSource + "; worker-src blob:; style-src 'unsafe-inline'; img-src data: " + cdnSource,
	}
}

func (am *AuthMiddleware) GateSitekey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sitekey := r.URL.Query().Get(common.ParamSiteKey)
		property, needsRefresh, err := am.Store.Impl().GetCachedPropertyBySitekey(ctx, sitekey)
		switch err {
		case nil:
			if needsRefresh {
				am.refreshPropertyBySitekey(ctx, sitekey)
			}
		case db.ErrCacheMiss:
			am.refreshPropertyBySitekey(ctx, sitekey)
		case db.ErrInvalidInput:
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted, db.ErrTestProperty:
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		default:
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if property != nil {
			if !property.Enabled || property.DeletedAt.Valid || property.EdgeTokenValidityInterval <= 0 {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			if softRestriction, err := am.Limiter.EvaluatePropertyAccess(ctx, property.OrgOwnerID.Int32); err == nil {
				status := http.StatusForbidden
				if softRestriction {
					status = http.StatusTooManyRequests
				}
				http.Error(w, http.StatusText(status), status)
				return
			}
			ctx = context.WithValue(ctx, common.PropertyContextKey, property)
		} else {
			ctx = context.WithValue(ctx, common.SitekeyContextKey, sitekey)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) gatePageHandler(w http.ResponseWriter, r *http.Request) {
	sitekey := r.URL.Query().Get(common.ParamSiteKey)
	property, _ := r.Context().Value(common.PropertyContextKey).(*dbgen.Property)
	if property == nil {
		property, _, _ = s.BusinessDB.Impl().GetCachedPropertyBySitekey(r.Context(), sitekey)
	}
	page := s.GatePage
	if page == nil {
		page = defaultGatePage
	}
	script := "privatecaptcha.js"
	if property != nil && property.Challenge == dbgen.ChallengeTypeArgon2ID {
		script = "privatecaptcha-ext.js"
	}
	startMode := dbgen.EdgeWidgetStartModeClick
	if property != nil && property.EdgeWidgetStartMode == dbgen.EdgeWidgetStartModeLoad {
		startMode = dbgen.EdgeWidgetStartModeLoad
	}
	domain := ""
	if property != nil {
		domain = property.Domain
	}
	out := &bytes.Buffer{}
	if err := GatePageTemplate.Execute(
		out,
		struct{ Sitekey, ScriptURL, PuzzleURL, StartMode, Domain, CompletePath string }{sitekey, page.cdnURL + script, page.puzzleURL, string(startMode), domain, GateCompletePath},
	); err != nil {
		slog.ErrorContext(r.Context(), "Failed to render gate page", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set(common.HeaderContentType, common.ContentTypeHTML)
	w.Header().Set(common.HeaderCacheControl, "public, max-age=3600")
	w.Header().Set(common.HeaderXContentTypeOptions, "nosniff")
	w.Header().Set(common.HeaderXRobotsTag, "noindex, nofollow")
	w.Header().Set(common.HeaderReferrerPolicy, "no-referrer")
	w.Header().Set(common.HeaderContentSecurityPolicy, page.csp)
	w.WriteHeader(http.StatusOK)
	if _, err := out.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "Failed to write gate page", common.ErrAttr(err))
	}
}
