package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/justinas/alice"
	"github.com/rs/cors"
	"golang.org/x/crypto/blake2b"
)

const (
	maxSolutionsBodySize  = 256 * 1024
	maxPaddleBodySize     = 10 * 1024
	verifyBatchSize       = 100
	propertyBucketSize    = 5 * time.Minute
	levelsBatchSize       = 100
	updateLimitsBatchSize = 100
	maxVerifyBatchSize    = 100_000
)

var (
	errProductNotFound = errors.New("product not found")
	errAPIKeyNotSet    = errors.New("API key is not set in context")
	headersAnyOrigin   = map[string][]string{
		http.CanonicalHeaderKey(common.HeaderAccessControlOrigin): []string{"*"},
		http.CanonicalHeaderKey(common.HeaderAccessControlAge):    []string{"86400"},
	}
	headersContentPlain = map[string][]string{
		http.CanonicalHeaderKey(common.HeaderContentType): []string{common.ContentTypePlain},
	}
)

type server struct {
	stage           string
	businessDB      *db.BusinessStore
	timeSeries      *db.TimeSeriesStore
	levels          *difficulty.Levels
	auth            *authMiddleware
	uaKey           *userFingerprintKey
	salt            *puzzleSalt
	verifyLogChan   chan *common.VerifyRecord
	verifyLogCancel context.CancelFunc
	paddleAPI       billing.PaddleAPI
	cors            *cors.Cors
	metrics         monitoring.Metrics
	mailer          common.Mailer
	testPuzzleData  *puzzle.PuzzlePayload
}

var _ puzzle.Engine = (*server)(nil)

func NewServer(store *db.BusinessStore,
	timeSeries *db.TimeSeriesStore,
	auth *authMiddleware,
	verifyFlushInterval time.Duration,
	paddleAPI billing.PaddleAPI,
	metrics monitoring.Metrics,
	mailer common.Mailer,
	cfg common.ConfigStore) *server {

	srv := &server{
		stage:         cfg.Get(common.StageKey).Value(),
		businessDB:    store,
		timeSeries:    timeSeries,
		auth:          auth,
		verifyLogChan: make(chan *common.VerifyRecord, 10*verifyBatchSize),
		salt:          newPuzzleSalt(cfg.Get(common.APISaltKey)),
		uaKey:         newUserFingerprintKey(cfg.Get(common.UserFingerprintIVKey)),
		paddleAPI:     paddleAPI,
		metrics:       metrics,
		mailer:        mailer,
	}

	srv.levels = difficulty.NewLevelsEx(timeSeries, levelsBatchSize, propertyBucketSize,
		2*time.Second /*access log interval*/, propertyBucketSize /*backfill interval*/)

	var cancelVerifyCtx context.Context
	cancelVerifyCtx, srv.verifyLogCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "flush_verify_log"))

	go common.ProcessBatchArray(cancelVerifyCtx, srv.verifyLogChan, verifyFlushInterval, verifyBatchSize, maxVerifyBatchSize, srv.timeSeries.WriteVerifyLogBatch)

	return srv
}

type apiKeyOwnerSource struct{}

func (a *apiKeyOwnerSource) OwnerID(ctx context.Context) (int32, error) {
	apiKey, ok := ctx.Value(common.APIKeyContextKey).(*dbgen.APIKey)
	if !ok {
		return -1, errAPIKeyNotSet
	}

	return apiKey.UserID.Int32, nil
}

type verifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

type verifyResponseRecaptchaV2 struct {
	verifyResponse
	ChallengeTS common.JSONTime `json:"challenge_ts"`
	Hostname    string          `json:"hostname"`
}

type verifyResponseRecaptchaV3 struct {
	verifyResponseRecaptchaV2
	Score  float64 `json:"score"`
	Action string  `json:"action"`
}

func (s *server) Init(ctx context.Context) error {
	if err := s.salt.Update(); err != nil {
		slog.ErrorContext(ctx, "Failed to update puzzle salt", common.ErrAttr(err))
		return err
	}

	if err := s.uaKey.Update(); err != nil {
		slog.ErrorContext(ctx, "Failed to update user fingerprint key", common.ErrAttr(err))
		return err
	}

	testPuzzle := puzzle.NewPuzzle(0 /*puzzle ID*/, db.TestPropertyUUID.Bytes, 0 /*difficulty*/)
	var err error
	s.testPuzzleData, err = testPuzzle.Serialize(ctx, s.salt.Value(), nil /*property salt*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize test puzzle", common.ErrAttr(err))
		return err
	}

	return nil
}

func (s *server) Setup(router *http.ServeMux, domain string, verbose bool) {
	corsOpts := cors.Options{
		// NOTE: due to the implementation of rs/cors, we need not to set "*" as AllowOrigin as this will ruin the response
		// (in case of "*" allowed origin, response contains the same, while we want to restrict the response to domain)
		AllowOriginVaryRequestFunc: s.auth.originAllowed,
		AllowedHeaders:             []string{common.HeaderCaptchaVersion, "accept", "content-type", "x-requested-with"},
		AllowedMethods:             []string{http.MethodGet},
		AllowPrivateNetwork:        true,
		OptionsPassthrough:         true,
		Debug:                      verbose,
		MaxAge:                     60 * 60, /*seconds*/
	}

	if corsOpts.Debug {
		corsOpts.Logger = &common.FmtLogger{Ctx: common.TraceContext(context.TODO(), "cors"), Level: common.LevelTrace}
	}

	s.cors = cors.New(corsOpts)

	s.setupWithPrefix(domain, router, s.cors.Handler)
}

