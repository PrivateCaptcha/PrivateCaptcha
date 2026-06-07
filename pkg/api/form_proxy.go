package api

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jpillora/backoff"
	"github.com/rs/xid"
)

const (
	maxFormBodySize = 1024 * 1024
	// the logic is basically maxFormFailures during db.DefaultCacheTTL period leads to immediate form block
	// NOTE: yes, we can get _very_ unlucky due to load balancing if all "bad" submissions for this form land here
	// but this is considered acceptable due to extremely low probability
	maxFormFailures = FormBatchSize / 10

	formQueueBackpressureTimeout     = 1 * time.Second
	formSubmitLogBackpressureTimeout = 400 * time.Millisecond
	formSubmitStatusSuccess          = 0
	formSubmitStatusFailure          = 1
)

var (
	errCaptchaVerificationFailed = errors.New("captcha verification failed")
	errFormSubmitFailed          = errors.New("form submit failed")
)

var formOutboundDialer = &net.Dialer{
	Timeout:   10 * time.Second,
	KeepAlive: 10 * time.Second,
}

type FormSubmission struct {
	ID              string
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
	StatusCode int
	Success    bool
	Valid      bool
}

func (fsr *FormSubmitResult) ResultCode() int8 {
	var code int8 = formSubmitStatusFailure
	if fsr.Success {
		code = formSubmitStatusSuccess
	}
	return code
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
		ID:              xid.New().String(),
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
		if form.RequestsPerMinute > 0 {
			capacity := leakybucket.TLevel(form.RequestsPerMinute) + 10
			leakInterval := time.Minute / time.Duration(form.RequestsPerMinute)
			s.RateLimiter.UpdateRequestLimits(r, capacity, leakInterval)
		} else {
			slog.WarnContext(ctx, "Skipping form rate limit update due to invalid RPM", "formID", form.ID, "requestsPerMinute", form.RequestsPerMinute)
		}
		payload = nil
	}

	submission := s.createFormSubmission(ctx, r, payload)

	timer := time.NewTimer(formQueueBackpressureTimeout)
	defer timer.Stop()

	select {
	case s.FormSubmissionChan <- submission:
		slog.Log(ctx, common.LevelTrace, "Accepted form submission", "submitID", submission.ID)
		w.WriteHeader(http.StatusAccepted)
	case <-ctx.Done():
		http.Error(w, http.StatusText(http.StatusRequestTimeout), http.StatusRequestTimeout)
	case <-timer.C:
		if form, ok := getRequestedForm(ctx, s.BusinessDB); ok {
			// at this stage it also means we have verified the captcha earlier (above)
			// we limit retries on the hot path as by definition here we are already quite busy
			formCopy := *form
			formCopy.RetryRequestCount = 0
			if err := s.processFormSubmission(ctx, &formCopy, submission); err == nil {
				w.WriteHeader(http.StatusAccepted)
			} else {
				http.Error(w, http.StatusText(http.StatusNotAcceptable), http.StatusNotAcceptable)
			}
		} else {
			s.Metrics.ObserveEventDropped(common.FormEventType)
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		}
	}
}

func getRequestedForm(ctx context.Context, store db.Implementor) (*dbgen.Form, bool) {
	if form, ok := ctx.Value(common.FormContextKey).(*dbgen.Form); ok && (form != nil) {
		return form, ok
	}

	// we try to fetch from cache again because when this method is called, we waited for `formQueueBackpressureTimeout`
	// already and it's likely that form was backfilled in Auth middleware meanwhile
	if formUUID, ok := ctx.Value(common.FormIDContextKey).(string); ok && len(formUUID) > 0 {
		if form, _, err := store.Impl().GetCachedFormByExternalID(ctx, formUUID); (err == nil) && (form != nil) {
			return form, true
		}
	}

	return nil, false
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

	const concurrencyLimit = 4

	sem := make(chan struct{}, concurrencyLimit)
	var wg sync.WaitGroup

	for _, submission := range batch {
		form, found := formsByID[submission.FormExternalID]
		if !found || (form == nil) {
			slog.ErrorContext(ctx, "Failed to find matching form for submission", "formID", submission.FormExternalID)
			continue
		}

		if !form.Enabled || !form.Active {
			slog.WarnContext(ctx, "Skipping inactive or disabled form", "formID", form.ID)
			continue
		}

		if (s.Auth != nil) && (s.Auth.Limiter != nil) && (form.OrgOwnerID.Valid) {
			if softRestriction, err := s.Auth.Limiter.EvaluateFormAccess(ctx, form.OrgOwnerID.Int32); (err == nil) && !softRestriction {
				slog.WarnContext(ctx, "Skipping submission due to user access", "formID", form.ID, "userID", form.OrgOwnerID.Int32)
				continue
			}
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
			if err := s.processFormSubmission(ctx, f, sub); err != nil {
				slog.WarnContext(ctx, "Failed to submit the form", "formID", f.ID, "submitID", sub.ID, common.ErrAttr(err))
			}
		}(form, submission)
	}

	wg.Wait()

	// we cleanup async, but we don't expect SO many bad forms out there
	go s.FailingForms.ClearExpired(db.DefaultCacheTTL, FormBatchSize /*max items*/)

	return nil
}

