package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
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
	FormExternalID string
	Values         url.Values
	UserAgent      string
	Referer        string
	ClientIP       string
	TraceID        string
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
	properties, err := s.Store.Impl().RetrievePropertiesByID(ctx, map[int32]uint{s.Form.PropertyID: 1})
	if err != nil {
		return -1, nil, err
	}
	if len(properties) == 0 {
		return -1, nil, db.ErrRecordNotFound
	}

	property := properties[0]
	if !property.Enabled {
		return -1, nil, db.ErrDisabled
	}

	var orgID *int32
	if property.OrgID.Valid {
		orgID = new(int32)
		*orgID = property.OrgID.Int32
	}

	return property.OrgOwnerID.Int32, orgID, nil
}

func captchaSolution(values url.Values) string {
	for _, field := range []string{common.ParamPrivateCaptchaSolution, common.ParamRecaptchaResponse, common.ParamResponse, common.ParamPortalSolution} {
		if value := values.Get(field); value != "" {
			return value
		}
	}
	return ""
}

func sanitizedFormValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, fieldValues := range values {
		switch key {
		case common.ParamPrivateCaptchaSolution, common.ParamRecaptchaResponse, common.ParamResponse, common.ParamPortalSolution:
			continue
		}
		result[key] = append([]string(nil), fieldValues...)
	}
	return result
}

func (s *Server) retrieveForm(ctx context.Context, guid string) (*dbgen.Form, error) {
	if form, ok := ctx.Value(common.FormContextKey).(*dbgen.Form); ok && form != nil {
		return form, nil
	}
	return s.BusinessDB.Impl().RetrieveFormByExternalID(ctx, guid)
}

func (s *Server) formProxyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guid, _ := ctx.Value(common.FormGUIDContextKey).(string)
	if guid == "" {
		guid = r.PathValue(common.ParamForm)
	}
	if !db.CanBeValidSitekey(guid) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get(common.HeaderContentType))
	if err != nil || mediaType != common.ContentTypeURLEncoded {
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}

	form, err := s.retrieveForm(ctx, guid)
	if err != nil {
		if errors.Is(err, db.ErrInvalidInput) {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		} else {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		}
		return
	}
	if !form.Enabled {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	guid = db.UUIDToString(form.ExternalID)

	if err := r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read form proxy request", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	solution := captchaSolution(r.PostForm)
	if solution == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	payload, err := s.Verifier.ParseSolutionPayload(ctx, []byte(solution))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	result, err := s.Verifier.Verify(ctx, payload, &formOwnerSource{Store: s.BusinessDB, Form: form}, time.Now().UTC())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if !result.Valid() || result.PropertyID != form.PropertyID {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	s.addVerifyRecord(ctx, result)

	submission := &FormSubmission{
		FormExternalID: guid,
		Values:         sanitizedFormValues(r.PostForm),
		UserAgent:      r.UserAgent(),
		Referer:        r.Header.Get(common.HeaderReferer),
	}
	if clientIP, ok := ctx.Value(common.RateLimitKeyContextKey).(string); ok {
		submission.ClientIP = clientIP
	}
	if traceID, ok := ctx.Value(common.TraceIDContextKey).(string); ok {
		submission.TraceID = traceID
	}

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

func (s *Server) SubmitFormBatch(ctx context.Context, batch []*FormSubmission) error {
	if len(batch) == 0 {
		return nil
	}

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

	for _, submission := range batch {
		form := formsByID[submission.FormExternalID]
		if form == nil {
			continue
		}
		s.submitForm(ctx, form, submission)
	}

	return nil
}

func (s *Server) submitForm(ctx context.Context, form *dbgen.Form, submission *FormSubmission) {
	method := strings.ToUpper(string(form.Method))
	if method == "" {
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
		if submission.UserAgent != "" {
			req.Header.Set("User-Agent", submission.UserAgent)
		}
		if submission.Referer != "" {
			req.Header.Set(common.HeaderReferer, submission.Referer)
		}
		if submission.ClientIP != "" {
			req.Header.Set(common.HeaderClientIP, submission.ClientIP)
		}
		if submission.TraceID != "" {
			req.Header.Set(common.HeaderTraceID, submission.TraceID)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.WarnContext(ctx, "Failed to submit form", "formID", form.ID, "attempt", attempt+1, common.ErrAttr(err))
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return
		}
		slog.WarnContext(ctx, "Form submission endpoint returned non-success status", "formID", form.ID, "status", resp.StatusCode, "attempt", attempt+1)
	}
}