func (s *server) Shutdown() {
	s.levels.Shutdown()

	slog.Debug("Shutting down API server routines")
	s.verifyLogCancel()
	close(s.verifyLogChan)
}

func (s *server) setupWithPrefix(domain string, router *http.ServeMux, corsHandler alice.Constructor) {
	prefix := domain + "/"
	slog.Debug("Setting up the API routes", "prefix", prefix)
	publicChain := alice.New(common.Recovered, monitoring.Traced, s.auth.EdgeVerify(domain), s.metrics.Handler)
	// NOTE: auth middleware provides rate limiting internally
	router.Handle(http.MethodGet+" "+prefix+common.PuzzleEndpoint, publicChain.Append(corsHandler, common.TimeoutHandler(1*time.Second), s.auth.Sitekey).ThenFunc(s.puzzleHandler))
	router.Handle(http.MethodOptions+" "+prefix+common.PuzzleEndpoint, publicChain.Append(common.Cached, corsHandler, s.auth.SitekeyOptions).ThenFunc(s.puzzlePreFlight))
	verifyChain := publicChain.Append(common.TimeoutHandler(5*time.Second), s.auth.APIKey)
	router.Handle(http.MethodPost+" "+prefix+common.VerifyEndpoint, verifyChain.Then(http.MaxBytesHandler(http.HandlerFunc(s.verifyHandler), maxSolutionsBodySize)))

	maxBytesHandler := func(next http.Handler) http.Handler {
		return http.MaxBytesHandler(next, maxPaddleBodySize)
	}
	// in almost all Paddle handlers we make external http requests, hence larger timeout
	paddleChain := alice.New(common.Recovered, s.metrics.Handler, s.auth.EdgeVerify(domain), s.auth.Private, monitoring.Logged, maxBytesHandler, common.TimeoutHandler(10*time.Second))
	router.Handle(http.MethodPost+" "+prefix+common.PaddleSubscriptionCreated, paddleChain.ThenFunc(s.subscriptionCreated))
	router.Handle(http.MethodPost+" "+prefix+common.PaddleSubscriptionUpdated, paddleChain.ThenFunc(s.subscriptionUpdated))
	router.Handle(http.MethodPost+" "+prefix+common.PaddleSubscriptionCancelled, paddleChain.ThenFunc(s.subscriptionCancelled))
	// "root" access
	router.Handle(http.MethodGet+" "+prefix+"{$}", publicChain.Then(common.HttpStatus(http.StatusForbidden)))
}

func (s *server) puzzleForRequest(r *http.Request) (*puzzle.Puzzle, *dbgen.Property, error) {
	ctx := r.Context()
	property, ok := ctx.Value(common.PropertyContextKey).(*dbgen.Property)
	// property will not be cached for auth.backfillDelay and we return an "average" puzzle instead
	// this is done in order to not check the DB on the hot path (decrease attack surface)
	if !ok {
		sitekey := ctx.Value(common.SitekeyContextKey).(string)
		if sitekey == db.TestPropertySitekey {
			return nil, nil, db.ErrTestProperty
		}

		uuid := db.UUIDFromSiteKey(sitekey)
		// if it's a legit request, then puzzle will be also legit (verifiable) with this PropertyID
		stubPuzzle := puzzle.NewPuzzle(0 /*puzzle ID*/, uuid.Bytes, uint8(common.DifficultyLevelMedium))
		if err := stubPuzzle.Init(puzzle.DefaultValidityPeriod); err != nil {
			slog.ErrorContext(ctx, "Failed to init stub puzzle", common.ErrAttr(err))
		}

		slog.Log(ctx, common.LevelTrace, "Returning stub puzzle before auth is backfilled", "puzzleID", stubPuzzle.PuzzleID,
			"sitekey", sitekey, "difficulty", stubPuzzle.Difficulty)
		return stubPuzzle, nil, nil
	}

	var fingerprint common.TFingerprint
	hash, err := blake2b.New256(s.uaKey.Value())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create blake2b hmac", common.ErrAttr(err))
		fingerprint = common.RandomFingerprint()
	} else {
		// TODO: Check if we really need to take user agent into account here
		// or it should be accounted on the anomaly detection side (user-agent is trivial to spoof)
		// hash.Write([]byte(r.UserAgent()))
		if ip, ok := ctx.Value(common.RateLimitKeyContextKey).(netip.Addr); ok && ip.IsValid() {
			hash.Write(ip.AsSlice())
		} else {
			slog.ErrorContext(ctx, "Rate limit context key type mismatch", "ip", ip)
			hash.Write([]byte(r.RemoteAddr))
		}
		hmac := hash.Sum(nil)
		truncatedHmac := hmac[:8]
		fingerprint = binary.BigEndian.Uint64(truncatedHmac)
	}

	tnow := time.Now()
	puzzleDifficulty := s.levels.Difficulty(fingerprint, property, tnow)

	puzzleID := puzzle.RandomPuzzleID()
	result := puzzle.NewPuzzle(puzzleID, property.ExternalID.Bytes, puzzleDifficulty)
	if err := result.Init(property.ValidityInterval); err != nil {
		slog.ErrorContext(ctx, "Failed to init puzzle", common.ErrAttr(err))
	}

	slog.Log(ctx, common.LevelTrace, "Prepared new puzzle", "propertyID", property.ID, "difficulty", result.Difficulty,
		"puzzleID", result.PuzzleID, "userID", property.OrgOwnerID.Int32)

	return result, property, nil
}

