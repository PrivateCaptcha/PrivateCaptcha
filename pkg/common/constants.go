package common

import "net/http"

const (
	DefaultOrgName        = "My Organization"
	PrivateCaptcha        = "Private Captcha"
	StageProd             = "prod"
	StageDev              = "dev"
	StageStaging          = "staging"
	StageTest             = "test"
	ContentTypePlain      = "text/plain"
	ContentTypeHTML       = "text/html; charset=utf-8"
	ContentTypeJSON       = "application/json"
	ContentTypeURLEncoded = "application/x-www-form-urlencoded"
	ParamSiteKey          = "sitekey"
	ParamResponse         = "response"
	ParamEmail            = "email"
	ParamName             = "name"
	ParamCSRFToken        = "csrf_token"
	ParamVerificationCode = "vcode"
	ParamDomain           = "domain"
	ParamDifficulty       = "difficulty"
	ParamGrowth           = "growth"
	ParamTab              = "tab"
	ParamNew              = "new"
	ParamMonths           = "months"
	ParamCategory         = "category"
	ParamMessage          = "message"
	ParamSubject          = "subject"
	ParamProduct          = "product"
	ParamYearly           = "yearly"
	ParamPrice            = "price"
	ParamOrg              = "org"
	ParamUser             = "user"
	ParamPeriod           = "period"
	ParamProperty         = "property"
	ParamKey              = "key"
	ParamCode             = "code"
	ParamID               = "id"
	ParamAllowSubdomains  = "allow_subdomains"
	ParamAllowLocalhost   = "allow_localhost"
)

var (
	HeaderCDNTag              = http.CanonicalHeaderKey("CDN-Tag")
	HeaderContentType         = http.CanonicalHeaderKey("Content-Type")
	HeaderContentLength       = http.CanonicalHeaderKey("Content-Length")
	HeaderAuthorization       = http.CanonicalHeaderKey("Authorization")
	HeaderCSRFToken           = http.CanonicalHeaderKey("X-CSRF-Token")
	HeaderCaptchaVersion      = http.CanonicalHeaderKey("X-PC-Captcha-Version")
	HeaderCaptchaCompat       = http.CanonicalHeaderKey("X-Captcha-Compat-Version")
	HeaderAPIKey              = http.CanonicalHeaderKey("X-API-Key")
	HeaderAccessControlOrigin = http.CanonicalHeaderKey("Access-Control-Allow-Origin")
	HeaderAccessControlAge    = http.CanonicalHeaderKey("Access-Control-Max-Age")
)
