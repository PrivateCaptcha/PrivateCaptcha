package common

import (
	"net/http"
)

const (
	DefaultOrgName              = "My Organization"
	PrivateCaptcha              = "Private Captcha"
	PrivateCaptchaTeam          = "Private Captcha Team"
	StageDev                    = "dev"
	StageStaging                = "staging"
	StageTest                   = "test"
	ContentTypePlain            = "text/plain"
	ContentTypeHTML             = "text/html; charset=utf-8"
	ContentTypeJSON             = "application/json"
	ContentTypeURLEncoded       = "application/x-www-form-urlencoded"
	ContentTypeCSV              = "text/csv"
	ParamSiteKey                = "sitekey"
	ParamSecret                 = "secret"
	ParamResponse               = "response"
	ParamEmail                  = "email"
	ParamName                   = "name"
	ParamURL                    = "url"
	ParamCSRFToken              = "csrf_token"
	ParamVerificationCode       = "vcode"
	ParamDomain                 = "domain"
	ParamDifficulty             = "difficulty"
	ParamGrowth                 = "growth"
	ParamTab                    = "tab"
	ParamNew                    = "new"
	ParamNever                  = "never"
	ParamDays                   = "days"
	ParamOrg                    = "org"
	ParamUser                   = "user"
	ParamPeriod                 = "period"
	ParamForm                   = "form"
	ParamProperty               = "property"
	ParamRule                   = "rule"
	ParamKey                    = "key"
	ParamCode                   = "code"
	ParamID                     = "id"
	ParamValidityInterval       = "validity_interval"
	ParamAllowSubdomains        = "allow_subdomains"
	ParamAllowLocalhost         = "allow_localhost"
	ParamAllowReplay            = "allow_replay"
	ParamIgnoreError            = "ignore_error"
	ParamLicenseKey             = "lid"
	ParamHardwareID             = "hwid"
	ParamVersion                = "version"
	ParamLicenseType            = "ltype"
	ParamPortalSolution         = "pc_portal_solution"
	ParamPrivateCaptchaSolution = "private-captcha-solution"
	ParamRecaptchaResponse      = "g-recaptcha-response"
	ParamTerms                  = "terms"
	ParamMaxReplayCount         = "max_replay_count"
	ParamPage                   = "page"
	ParamPerPage                = "per_page"
	ParamScope                  = "scope"
	ParamEnabled                = "enabled"
	ParamActive                 = "active"
	ParamRetryRequestCount      = "retry_request_count"
	ParamRequestsPerMinute      = "requests_per_minute"
	ParamConditionProperty      = "condition_property"
	ParamConditionOperator      = "condition_operator"
	ParamConditionValue         = "condition_value"
	ParamConditionNegated       = "condition_negated"
	ParamActionProperty         = "action_property"
	ParamActionValue            = "action_value"
	ParamTerminal               = "terminal"
	ParamPosition               = "position"
	ParamWeeklyReport           = "weekly_report"
	ParamMonthlyReport          = "monthly_report"
	ParamOnboarding             = "onboarding"
	ParamBody                   = "body"
	All                         = "all"
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
	HeaderTraceID             = http.CanonicalHeaderKey("X-Trace-ID")
	HeaderETag                = http.CanonicalHeaderKey("ETag")
	HeaderIfNoneMatch         = http.CanonicalHeaderKey("If-None-Match")
	HeaderSitekey             = http.CanonicalHeaderKey("X-PC-Sitekey")
	HeaderWidgetNotice        = http.CanonicalHeaderKey("X-PC-Widget-Notice")
	HeaderClientIP            = http.CanonicalHeaderKey("X-PC-Client-IP")
	HeaderFormSubmissionID    = http.CanonicalHeaderKey("X-PC-Form-Submission-ID")
	HeaderCacheControl        = http.CanonicalHeaderKey("Cache-Control")
	HeaderReferer             = http.CanonicalHeaderKey("Referer")
	HeaderOrigin              = http.CanonicalHeaderKey("Origin")
	HeaderUserAgent           = http.CanonicalHeaderKey("User-Agent")
)
