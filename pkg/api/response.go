package api

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

type ResponseMetadata struct {
	Code        common.StatusCode `json:"code"`
	RequestID   string            `json:"request_id,omitempty"`
	Description string            `json:"description,omitempty"`
}

type APIResponse struct {
	Meta ResponseMetadata `json:"meta"`
	Data interface{}      `json:"data,omitempty"`
}

type apiOrgInput struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type apiOrgOutput struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type apiPropertyInput struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name"`
	Sitekey         string `json:"sitekey,omitempty"`
	OrgID           string `json:"org_id,omitempty"`
	Domain          string `json:"domain"`
	Level           int    `json:"level,omitempty"`
	Growth          string `json:"growth,omitempty"`
	ValiditySeconds int    `json:"validity_seconds,omitempty"`
	AllowSubdomains bool   `json:"allow_subdomains,omitempty"`
	AllowLocalhost  bool   `json:"allow_localhost,omitempty"`
	MaxReplayCount  int    `json:"max_replay_count,omitempty"`
}

type operationResult struct {
	Code common.StatusCode `json:"code"`
}

type apiAsyncTaskOutput struct {
	ID string `json:"id"`
}

type apiAsyncTaskResultOutput struct {
	ID       string      `json:"id"`
	Finished bool        `json:"finished"`
	Result   interface{} `json:"result"`
}
