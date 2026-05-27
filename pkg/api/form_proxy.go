package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jpillora/backoff"
)

const (
	maxFormBodySize                  = 1024 * 1024
	formQueueBackpressureTimeout     = 400 * time.Millisecond
	formSubmitLogBackpressureTimeout = 400 * time.Millisecond
	formSubmitStatusSuccess          = 0
	formSubmitStatusFailure          = 1
)

var formOutboundDialer = &net.Dialer{
	Timeout:   10 * time.Second,
	KeepAlive: 10 * time.Second,
}

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

type FormSubmitResult struct {
	Success    bool
	StatusCode int
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
	if s.Form == nil {
		return 0, nil, db.ErrInvalidInput
	}

	if !s.Form.OrgOwnerID.Valid || !s.Form.OrgID.Valid {
		return 0, nil, db.ErrInvalidInput
	}

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

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodySize)
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
		formsByID[db.UUIDToString(form.ExternalID)] = form
	}

	client := s.newFormHTTPClient()

	concurrencyLimit := 4

	sem := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup

	for _, submission := range batch {
		form, found := formsByID[submission.FormExternalID]
		if !found || (form == nil) {
			slog.ErrorContext(ctx, "Failed to find matching form for submission", "formID", submission.FormExternalID)
			continue
		}

		if !form.Enabled || !form.Active {
			slog.WarnContext(ctx, "Skipping inactive or disable form", "formID", form.ID)
			continue
		}

		select {
		case <-ctx.Done():
			slog.WarnContext(ctx, "Batch processing aborted due to context cancellation")
			wg.Wait()
			return ctx.Err()
			// Block here if the semaphore buffer is full
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(f *dbgen.Form, sub *FormSubmission) {
			defer wg.Done()
			defer func() { <-sem }() // Free up a spot in the semaphore when finished

			// delayed captcha check if original form was not cached at the time of request
			if sub.CaptchaSolution != nil {
				ownerSource := &formOwnerSource{Store: s.BusinessDB, Form: f}
				result, err := s.Verifier.Verify(ctx, sub.CaptchaSolution, ownerSource, sub.Time)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to verify captcha due to internal error", common.ErrAttr(err))
					return
				}

				if !result.Success() || (result.PropertyID != ownerSource.Form.PropertyID) {
					slog.WarnContext(ctx, "Skipping form submission due to captcha verification error", "result", result.Error.String())
					return
				}

				s.addVerifyRecord(ctx, result)
			}

			if err := s.FormURLVerifier.VerifyURL(ctx, f.URL); err != nil {
				slog.WarnContext(ctx, "Skipping unsafe form submission URL", "formID", f.ID, common.ErrAttr(err))
				return
			}

			_ = s.submitForm(ctx, client, f, sub)
		}(form, submission)
	}

	wg.Wait()

	return nil
}

func (s *Server) newFormHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = s.formDialContext

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := s.FormURLVerifier.VerifyURL(req.Context(), req.URL.String()); err != nil {
				return fmt.Errorf("unsafe form redirect: %w", err)
			}
			return nil
		},
	}
}

func (s *Server) formDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	host = normalizeFormURLHostname(host)
	if ip, err := netip.ParseAddr(host); err == nil {
		if err := s.FormURLVerifier.VerifyResolvedAddress(ctx, host, ip); err != nil {
			return nil, err
		}
		return formOutboundDialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}

	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("form dial hostname resolved no addresses: %s", host)
	}

	var lastErr error
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok {
			return nil, fmt.Errorf("form dial resolved invalid address: %s", address.IP.String())
		}
		if err := s.FormURLVerifier.VerifyResolvedAddress(ctx, host, ip); err != nil {
			return nil, err
		}

		conn, err := formOutboundDialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

func (s *Server) addFormSubmitRecord(ctx context.Context, form *dbgen.Form, status int8) {
	if form == nil {
		return
	}

	record := &common.FormSubmitRecord{
		UserID:    form.OrgOwnerID.Int32,
		OrgID:     form.OrgID.Int32,
		FormID:    form.ID,
		Timestamp: time.Now().UTC(),
		Status:    status,
	}

	select {
	case s.FormSubmitLogChan <- record:
		// nothing
	case <-ctx.Done():
		slog.WarnContext(ctx, "Context cancelled for adding form submit record", common.ErrAttr(ctx.Err()))
	case <-time.After(formSubmitLogBackpressureTimeout):
		s.Metrics.ObserveEventDropped(common.FormLogEventType)
	}
}

func (s *Server) submitForm(ctx context.Context, client *http.Client, form *dbgen.Form, submission *FormSubmission) *FormSubmitResult {
	result := &FormSubmitResult{}

	method := strings.ToUpper(string(form.Method))
	if len(method) == 0 {
		method = http.MethodPost
	}

	b := &backoff.Backoff{
		Min:    100 * time.Millisecond,
		Max:    2 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	attempts := int(form.RetryRequestCount) + 1
	body := submission.Values.Encode()

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "Job context cancelled while submitting form", common.ErrAttr(ctx.Err()))
				return result
			case <-time.After(b.Duration()):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, form.URL, bytes.NewBufferString(body))
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create form submission request", "formID", form.ID, common.ErrAttr(err))
			return result
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
		result.StatusCode = resp.StatusCode
		_ = resp.Body.Close()

		if (resp.StatusCode >= http.StatusOK) && (resp.StatusCode < http.StatusMultipleChoices) {
			s.addFormSubmitRecord(ctx, form, formSubmitStatusSuccess)
			result.Success = true
			return result
		}

		slog.WarnContext(ctx, "Form submission endpoint returned non-success status", "formID", form.ID, "status", resp.StatusCode, "attempt", attempt+1)
	}

	s.addFormSubmitRecord(ctx, form, formSubmitStatusFailure)
	return result
}