func (s *server) puzzlePreFlight(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// the reason for this is that we intend to cache test property responses
	if sitekey, ok := ctx.Value(common.SitekeyContextKey).(string); ok && (sitekey == db.TestPropertySitekey) {
		common.WriteHeaders(w, headersAnyOrigin)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *server) puzzleHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	puzzle, property, err := s.puzzleForRequest(r)
	if err != nil {
		if err == db.ErrTestProperty {
			common.WriteHeaders(w, common.CachedHeaders)
			// we cache test property responses, can as well allow them anywhere
			common.WriteHeaders(w, headersAnyOrigin)
			common.WriteHeaders(w, headersContentPlain)
			_ = s.testPuzzleData.Write(w)
			return
		}

		slog.ErrorContext(ctx, "Failed to create puzzle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var extraSalt []byte
	var userID int32 = -1
	if property != nil {
		userID = property.OrgOwnerID.Int32
		extraSalt = property.Salt
	}

	if err := s.Write(ctx, puzzle, extraSalt, w); err != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle", common.ErrAttr(err))
	}

	s.metrics.ObservePuzzleCreated(userID)
}

func (s *server) Write(ctx context.Context, p *puzzle.Puzzle, extraSalt []byte, w http.ResponseWriter) error {
	payload, err := p.Serialize(ctx, s.salt.Value(), extraSalt)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return err
	}

	common.WriteHeaders(w, common.NoCacheHeaders)
	common.WriteHeaders(w, headersContentPlain)
	return payload.Write(w)
}

func (s *server) Verify(ctx context.Context, payload string, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.Puzzle, puzzle.VerifyError, error) {
	verifyPayload, err := puzzle.ParseVerifyPayload(ctx, payload)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse verify payload", common.ErrAttr(err))
		return nil, puzzle.ParseResponseError, nil
	}

	puzzleObject, property, perr := s.verifyPuzzleValid(ctx, verifyPayload, expectedOwner, tnow)
	if perr != puzzle.VerifyNoError && perr != puzzle.MaintenanceModeError {
		return puzzleObject, perr, nil
	}

	if metadata, verr := verifyPayload.VerifySolutions(ctx); verr != puzzle.VerifyNoError {
		// NOTE: unlike solutions/puzzle, diagnostics bytes can be totally tampered
		slog.WarnContext(ctx, "Failed to verify solutions", "result", verr.String(), "clientError", metadata.ErrorCode(),
			"elapsedMillis", metadata.ElapsedMillis(), "puzzleID", puzzleObject.PuzzleID, "userID", property.OrgOwnerID.Int32,
			"propertyID", property.ID)

		s.addVerifyRecord(ctx, puzzleObject, property, verr)
		return puzzleObject, verr, nil
	}

	if (puzzleObject != nil) && (property != nil) && !property.AllowReplay {
		if cerr := s.businessDB.CachePuzzle(ctx, puzzleObject, tnow); cerr != nil {
			slog.ErrorContext(ctx, "Failed to cache puzzle", common.ErrAttr(cerr))
		}
	}

	s.addVerifyRecord(ctx, puzzleObject, property, puzzle.VerifyNoError)

	return puzzleObject, perr, nil
}

