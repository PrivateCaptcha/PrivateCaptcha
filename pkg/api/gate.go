package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	GateCompletePath = "/__privatecaptcha/complete"
	// Defer error resets so the widget can finish handling the failed click.
	gatePageScript = `let gateCompleting = false;
async function onCaptchaSolved(widget) {
  if (gateCompleting) return;
  gateCompleting = true;
  document.getElementById('gate-title').textContent = 'Challenge completed';
  document.getElementById('gate-status').textContent = 'Waiting for the website to respond...';
  document.getElementById('gate-help').hidden = true;
  const form = document.getElementById('gate-form');
  form.hidden = true;
  try {
    const response = await fetch(form.action, {
      method: 'POST',
      credentials: 'same-origin',
      body: new URLSearchParams(new FormData(form)),
      redirect: 'error',
      cache: 'no-store'
    });
    if (response.status !== 204) throw new Error('Verification failed');
    window.location.reload();
  } catch (_) {
    gateCompleting = false;
    form.hidden = false;
    document.getElementById('gate-title').textContent = 'Verify your request';
    onCaptchaError(widget);
  }
}
function onCaptchaError(widget) {
  setTimeout(function() { widget.reset({ startMode: 'click' }); }, 0);
  document.getElementById('gate-status').textContent = 'Verification could not be completed. Click the challenge to try again.';
  document.getElementById('gate-help').hidden = false;
}
function onCaptchaStarted() {
  document.getElementById('gate-status').textContent = 'Complete the challenge to continue to the website.';
  document.getElementById('gate-help').hidden = true;
}
window.addEventListener('error', function(event) {
  if (event.target.id === 'gate-widget-script') {
    document.getElementById('gate-form').hidden = true;
    document.getElementById('gate-status').textContent = 'The challenge could not be loaded. Reload this page to try again.';
    document.getElementById('gate-help').hidden = false;
  }
}, true);`
)

var gatePageCachedHeaders = map[string][]string{
	common.HeaderCacheControl: {"public, max-age=300, must-revalidate"},
}

