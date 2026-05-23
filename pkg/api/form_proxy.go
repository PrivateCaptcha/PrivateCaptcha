package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

const (
	maxFormBodySize              = 1024 * 1024
	formQueueBackpressureTimeout = 400 * time.Millisecond
)

type FormSubmission struct {
	FormExternalID  string
	Values          url.Values
	UserAgent       string
	Referer         string
	ClientIP        string
	TraceID         string
	CaptchaSolution puzzle.SolutionPayload
	Time            time.Time
}

func (s *Server) formPreFlight(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

type formOwnerSource struct {
	Store db.Implementor
	Form  *dbgen.Form
}

var _ puzzle.OwnerIDSource = (*formOwnerSource)(nil)

func (s *formOwnerSource) OwnerID(ctx context.Context, tnow time.Time) (int32, *int32, error) {
	orgID := new(int32)
	*orgID = s.Form.OrgID.Int32

	return s.Form.OrgOwnerID.Int32, orgID, nil
}

func captchaSolution(values url.Values) string {
	for _, field := range []string{common.ParamPrivateCaptchaSolution, common.ParamRecaptchaResponse} {
		if value := values.Get(field); len(value) > 0 {
			return value
		}
	}
	return ""
}

func sanitizedFormValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, fieldValues := range values {
		switch key {
		case common.ParamPrivateCaptchaSolution, common.ParamRecaptchaResponse:
			continue
		}
		result[key] = append([]string(nil), fieldValues...)
	}
	return result
}

func (s *Server) createFormSubmission(ctx context.Context, r *http.Request, payload puzzle.SolutionPayload) *FormSubmission {
	submission := &FormSubmission{
		UserAgent:       r.UserAgent(),
		Referer:         r.Header.Get(common.HeaderReferer),
		CaptchaSolution: payload,
		Time:            time.Now().UTC(),
	}

	submission.Values = sanitizedFormValues(r.PostForm)

	if submission.FormExternalID, _ = ctx.Value(common.FormIDContextKey).(string); len(submission.FormExternalID) == 0 {
		submission.FormExternalID = r.PathValue(common.ParamForm)
	}

	if clientIP, ok := ctx.Value(common.RateLimitKeyContextKey).(netip.Addr); ok {
		submission.ClientIP = clientIP.String()
	}

	if traceID, ok := ctx.Value(common.TraceIDContextKey).(string); ok {
		submission.TraceID = traceID
	}

	return submission
}

func (s *Server) formProxyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	mediaType, _, err := mime.ParseMediaType(r.Header.Get(common.HeaderContentType))
	if (err != nil) || (mediaType != common.ContentTypeURLEncoded) {
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}

	if err := r.ParseForm(); err != nil {
		slog.WarnContext(ctx, "Failed to read form proxy request", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	solution := captchaSolution(r.PostForm)
	if len(solution) == 0 {
		slog.WarnContext(ctx, "Failed to find captcha solution in submission")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	payload, err := s.Verifier.ParseSolutionPayload(ctx, []byte(solution))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// if form is cached, we verify captcha on the hot path
	if form, ok := ctx.Value(common.FormContextKey).(*dbgen.Form); ok && form != nil {
		ownerSource := &formOwnerSource{Store: s.BusinessDB, Form: form}
		result, err := s.Verifier.Verify(ctx, payload, ownerSource, time.Now().UTC())
		if err != nil {
			slog.ErrorContext(ctx, "Failed to verify captcha due to internal error", common.ErrAttr(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		if !result.Success() || (result.PropertyID != ownerSource.Form.PropertyID) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		s.addVerifyRecord(ctx, result)
		payload = nil
	}

	submission := s.createFormSubmission(ctx, r, payload)

	timer := time.NewTimer(formQueueBackpressureTimeout)
	defer timer.Stop()

	select {
	case s.FormSubmissionChan <- submission:
		w.WriteHeader(http.StatusAccepted)
	case <-ctx.Done():
		http.Error(w, http.StatusText(http.StatusRequestTimeout), http.StatusRequestTimeout)
	case <-timer.C:
		s.Metrics.ObserveEventDropped(common.FormEventType)
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
	}
}

func (s *Server) submitFormBatch(ctx context.Context, batch []*FormSubmission) error {
	if len(batch) == 0 {
		return nil
	}

	slog.DebugContext(ctx, "About to submit forms batch", "count", len(batch))

	formIDs := make(map[string]uint, len(batch))
	for _, submission := range batch {
		formIDs[submission.FormExternalID]++
	}

	forms, err := s.BusinessDB.Impl().RetrieveFormsByExternalID(ctx, formIDs, 0)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve forms for submissions", "count", len(formIDs), common.ErrAttr(err))
		return err
	}

	formsByID := make(map[string]*dbgen.Form, len(forms))
	for _, form := range forms {
		if form.Enabled {
			formsByID[db.UUIDToString(form.ExternalID)] = form
		}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for _, submission := range batch {
		form, found := formsByID[submission.FormExternalID]
		if !found || (form == nil) {
			slog.ErrorContext(ctx, "Failed to find matching form for submission", "formID", submission.FormExternalID)
			continue
		}

		// delayed captcha check if original form was not cached at the time of request
		if submission.CaptchaSolution != nil {
			ownerSource := &formOwnerSource{Store: s.BusinessDB, Form: form}
			result, err := s.Verifier.Verify(ctx, submission.CaptchaSolution, ownerSource, submission.Time)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to verify captcha due to internal error", common.ErrAttr(err))
				continue
			}

			if !result.Success() || (result.PropertyID != ownerSource.Form.PropertyID) {
				slog.WarnContext(ctx, "Skipping form submission due to captcha verification error", "result", result.Error.String())
				continue
			}

			s.addVerifyRecord(ctx, result)
		}

		s.submitForm(ctx, client, form, submission)
	}

	return nil
}

func (s *Server) submitForm(ctx context.Context, client *http.Client, form *dbgen.Form, submission *FormSubmission) {
	method := strings.ToUpper(string(form.Method))
	if len(method) == 0 {
		method = http.MethodPost
	}

	attempts := int(form.RetryRequestCount) + 1
	for attempt := 0; attempt < attempts; attempt++ {
		body := submission.Values.Encode()
		req, err := http.NewRequestWithContext(ctx, method, form.URL, bytes.NewBufferString(body))
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create form submission request", "formID", form.ID, common.ErrAttr(err))
			return
		}

		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
		if len(submission.UserAgent) > 0 {
			req.Header.Set(common.HeaderUserAgent, submission.UserAgent)
		} else {
			req.Header.Set(common.HeaderUserAgent, "private-captcha/1.0")
		}

		if len(submission.Referer) > 0 {
			req.Header.Set(common.HeaderReferer, submission.Referer)
		}

		if len(submission.ClientIP) > 0 {
			req.Header.Set(common.HeaderClientIP, submission.ClientIP)
		}

		if len(submission.TraceID) > 0 {
			req.Header.Set(common.HeaderTraceID, submission.TraceID)
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.WarnContext(ctx, "Failed to submit form", "formID", form.ID, "attempt", attempt+1, common.ErrAttr(err))
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if (resp.StatusCode >= http.StatusOK) && (resp.StatusCode < http.StatusMultipleChoices) {
			return
		}

		slog.WarnContext(ctx, "Form submission endpoint returned non-success status", "formID", form.ID, "status", resp.StatusCode, "attempt", attempt+1)
	}
}