func (s *Server) processFormSubmission(ctx context.Context, f *dbgen.Form, sub *FormSubmission) error {
	if count, ok := s.FailingForms.Get(f.ID); ok && (count > maxFormFailures) {
		slog.WarnContext(ctx, "Skipping form with too many submit failures", "formID", f.ID, "count", count)
		return nil
	}

	// delayed captcha check if original form was not cached at the time of request
	if sub.CaptchaSolution != nil {
		ownerSource := &formOwnerSource{Store: s.BusinessDB, Form: f}
		result, err := s.Verifier.Verify(ctx, sub.CaptchaSolution, ownerSource, sub.Time)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to verify captcha due to internal error", common.ErrAttr(err))
			return err
		}

		if !result.Success() || (result.PropertyID != ownerSource.Form.PropertyID) {
			slog.WarnContext(ctx, "Skipping form submission due to captcha verification error", "result", result.Error.String())
			return errCaptchaVerificationFailed
		}

		s.addVerifyRecord(ctx, result)
	}

	if err := s.FormURLVerifier.VerifyURL(ctx, f.URL); err != nil {
		slog.WarnContext(ctx, "Skipping unsafe form submission URL", "formID", f.ID, common.ErrAttr(err))
		return err
	}

	client := s.FormsClient
	if f.RedirectCount > 0 {
		client = common.NewFormRedirectHTTPClient(s.FormURLVerifier, f.RedirectCount)
	} else if client == nil {
		client = common.NewFormHTTPClient()
	}

	if result := SubmitFormWithRetry(ctx, client, f, sub); result.Valid {
		s.addFormSubmitRecord(ctx, f, result.ResultCode())
		return nil
	}

	return errFormSubmitFailed
}

func (s *Server) addFormSubmitRecord(ctx context.Context, form *dbgen.Form, status int8) {
	if form == nil {
		return
	}

	if status == 0 {
		s.FailingForms.Delete(form.ID)
	} else {
		_ = s.FailingForms.Inc(form.ID)
	}

	record := &common.FormSubmitRecord{
		UserID:    form.OrgOwnerID.Int32,
		OrgID:     form.OrgID.Int32,
		FormID:    form.ID,
		Timestamp: time.Now().UTC(),
		Status:    status,
	}

	timer := time.NewTimer(formSubmitLogBackpressureTimeout)
	defer timer.Stop()

	select {
	case s.FormSubmitLogChan <- record:
		// nothing
	case <-ctx.Done():
		slog.WarnContext(ctx, "Context cancelled for adding form submit record", common.ErrAttr(ctx.Err()))
	case <-timer.C:
		s.Metrics.ObserveEventDropped(common.FormLogEventType)
	}
}

func SubmitFormWithRetry(ctx context.Context, client *http.Client, form *dbgen.Form, submission *FormSubmission) *FormSubmitResult {
	b := &backoff.Backoff{
		Min:    100 * time.Millisecond,
		Max:    2 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	attempts := int(form.RetryRequestCount) + 1
	result := &FormSubmitResult{}

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "Job context cancelled while submitting form", common.ErrAttr(ctx.Err()))
				return result
			case <-time.After(b.Duration()):
			}
		}

		var err error
		result, err = submitFormOnce(ctx, client, form, submission)
		var rerr common.RetriableError
		if err != nil && errors.As(err, &rerr) {
			err = rerr.Unwrap()
			slog.WarnContext(ctx, "Failed to submit form", "formID", form.ID, "submitID", submission.ID, "attempt", attempt+1, "status", result.StatusCode, common.ErrAttr(err))
			continue
		}

		if err != nil || result.Success {
			return result
		}
	}

	slog.DebugContext(ctx, "Submitted form", "formID", form.ID, "submitID", submission.ID, "status", result.StatusCode)

	return result
}

func submitFormOnce(ctx context.Context, client *http.Client, form *dbgen.Form, submission *FormSubmission) (*FormSubmitResult, error) {
	result := &FormSubmitResult{}
	method := strings.ToUpper(string(form.Method))
	if len(method) == 0 {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, form.URL, bytes.NewBufferString(submission.Values.Encode()))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create form submission request", "formID", form.ID, "submitID", submission.ID, common.ErrAttr(err))
		return result, err
	}

	req.Close = true

	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	if len(submission.ID) > 0 {
		req.Header.Set(common.HeaderFormSubmissionID, submission.ID)
	}
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

	result.Valid = true
	resp, err := client.Do(req)
	if err != nil {
		return result, common.NewRetriableError(err)
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	if (resp.StatusCode >= http.StatusOK) && (resp.StatusCode < http.StatusMultipleChoices) {
		result.Success = true
		return result, nil
	}

	if (resp.StatusCode >= http.StatusInternalServerError) ||
		(resp.StatusCode == http.StatusTooManyRequests) ||
		(resp.StatusCode == http.StatusRequestTimeout) ||
		(resp.StatusCode == http.StatusTooEarly) {
		return result, common.NewRetriableError(errFormSubmitFailed)
	}

	slog.WarnContext(ctx, "Form submission endpoint returned non-success status", "formID", form.ID, "submitID", submission.ID, "status", resp.StatusCode)

	return result, errFormSubmitFailed
}
