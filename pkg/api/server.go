package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/justinas/alice"
	"github.com/rs/cors"
)

const (
	maxSolutionsBodySize  = 256 * 1024
	VerifyBatchSize       = 100
	PropertyBucketSize    = 5 * time.Minute
	updateLimitsBatchSize = 100
	maxVerifyBatchSize    = 100_000
	ApiService            = "api"
)

var (
	errAPIKeyNotSet   = errors.New("API key is not set in context")
	errInvalidAPIKey  = errors.New("API key is not valid")
	errAPIKeyScope    = errors.New("API key scope is not valid")
	errAPIKeyReadOnly = errors.New("API key read-write mode mismatch")
	errPuzzleOwner    = errors.New("error fetching puzzle owner")
	errInvalidArg     = errors.New("invalid arguments")
	errTestSolutions  = errors.New("invalid test solutions")
	headersAnyOrigin  = map[string][]string{
		http.CanonicalHeaderKey(common.HeaderAccessControlOrigin): []string{"*"},
		http.CanonicalHeaderKey(common.HeaderAccessControlAge):    []string{"86400"},
	}
	headersContentPlain = map[string][]string{
		http.CanonicalHeaderKey(common.HeaderContentType): []string{common.ContentTypePlain},
	}
	invalidPropertyResponse          []byte
	invalidPropertyRecaptchaResponse []byte
)

func init() {
	var err error
	invalidPropertyResponse, err = json.Marshal(&VerificationResponse{
		Success: false,
		Code:    puzzle.InvalidPropertyError,
	})
	if err != nil {
		panic(err)
	}

	invalidPropertyRecaptchaResponse, err = json.Marshal(&VerifyResponseRecaptchaV2{
		Success:    false,
		ErrorCodes: []string{puzzle.InvalidPropertyError.String()},
	})
	if err != nil {
		panic(err)
	}
}

type Server struct {
	APIHeaders         map[string][]string
	Stage              string
	BusinessDB         db.Implementor
	TimeSeries         common.TimeSeriesStore
	Levels             *difficulty.Levels
	Auth               *AuthMiddleware
	VerifyLogChan      chan *common.VerifyRecord
	VerifyLogCancel    context.CancelFunc
	Cors               *cors.Cors
	Metrics            common.APIMetrics
	Mailer             common.Mailer
	RateLimiter        ratelimit.HTTPRateLimiter
	Verifier           *Verifier
	SubscriptionLimits db.SubscriptionLimits
	IDHasher           common.IdentifierHasher
	AsyncTasks         db.AsyncTasks
}

type apiKeyOwnerSource struct {
	Store     db.Implementor
	cachedKey *dbgen.APIKey
	scope     dbgen.ApiKeyScope
}

var _ puzzle.OwnerIDSource = (*apiKeyOwnerSource)(nil)

func (a *apiKeyOwnerSource) apiKey(ctx context.Context) (*dbgen.APIKey, error) {
	if apiKey, ok := ctx.Value(common.APIKeyContextKey).(*dbgen.APIKey); ok && (apiKey != nil) {
		a.cachedKey = apiKey
		return apiKey, nil
	}

	if secret, ok := ctx.Value(common.SecretContextKey).(string); ok && (len(secret) > 0) {
		// this is the "postponed" DB access mentioned in APIKey() middleware
		// NOTE: here we do NOT verify user's subscription validity, it's done only in middleware
		key, err := a.Store.Impl().RetrieveAPIKey(ctx, secret)
		if key != nil {
			a.cachedKey = key
		}
		return key, err
	}

	return nil, errAPIKeyNotSet
}

func (a *apiKeyOwnerSource) OwnerID(ctx context.Context, tnow time.Time) (int32, *int32, error) {
	apiKey, err := a.apiKey(ctx)
	if err != nil {
		if (err == db.ErrSetMissing) || (err == db.ErrNegativeCacheHit) {
			return -1, nil, errInvalidAPIKey
		}
		return -1, nil, err
	}

	if !isAPIKeyValid(ctx, apiKey, tnow) {
		return -1, nil, errInvalidAPIKey
	}

	if apiKey.Scope != a.scope {
		return -1, nil, errAPIKeyScope
	}

	var orgID *int32
	if apiKey.OrgID.Valid {
		orgID = new(int32)
		*orgID = apiKey.OrgID.Int32
	}

	return apiKey.UserID.Int32, orgID, nil
}

type VerificationResponse struct {
	Success   bool               `json:"success"`
	Code      puzzle.VerifyError `json:"code"`
	Origin    string             `json:"origin,omitempty"`
	Timestamp common.JSONTime    `json:"timestamp,omitempty"`
}

