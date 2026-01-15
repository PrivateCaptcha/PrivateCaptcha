package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestBaseDifficultyOverride(t *testing.T) {
	t.Parallel()

	v := &Verifier{}

	tests := []struct {
		name       string
		userAgent  string
		headers    map[string]string
		wantResult uint8
	}{
		{
			name:       "EmptyUserAgent",
			userAgent:  "",
			headers:    nil,
			wantResult: uint8(common.DifficultyLevelHigh),
		},
		{
			name:       "ShortUserAgentCurl",
			userAgent:  "curl/7.68.0",
			headers:    nil,
			wantResult: uint8(common.DifficultyLevelMedium),
		},
		{
			name:       "ShortUserAgentPython",
			userAgent:  "python-requests/2.25.1",
			headers:    nil,
			wantResult: uint8(common.DifficultyLevelMedium),
		},
		{
			name:       "NormalUserAgentNoVersion",
			userAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			headers:    nil,
			wantResult: uint8(common.DifficultyLevelHigh),
		},
		{
			name:       "NormalUserAgentWithValidVersion",
			userAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			headers:    map[string]string{common.HeaderCaptchaVersion: "1"},
			wantResult: 0,
		},
		{
			name:       "NormalUserAgentWithInvalidVersion",
			userAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			headers:    map[string]string{common.HeaderCaptchaVersion: "2"},
			wantResult: uint8(common.DifficultyLevelHigh),
		},
		{
			name:       "NormalUserAgentWithEmptyVersion",
			userAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			headers:    map[string]string{common.HeaderCaptchaVersion: ""},
			wantResult: uint8(common.DifficultyLevelHigh),
		},
		{
			name:       "ExactlyAtThreshold75Chars",
			userAgent:  "012345678901234567890123456789012345678901234567890123456789012345678901234",
			headers:    map[string]string{common.HeaderCaptchaVersion: "1"},
			wantResult: 0,
		},
		{
			name:       "JustUnderThreshold74Chars",
			userAgent:  "01234567890123456789012345678901234567890123456789012345678901234567890123",
			headers:    nil,
			wantResult: uint8(common.DifficultyLevelMedium),
		},
		{
			name:       "AtThreshold75CharsNoVersion",
			userAgent:  "012345678901234567890123456789012345678901234567890123456789012345678901234",
			headers:    nil,
			wantResult: uint8(common.DifficultyLevelHigh),
		},
		{
			name:       "AboveThresholdNoVersion",
			userAgent:  "0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890",
			headers:    nil,
			wantResult: uint8(common.DifficultyLevelHigh),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("User-Agent", tt.userAgent)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := v.baseDifficultyOverride(req)
			if result != tt.wantResult {
				t.Errorf("baseDifficultyOverride() = %d, want %d", result, tt.wantResult)
			}
		})
	}
}
