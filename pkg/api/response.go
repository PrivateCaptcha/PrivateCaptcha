package api

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

//easyjson:json
type VerificationResponse struct {
	Success   bool               `json:"success"`
	Code      puzzle.VerifyError `json:"code"`
	Origin    string             `json:"origin,omitempty"`
	Timestamp *common.JSONTime   `json:"timestamp,omitempty"`
}

//easyjson:json
type VerifyResponseRecaptchaV2 struct {
	Success     bool            `json:"success"`
	ErrorCodes  []string        `json:"error-codes,omitempty"`
	ChallengeTS common.JSONTime `json:"challenge_ts"`
	Hostname    string          `json:"hostname"`
}

//easyjson:json
type VerifyResponseRecaptchaV3 struct {
	VerifyResponseRecaptchaV2
	Score  float64 `json:"score"`
	Action string  `json:"action"`
}

//easyjson:json
type ResponseMetadata struct {
	Code        common.StatusCode `json:"code"`
	RequestID   string            `json:"request_id,omitempty"`
	Description string            `json:"description,omitempty"`
}

//easyjson:json
type APIResponse struct {
	Meta       ResponseMetadata `json:"meta"`
	Data       interface{}      `json:"data,omitempty"`
	Pagination *Pagination      `json:"pagination,omitempty"`
}

//easyjson:json
type Pagination struct {
	Page    int  `json:"page"`
	PerPage int  `json:"per_page"`
	HasMore bool `json:"has_more"`
}

type apiOrgInput struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

//easyjson:json
type apiOrgOutput struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

//easyjson:json
type apiOrgPropertyOutput struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Sitekey string `json:"sitekey"`
}

type apiPropertySettings struct {
	Name            string `json:"name"`
	Level           int    `json:"level,omitempty"`
	Growth          string `json:"growth,omitempty"`
	ValiditySeconds int    `json:"validity_seconds,omitempty"`
	AllowSubdomains bool   `json:"allow_subdomains,omitempty"`
	AllowLocalhost  bool   `json:"allow_localhost,omitempty"`
	MaxReplayCount  int    `json:"max_replay_count,omitempty"`
}

type apiCreatePropertyInput struct {
	apiPropertySettings
	Domain string `json:"domain"`
}

type apiUpdatePropertyInput struct {
	apiPropertySettings
	ID string `json:"id"`
}

//easyjson:json
type operationResult struct {
	Code common.StatusCode `json:"code"`
}

//easyjson:json
type apiAsyncTaskOutput struct {
	ID string `json:"id"`
}

//easyjson:json
type apiAsyncTaskResultOutput struct {
	ID       string      `json:"id"`
	Finished bool        `json:"finished"`
	Result   interface{} `json:"result"`
}

//easyjson:json
type apiPropertyOutput struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Domain          string `json:"domain"`
	Sitekey         string `json:"sitekey"`
	Level           int    `json:"level,omitempty"`
	Growth          string `json:"growth,omitempty"`
	ValiditySeconds int    `json:"validity_seconds,omitempty"`
	AllowSubdomains bool   `json:"allow_subdomains,omitempty"`
	AllowLocalhost  bool   `json:"allow_localhost,omitempty"`
	MaxReplayCount  int    `json:"max_replay_count,omitempty"`
}