type VerifyResponseRecaptchaV2 struct {
	Success     bool            `json:"success"`
	ErrorCodes  []string        `json:"error-codes,omitempty"`
	ChallengeTS common.JSONTime `json:"challenge_ts"`
	Hostname    string          `json:"hostname"`
}

type VerifyResponseRecaptchaV3 struct {
	VerifyResponseRecaptchaV2
	Score  float64 `json:"score"`
	Action string  `json:"action"`
}

func (s *Server) Init(ctx context.Context, verifyFlushInterval, authBackfillDelay time.Duration) error {
	s.APIHeaders = make(map[string][]string)

	if err := s.Verifier.Update(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to update puzzle verifier", common.ErrAttr(err))
		return err
	}

	s.Levels.Init(2*time.Second /*access log interval*/, PropertyBucketSize /*backfill interval*/)
	s.Auth.StartBackfill(authBackfillDelay)
	s.RegisterTaskHandlers(ctx)

	baseVerifyCtx := context.WithValue(context.Background(), common.ServiceContextKey, ApiService)
	var cancelVerifyCtx context.Context
	cancelVerifyCtx, s.VerifyLogCancel = context.WithCancel(context.WithValue(baseVerifyCtx, common.TraceIDContextKey, "flush_verify_log"))

	go common.ProcessBatchArray(cancelVerifyCtx, s.VerifyLogChan, verifyFlushInterval, VerifyBatchSize, maxVerifyBatchSize, s.TimeSeries.WriteVerifyLogBatch)

	return nil
}

func (s *Server) Setup(domain string, verbose bool, security alice.Constructor) *common.RouteGenerator {
	corsOpts := cors.Options{
		// NOTE: due to the implementation of rs/cors, we need not to set "*" as AllowOrigin as this will ruin the response
		// (in case of "*" allowed origin, response contains the same, while we want to restrict the response to domain)
		AllowOriginVaryRequestFunc: s.Auth.originAllowed,
		AllowedHeaders:             []string{common.HeaderCaptchaVersion, "accept", "content-type", "x-requested-with"},
		AllowedMethods:             []string{http.MethodGet},
		AllowPrivateNetwork:        true,
		OptionsPassthrough:         true,
		Debug:                      verbose,
		MaxAge:                     60 * 60, /*seconds*/
	}

	if corsOpts.Debug {
		ctx := common.TraceContext(context.TODO(), "cors")
		ctx = context.WithValue(ctx, common.ServiceContextKey, ApiService)
		corsOpts.Logger = &common.FmtLogger{Ctx: ctx, Level: common.LevelTrace}
	}

	s.Cors = cors.New(corsOpts)

	prefix := domain + "/"
	slog.Debug("Setting up the API routes", "prefix", prefix)
	rg := &common.RouteGenerator{Prefix: prefix}
	s.setupWithPrefix(rg, s.Cors.Handler, security)

	return rg
}

func (s *Server) Shutdown() {
	s.Levels.Shutdown()
	s.Auth.Shutdown()

	slog.Debug("Shutting down API server routines")
	s.VerifyLogCancel()
	close(s.VerifyLogChan)
}

func (s *Server) setupWithPrefix(rg *common.RouteGenerator, corsHandler, security alice.Constructor) {
	svc := common.ServiceMiddleware(ApiService)
	publicChain := alice.New(svc, common.Recovered, security)
	// NOTE: auth middleware provides rate limiting internally
	puzzleChain := publicChain.Append(s.Metrics.Handler, s.RateLimiter.RateLimit, monitoring.Traced, common.TimeoutHandler(1*time.Second))
	rg.Handle(rg.Get(common.PuzzleEndpoint), puzzleChain.Append(corsHandler, s.Auth.Sitekey), http.HandlerFunc(s.puzzleHandler))
	rg.Handle(rg.Options(common.PuzzleEndpoint), puzzleChain.Append(common.Cached, corsHandler, s.Auth.SitekeyOptions), http.HandlerFunc(s.puzzlePreFlight))

	const (
		// NOTE: these defaults will be adjusted per API key quota almost immediately after verifying API key
		// requests burst
		apiKeyLeakyBucketCap = 10
		// effective 0.5 rps
		apiKeyLeakInterval = 2 * time.Second
	)
	apiRateLimiter := s.RateLimiter.RateLimitExFunc(apiKeyLeakyBucketCap, apiKeyLeakInterval)

	verifyChain := publicChain.Append(s.Metrics.Handler, apiRateLimiter, monitoring.Traced, common.TimeoutHandler(5*time.Second))
	// reCAPTCHA compatibility
	// the difference from our side is _when_ we fetch API key: for reCAPTCHA it comes in form field "secret" and
	// we want to put it _behind_ the MaxBytesHandler, while for Private Captcha format (header) it can be before
	formAPIAuth := s.Auth.APIKey(formSecretAPIKey, dbgen.ApiKeyScopePuzzle)
	rg.Handle(rg.Post(common.SiteVerifyEndpoint), verifyChain, http.MaxBytesHandler(formAPIAuth(http.HandlerFunc(s.recaptchaVerifyHandler)), maxSolutionsBodySize))
	// Private Captcha format
	rg.Handle(rg.Post(common.VerifyEndpoint), verifyChain.Append(s.Auth.APIKey(headerAPIKey, dbgen.ApiKeyScopePuzzle)), http.MaxBytesHandler(http.HandlerFunc(s.pcVerifyHandler), maxSolutionsBodySize))

	s.setupEnterprise(rg, publicChain, apiRateLimiter)

	// "root" access
	rg.Handle(rg.Prefix+"{$}", publicChain.Append(s.Metrics.Handler), common.HttpStatus(http.StatusForbidden))
}