var GatePageTemplate = template.Must(template.New("gate").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="icon" href="data:,"><title>Verify your request</title>
<style>
  body { display: flex; flex-direction: column; min-height: 100vh; margin: 0; background: #faf8f5; color: #303030; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Arial, sans-serif; }
  main { flex: 1; width: 100%; max-width: 38rem; box-sizing: border-box; margin: 0 auto; padding: clamp(5rem, 17vh, 9rem) 1.5rem 3rem; }
  h1 { margin: 0; font-size: clamp(1.75rem, 5vw, 2rem); font-weight: 600; line-height: 1.25; letter-spacing: -0.02em; }
  p { margin: 1rem 0 0; color: #555; font-size: 1rem; line-height: 1.5; }
  [hidden] { display: none !important; }
  .site-domain { margin: 0 0 1rem; color: inherit; font-size: clamp(2rem, 6vw, 2.5rem); font-weight: 600; line-height: 1.2; overflow-wrap: anywhere; }
  .site-domain svg { height: 1cap; width: 1cap; color: #686868; vertical-align: baseline; margin-right: 0.5rem; }
  form { margin-top: 2rem; }
  #gate-help { margin-top: 2rem; font-size: .875rem; }
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
</style><noscript><style>#gate-form, #gate-status { display: none; }</style></noscript></head>
<body><main>{{if .Domain}}<p class="site-domain"><svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="2 2 20 20" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" /></svg>{{.Domain}}</p>{{end}}<h1 id="gate-title">Verify your request</h1><p id="gate-status" role="status">Complete the challenge to continue to the website.</p>
<noscript><p>JavaScript is required to continue. Enable JavaScript and reload this page.</p></noscript>
<form id="gate-form" method="POST" action="{{.CompletePath}}" class="widget-wrapper"><div class="private-captcha" data-sitekey="{{.Sitekey}}" data-puzzle-endpoint="{{.PuzzleURL}}" data-start-mode="{{.StartMode}}" data-finished-callback="onCaptchaSolved" data-errored-callback="onCaptchaError" data-started-callback="onCaptchaStarted" data-theme="auto" data-styles="display: block; min-width: 0; height: 100%; font-size: clamp(12px, 1vw + 4px, 18px); --border: 0"></div></form>
<p id="gate-help" hidden>If the problem persists, check your internet connection or whether a browser extension is blocking the challenge.</p>
</main><footer>Protected by <a href="https://privatecaptcha.com" target="_blank">Private Captcha</a><span class="footer-divider" aria-hidden="true"></span><a href="https://privatecaptcha.com/legal/privacy-end-user/" target="_blank">Privacy policy</a></footer>
<script>` + gatePageScript + `</script><script id="gate-widget-script" defer src="{{.ScriptURL}}" crossorigin="anonymous"></script></body></html>`))

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
		) + "' 'wasm-unsafe-eval'; connect-src 'self' " + apiSource + "; worker-src blob:; style-src 'unsafe-inline'; img-src data: " + cdnSource + "; frame-ancestors 'none'",
	}
}

func (am *AuthMiddleware) GateSitekey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sitekey := r.URL.Query().Get(common.ParamSiteKey)
		sitekey = strings.ToLower(sitekey)
		settings, needsRefresh, err := am.Store.Impl().GetCachedEdgeSettingsBySitekey(ctx, sitekey)
		switch err {
		case nil:
			ctx = context.WithValue(ctx, common.EdgeSettingsContextKey, settings)
			if needsRefresh {
				am.refreshEdgeSettingsBySitekey(ctx, sitekey)
			}
		case db.ErrCacheMiss, db.ErrNegativeCacheHit, db.ErrRecordNotFound:
			property, propertyNeedsRefresh, propertyErr := am.Store.Impl().GetCachedPropertyBySitekey(ctx, sitekey)
			switch propertyErr {
			case nil:
				if !property.Enabled || property.DeletedAt.Valid {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
				if propertyNeedsRefresh {
					am.refreshPropertyBySitekey(ctx, sitekey)
				}
			case db.ErrCacheMiss:
				am.refreshPropertyBySitekey(ctx, sitekey)
			case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted, db.ErrTestProperty:
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			default:
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if err == db.ErrCacheMiss {
				am.refreshEdgeSettingsBySitekey(ctx, sitekey)
			}
		case db.ErrInvalidInput:
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		case db.ErrSoftDeleted, db.ErrTestProperty:
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		default:
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		ctx = context.WithValue(ctx, common.SitekeyContextKey, sitekey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) gatePageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sitekey := r.URL.Query().Get(common.ParamSiteKey)
	settings, _ := ctx.Value(common.EdgeSettingsContextKey).(*dbgen.GetEdgeSettingsBySitekeyRow)
	if settings == nil {
		settings, _, _ = s.BusinessDB.Impl().GetCachedEdgeSettingsBySitekey(ctx, sitekey)
	}
	page := s.GatePage
	if page == nil {
		page = defaultGatePage
	}
	script := "privatecaptcha.js"
	if settings != nil && settings.Challenge == dbgen.ChallengeTypeArgon2ID {
		script = "privatecaptcha-ext.js"
	}
	startMode := dbgen.EdgeWidgetStartModeClick
	if settings != nil && settings.EdgeWidgetStartMode == dbgen.EdgeWidgetStartModeLoad {
		startMode = dbgen.EdgeWidgetStartModeLoad
	}
	domain := ""
	if settings != nil {
		domain = settings.Domain
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
	if settings == nil {
		w.Header().Set(common.HeaderCacheControl, "no-store")
	} else {
		common.WriteHeaders(w, gatePageCachedHeaders)
	}
	w.Header().Set(common.HeaderXContentTypeOptions, "nosniff")
	w.Header().Set(common.HeaderXRobotsTag, "noindex, nofollow")
	w.Header().Set(common.HeaderReferrerPolicy, "no-referrer")
	w.Header().Set(common.HeaderContentSecurityPolicy, page.csp)
	w.WriteHeader(http.StatusOK)
	if _, err := out.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "Failed to write gate page", common.ErrAttr(err))
	}
}