func (s *server) verifyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	p, verr, err := s.Verify(ctx, string(data), &apiKeyOwnerSource{}, time.Now().UTC())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	errorCodes := []puzzle.VerifyError{}
	if verr != puzzle.VerifyNoError {
		errorCodes = append(errorCodes, verr)
	}

	vr2 := &verifyResponseRecaptchaV2{
		verifyResponse: verifyResponse{
			Success: (verr == puzzle.VerifyNoError) ||
				(verr == puzzle.MaintenanceModeError) ||
				(verr == puzzle.TestPropertyError),
			ErrorCodes: puzzle.ErrorCodesToStrings(errorCodes),
		},
	}

	if p != nil && !p.IsZero() {
		vr2.ChallengeTS = common.JSONTime(p.Expiration.Add(-puzzle.DefaultValidityPeriod))

		sitekey := db.UUIDToSiteKey(pgtype.UUID{Valid: true, Bytes: p.PropertyID})
		if property, err := s.businessDB.GetCachedPropertyBySitekey(ctx, sitekey); err == nil {
			vr2.Hostname = property.Domain
		}
	}

	var result interface{}

	recaptchaCompatVersion := r.Header.Get(common.HeaderCaptchaCompat)
	if recaptchaCompatVersion == "rcV3" {
		result = &verifyResponseRecaptchaV3{
			verifyResponseRecaptchaV2: *vr2,
			Action:                    "",
			Score:                     0.5,
		}
	} else {
		result = vr2
	}

	common.SendJSONResponse(ctx, w, result, common.NoCacheHeaders)
}

func (s *server) addVerifyRecord(ctx context.Context, p *puzzle.Puzzle, property *dbgen.Property, verr puzzle.VerifyError) {
	if (p == nil) || (property == nil) {
		slog.ErrorContext(ctx, "Invalid input for verify record", "property", (property != nil), "puzzle", (p != nil))
		return
	}

	vr := &common.VerifyRecord{
		UserID:     property.OrgOwnerID.Int32,
		OrgID:      property.OrgID.Int32,
		PropertyID: property.ID,
		PuzzleID:   p.PuzzleID,
		Timestamp:  time.Now().UTC(),
		Status:     int8(verr),
	}

	s.verifyLogChan <- vr

	s.metrics.ObservePuzzleVerified(vr.UserID, verr, p.IsStub())
}

func (s *server) verifyPuzzleValid(ctx context.Context, payload *puzzle.VerifyPayload, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.Puzzle, *dbgen.Property, puzzle.VerifyError) {
	p := payload.Puzzle()
	plog := slog.With("puzzleID", p.PuzzleID)

	if p.IsZero() && bytes.Equal(p.PropertyID[:], db.TestPropertyUUID.Bytes[:]) {
		plog.DebugContext(ctx, "Verifying test puzzle")
		return p, nil, puzzle.TestPropertyError
	}

	if !tnow.Before(p.Expiration) {
		plog.WarnContext(ctx, "Puzzle is expired", "expiration", p.Expiration, "now", tnow)
		return p, nil, puzzle.PuzzleExpiredError
	}

	if !payload.NeedsExtraSalt() {
		if serr := payload.VerifySignature(ctx, s.salt.Value(), nil /*extra salt*/); serr != nil {
			return p, nil, puzzle.IntegrityError
		}
	}

	if s.businessDB.CheckPuzzleCached(ctx, p) {
		plog.WarnContext(ctx, "Puzzle is already cached")
		return p, nil, puzzle.VerifiedBeforeError
	}

	sitekey := db.UUIDToSiteKey(pgtype.UUID{Valid: true, Bytes: p.PropertyID})
	properties, err := s.businessDB.RetrievePropertiesBySitekey(ctx, map[string]struct{}{sitekey: {}})
	if (err != nil) || (len(properties) != 1) {
		switch err {
		case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
			return p, nil, puzzle.InvalidPropertyError
		case db.ErrMaintenance:
			return p, nil, puzzle.MaintenanceModeError
		default:
			plog.ErrorContext(ctx, "Failed to find property by sitekey", "sitekey", sitekey, common.ErrAttr(err))
			return p, nil, puzzle.VerifyErrorOther
		}
	}

	property := properties[0]
	if payload.NeedsExtraSalt() {
		if serr := payload.VerifySignature(ctx, s.salt.Value(), property.Salt); serr != nil {
			return p, nil, puzzle.IntegrityError
		}
	}

	if ownerID, err := expectedOwner.OwnerID(ctx); err == nil {
		if property.OrgOwnerID.Int32 != ownerID {
			plog.WarnContext(ctx, "Org owner does not match expected owner", "expectedOwner", ownerID,
				"orgOwner", property.OrgOwnerID.Int32)
			return p, property, puzzle.WrongOwnerError
		}
	} else {
		plog.ErrorContext(ctx, "Failed to fetch owner ID", common.ErrAttr(err))
	}

	return p, property, puzzle.VerifyNoError
}