func (s *Server) puzzlePreFlight(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// the reason for this is that we intend to cache test property responses
	if sitekey, ok := ctx.Value(common.SitekeyContextKey).(string); ok && (sitekey == db.TestPropertySitekey) {
		common.WriteHeaders(w, headersAnyOrigin)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) puzzleHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	puzzle, property, err := s.Verifier.PuzzleForRequest(r, s.Levels)
	if err != nil {
		if err == db.ErrTestProperty {
			common.WriteHeaders(w, common.CachedHeaders)
			// we cache test property responses, can as well allow them anywhere
			common.WriteHeaders(w, headersAnyOrigin)
			common.WriteHeaders(w, headersContentPlain)
			_ = s.Verifier.WriteTestPuzzle(w)
			return
		}

		status := http.StatusInternalServerError
		if err == errInvalidArg {
			status = http.StatusBadRequest
		} else {
			slog.ErrorContext(ctx, "Failed to create puzzle", common.ErrAttr(err))
		}

		http.Error(w, "", status)
		return
	}

	var extraSalt []byte
	var userID int32 = -1
	if property != nil {
		userID = property.OrgOwnerID.Int32
		extraSalt = property.Salt
	}

	if err := s.Verifier.Write(ctx, puzzle, extraSalt, w); err != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle", common.ErrAttr(err))
	}

	s.Metrics.ObservePuzzleCreated(userID)
}

// reCAPTCHA format: puzzle response is in form field "response", API key is in form field "secret"
// https://developers.google.com/recaptcha/docs/verify
func (s *Server) recaptchaVerifyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read request form", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	data := r.FormValue(common.ParamResponse)
	if len(data) == 0 {
		slog.ErrorContext(ctx, "Empty captcha response")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	payload, err := s.Verifier.ParseSolutionPayload(ctx, []byte(data))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if sitekey := r.FormValue(common.ParamSiteKey); db.CanBeValidSitekey(sitekey) {
		propertyID := payload.Puzzle().PropertyID()
		if propertyExternalID := db.UUIDFromSiteKey(sitekey); !bytes.Equal(propertyExternalID.Bytes[:], propertyID[:]) {
			slog.WarnContext(ctx, "Expected property ID does not match", "expected", sitekey, "actual", hex.EncodeToString(propertyID[:]))
			common.SendReponse(ctx, w, invalidPropertyRecaptchaResponse, common.JSONContentHeaders, common.NoCacheHeaders, s.APIHeaders)
			return
		}
	}

	ownerSource := &apiKeyOwnerSource{Store: s.BusinessDB, scope: dbgen.ApiKeyScopePuzzle}
	result, err := s.Verifier.Verify(ctx, payload, ownerSource, time.Now().UTC())
	if err != nil {
		switch err {
		case errPuzzleOwner:
			// "late" auth check (we postpone API key check in case it's not cached in Auth)
			// in this case we also automatically set "API key" (or whatever is passed) as missing in cache
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		default:
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}

	if result.Valid() {
		s.addVerifyRecord(ctx, result)
	}

	if apiKey := ownerSource.cachedKey; apiKey != nil {
		// if we are not cached, then we will recheck via "delayed" mechanism of OwnerIDSource
		// when rate limiting is cleaned up (due to inactivity) we should still be able to access on defaults
		interval := float64(time.Second) / apiKey.RequestsPerSecond
		s.RateLimiter.UpdateRequestLimits(r, uint32(apiKey.RequestsBurst), time.Duration(interval))
	}

	vr2 := &VerifyResponseRecaptchaV2{
		Success:     result.Success(),
		ErrorCodes:  result.ErrorsToStrings(),
		ChallengeTS: common.JSONTime(result.CreatedAt),
		Hostname:    result.Domain,
	}

	var response interface{} = vr2
	if recaptchaCompatVersion := r.Header.Get(common.HeaderCaptchaCompat); recaptchaCompatVersion == "rcV3" {
		response = &VerifyResponseRecaptchaV3{
			VerifyResponseRecaptchaV2: *vr2,
			Action:                    "",
			Score:                     0.5,
		}
	}

	common.SendJSONResponse(r.Context(), w, response, common.NoCacheHeaders)
}

// Private Captcha format: puzzle response is the whole body, API key is in header
func (s *Server) pcVerifyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	payload, err := s.Verifier.ParseSolutionPayload(ctx, data)
	if err != nil {
		slog.Log(ctx, common.LevelTrace, "Failed to parse solution payload", common.ErrAttr(err))
		http.Error(w, "Failed to parse payload", http.StatusBadRequest)
		return
	}

	if sitekey := r.Header.Get(common.HeaderSitekey); db.CanBeValidSitekey(sitekey) {
		propertyID := payload.Puzzle().PropertyID()
		if propertyExternalID := db.UUIDFromSiteKey(sitekey); !bytes.Equal(propertyExternalID.Bytes[:], propertyID[:]) {
			slog.WarnContext(ctx, "Expected property ID does not match", "expected", sitekey, "actual", hex.EncodeToString(propertyID[:]))
			common.SendReponse(ctx, w, invalidPropertyResponse, common.JSONContentHeaders, common.NoCacheHeaders, s.APIHeaders)
			return
		}
	}

	ownerSource := &apiKeyOwnerSource{Store: s.BusinessDB, scope: dbgen.ApiKeyScopePuzzle}
	result, err := s.Verifier.Verify(ctx, payload, ownerSource, time.Now().UTC())
	if err != nil {
		switch err {
		case errPuzzleOwner:
			// "late" auth check (we postpone API key check in case it's not cached in Auth)
			// in this case we also automatically set "API key" (or whatever is passed) as missing in cache
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		default:
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}

	if result.Valid() {
		s.addVerifyRecord(ctx, result)
	}

	if apiKey := ownerSource.cachedKey; apiKey != nil {
		// if we are not cached, then we will recheck via "delayed" mechanism of OwnerIDSource
		// when rate limiting is cleaned up (due to inactivity) we should still be able to access on defaults
		interval := float64(time.Second) / apiKey.RequestsPerSecond
		s.RateLimiter.UpdateRequestLimits(r, uint32(apiKey.RequestsBurst), time.Duration(interval))
	}

	response := &VerificationResponse{
		Success:   result.Success(),
		Code:      result.Error,
		Origin:    result.Domain,
		Timestamp: common.JSONTime(result.CreatedAt),
	}

	common.SendJSONResponse(r.Context(), w, response, common.NoCacheHeaders, s.APIHeaders)
}

func (s *Server) addVerifyRecord(ctx context.Context, result *puzzle.VerifyResult) {
	vr := &common.VerifyRecord{
		UserID:     result.UserID,
		OrgID:      result.OrgID,
		PropertyID: result.PropertyID,
		PuzzleID:   result.PuzzleID,
		Timestamp:  time.Now().UTC(),
		Status:     int8(result.Error),
	}

	s.VerifyLogChan <- vr

	s.Metrics.ObservePuzzleVerified(vr.UserID, result.Error.String(), (result.PuzzleID == 0) /*is stub*/)

	// we do not record access for stub puzzles in /puzzle initially, but now they are "verified" so we can backfill
	if (result.PuzzleID == 0) && !result.CreatedAt.IsZero() {
		s.Levels.BackfillAccess(result)
	}
}

func (s *Server) ReportingVerifier() puzzle.Engine {
	return &reportingVerifier{
		verifier:   s.Verifier,
		reportFunc: s.addVerifyRecord,
	}
}

type reportingVerifier struct {
	verifier   puzzle.Engine
	reportFunc func(context.Context, *puzzle.VerifyResult)
}

var _ puzzle.Engine = (*reportingVerifier)(nil)

func (rv *reportingVerifier) Create(puzzleID uint64, propertyID [puzzle.PropertyIDSize]byte, difficulty uint8) puzzle.Puzzle {
	return rv.verifier.Create(puzzleID, propertyID, difficulty)
}
func (rv *reportingVerifier) Write(ctx context.Context, p puzzle.Puzzle, extraSalt []byte, w http.ResponseWriter) error {
	return rv.verifier.Write(ctx, p, extraSalt, w)
}
func (rv *reportingVerifier) ParseSolutionPayload(ctx context.Context, payload []byte) (puzzle.SolutionPayload, error) {
	return rv.verifier.ParseSolutionPayload(ctx, payload)
}
func (rv *reportingVerifier) Verify(ctx context.Context, payload puzzle.SolutionPayload, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.VerifyResult, error) {
	result, err := rv.verifier.Verify(ctx, payload, expectedOwner, tnow)
	if err == nil && result.Valid() {
		rv.reportFunc(ctx, result)
	}
	return result, err
}
