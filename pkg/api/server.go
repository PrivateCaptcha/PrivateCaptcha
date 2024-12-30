package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	errNoop            = errors.New("nothing to do")
	errIPNotFound      = errors.New("no valid IP address found")
	errAPIKeyNotSet    = errors.New("API key is not set in context")
)

type server struct {
	stage           string
	businessDB      *db.BusinessStore
	timeSeries      *db.TimeSeriesStore
	levels          *difficulty.Levels
	auth            *authMiddleware
	uaKey           [64]byte
	salt            []byte
	verifyLogChan   chan *common.VerifyRecord
	verifyLogCancel context.CancelFunc
	paddleAPI       billing.PaddleAPI
	cors            *cors.Cors
	metrics         monitoring.Metrics
}

var _ puzzle.Verifier = (*server)(nil)

func NewServer(store *db.BusinessStore,
	timeSeries *db.TimeSeriesStore,
	auth *authMiddleware,
	verifyFlushInterval time.Duration,
	paddleAPI billing.PaddleAPI,
	metrics monitoring.Metrics,
	getenv func(string) string) *server {
	srv := &server{
		stage:         getenv("STAGE"),
		businessDB:    store,
		timeSeries:    timeSeries,
		auth:          auth,
		verifyLogChan: make(chan *common.VerifyRecord, 3*verifyBatchSize/2),
		salt:          []byte(getenv("API_SALT")),
		paddleAPI:     paddleAPI,
		metrics:       metrics,
	}

	srv.levels = difficulty.NewLevelsEx(timeSeries, levelsBatchSize, propertyBucketSize,
		2*time.Second /*access log interval*/, propertyBucketSize /*backfill interval*/, nil /*cleanup callback*/)

	if byteArray, err := hex.DecodeString(getenv("UA_KEY")); (err == nil) && (len(byteArray) == 64) {
		copy(srv.uaKey[:], byteArray[:])
	} else {
		slog.Error("Error initializing UA key for server", common.ErrAttr(err), "size", len(byteArray))
	}

	var cancelVerifyCtx context.Context
	cancelVerifyCtx, srv.verifyLogCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "flush_verify_log"))
	go srv.flushVerifyLog(cancelVerifyCtx, verifyFlushInterval)

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
	Success    bool                 `json:"success"`
	ErrorCodes []puzzle.VerifyError `json:"error-codes,omitempty"`
	// other fields from Recaptcha - unclear if we need them
	// ChallengeTS common.JSONTime       `json:"challenge_ts"`
	// Hostname    string                `json:"hostname"`
}

func (s *server) Setup(router *http.ServeMux, domain string, verbose bool) {
	corsOpts := cors.Options{
		// NOTE: due to the implementation of rs/cors, we need not to set "*" as AllowOrigin as this will ruin the response
		// (in case of "*" allowed origin, response contains the same, while we want to restrict the response to domain)
		AllowOriginFunc:     s.auth.originAllowed,
		AllowedHeaders:      []string{common.HeaderCaptchaVersion, "accept", "content-type", "x-requested-with"},
		AllowedMethods:      []string{http.MethodGet},
		AllowPrivateNetwork: true,
		Debug:               verbose,
		MaxAge:              60, /*seconds*/
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
	publicChain := alice.New(common.Recovered, s.auth.EdgeVerify(domain), s.metrics.Handler)
	puzzleBase := publicChain.Append(corsHandler)
	puzzleChain := puzzleBase.Append(s.auth.Sitekey)
	verifyChain := publicChain.Append(s.auth.APIKey)
	// NOTE: auth middleware provides rate limiting internally
	router.Handle(http.MethodGet+" "+prefix+common.PuzzleEndpoint, puzzleChain.ThenFunc(s.puzzle))
	router.Handle(http.MethodOptions+" "+prefix+common.PuzzleEndpoint, puzzleBase.ThenFunc(s.puzzlePreFlight))
	router.Handle(http.MethodPost+" "+prefix+common.VerifyEndpoint, verifyChain.Then(http.MaxBytesHandler(http.HandlerFunc(s.verifyHandler), maxSolutionsBodySize)))

	maxBytesHandler := func(next http.Handler) http.Handler {
		return http.MaxBytesHandler(next, maxPaddleBodySize)
	}
	paddleChain := alice.New(common.Recovered, s.metrics.Handler, s.auth.EdgeVerify(domain), s.auth.Private, monitoring.Logged, maxBytesHandler)
	router.Handle(http.MethodPost+" "+prefix+common.PaddleSubscriptionCreated, paddleChain.ThenFunc(s.subscriptionCreated))
	router.Handle(http.MethodPost+" "+prefix+common.PaddleSubscriptionUpdated, paddleChain.ThenFunc(s.subscriptionUpdated))
	router.Handle(http.MethodPost+" "+prefix+common.PaddleSubscriptionCancelled, paddleChain.ThenFunc(s.subscriptionCancelled))
}

func (s *server) puzzleForRequest(r *http.Request) (*puzzle.Puzzle, error) {
	puzzle, err := puzzle.NewPuzzle()
	if err != nil {
		return nil, err
	}

	ctx := r.Context()
	property, ok := ctx.Value(common.PropertyContextKey).(*dbgen.Property)
	// property will not be cached for auth.backfillDelay and we return an "average" puzzle instead
	// this is done in order to not check the DB on the hot path (decrease attack surface)
	if !ok {
		sitekey := ctx.Value(common.SitekeyContextKey).(string)
		uuid := db.UUIDFromSiteKey(sitekey)
		// if it's a legit request, then puzzle will be also legit (verifiable) with this PropertyID
		puzzle.PropertyID = uuid.Bytes
		puzzle.Difficulty = difficulty.LevelMedium
		slog.WarnContext(ctx, "Returning stub puzzle before auth is backfilled", "sitekey", sitekey,
			"difficulty", puzzle.Difficulty)
		return puzzle, nil
	}

	puzzle.PropertyID = property.ExternalID.Bytes

	var fingerprint common.TFingerprint
	hash, err := blake2b.New256(s.uaKey[:])
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create blake2b hmac", common.ErrAttr(err))
		// TODO: handle calculating hash with error
		fingerprint = common.RandomFingerprint()
	} else {
		hash.Write([]byte(r.UserAgent()))
		if ip, ok := ctx.Value(common.RateLimitKeyContextKey).(string); ok && (len(ip) > 0) {
			hash.Write([]byte(ip))
		}
		hash.Write([]byte(property.Domain))
		hmac := hash.Sum(nil)
		truncatedHmac := hmac[:8]
		fingerprint = binary.BigEndian.Uint64(truncatedHmac)
	}

	tnow := time.Now()
	puzzle.Difficulty = s.levels.Difficulty(fingerprint, property, tnow)

	slog.DebugContext(ctx, "Prepared new puzzle", "propertyID", property.ID, "difficulty", puzzle.Difficulty)

	return puzzle, nil
}

func (s *server) puzzlePreFlight(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) puzzle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	puzzle, err := s.puzzleForRequest(r)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create puzzle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	puzzleBytes, err := puzzle.MarshalBinary()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize puzzle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hasher := hmac.New(sha1.New, s.salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(werr))
	}

	hash := hasher.Sum(nil)
	encodedPuzzle := base64.StdEncoding.EncodeToString(puzzleBytes)
	encodedHash := base64.StdEncoding.EncodeToString(hash)
	response := []byte(fmt.Sprintf("%s.%s", encodedPuzzle, encodedHash))

	common.WriteNoCache(w)
	w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
	w.WriteHeader(http.StatusOK)
	if _, werr := w.Write(response); werr != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle response", common.ErrAttr(werr))
	}
}

func (s *server) sendVerifyErrors(ctx context.Context, w http.ResponseWriter, errors ...puzzle.VerifyError) {
	response := &verifyResponse{
		Success:    false,
		ErrorCodes: errors,
	}

	common.SendJSONResponse(ctx, w, response, map[string]string{})
}

func (s *server) Verify(ctx context.Context, payload string, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (puzzle.VerifyError, error) {
	solutionsData, puzzleBytes, err := puzzle.ParseSolutions(ctx, payload, s.salt)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse puzzle", common.ErrAttr(err))
		return puzzle.ParseResponseError, nil
	}

	puzzleObject, property, verr := s.verifyPuzzleValid(ctx, puzzleBytes, expectedOwner, tnow)
	if verr != puzzle.VerifyNoError && verr != puzzle.MaintenanceModeError {
		return verr, nil
	}

	if serr := puzzle.VerifySolutions(ctx, puzzleObject, puzzleBytes, solutionsData); serr != puzzle.VerifyNoError {
		s.addVerifyRecord(ctx, puzzleObject, property, serr)
		return serr, nil
	}

	if puzzleObject != nil {
		if cerr := s.businessDB.CachePuzzle(ctx, puzzleObject, tnow); cerr != nil {
			slog.ErrorContext(ctx, "Failed to cache puzzle", common.ErrAttr(cerr))
		}
	}

	s.addVerifyRecord(ctx, puzzleObject, property, puzzle.VerifyNoError)

	return verr, nil
}

func (s *server) verifyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := r.FormValue(common.ParamResponse)

	verr, err := s.Verify(ctx, data, &apiKeyOwnerSource{}, time.Now().UTC())
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if (verr != puzzle.VerifyNoError) && (verr != puzzle.MaintenanceModeError) {
		s.sendVerifyErrors(ctx, w, verr)
		return
	}

	response := &verifyResponse{Success: true}
	if verr == puzzle.MaintenanceModeError {
		response.ErrorCodes = []puzzle.VerifyError{puzzle.MaintenanceModeError}
	}

	common.SendJSONResponse(ctx, w, response, common.NoCacheHeaders)
}

func (s *server) addVerifyRecord(ctx context.Context, p *puzzle.Puzzle, property *dbgen.Property, verr puzzle.VerifyError) {
	if (p == nil) || (property == nil) {
		slog.ErrorContext(ctx, "Invalid input for verify record")
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
}

func (s *server) verifyPuzzleValid(ctx context.Context, puzzleBytes []byte, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.Puzzle, *dbgen.Property, puzzle.VerifyError) {
	p := new(puzzle.Puzzle)

	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", common.ErrAttr(uerr))
		return nil, nil, puzzle.ParseResponseError
	}

	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Puzzle is expired", "expiration", p.Expiration, "now", tnow)
		return p, nil, puzzle.PuzzleExpiredError
	}

	if s.businessDB.CheckPuzzleCached(ctx, p) {
		slog.WarnContext(ctx, "Puzzle is already cached", "puzzleID", p.PuzzleIDString())
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
			slog.ErrorContext(ctx, "Failed to find property by sitekey", "sitekey", sitekey, common.ErrAttr(err))
			return p, nil, puzzle.VerifyErrorOther
		}
	}

	property := properties[0]

	if ownerID, err := expectedOwner.OwnerID(ctx); err == nil {
		if property.OrgOwnerID.Int32 != ownerID {
			slog.WarnContext(ctx, "Org owner does not match expected owner", "expected_owner", ownerID,
				"org_owner", property.OrgOwnerID.Int32)
			return p, property, puzzle.WrongOwnerError
		}
	} else {
		slog.ErrorContext(ctx, "Failed to fetch owner ID", common.ErrAttr(err))
	}

	return p, property, puzzle.VerifyNoError
}

func (s *server) flushVerifyLog(ctx context.Context, delay time.Duration) {
	var batch []*common.VerifyRecord
	slog.DebugContext(ctx, "Processing verify log", "interval", delay.String())

	for running := true; running; {
		if len(batch) > maxVerifyBatchSize {
			slog.ErrorContext(ctx, "Dropping pending verify log due to errors", "count", len(batch))
			batch = []*common.VerifyRecord{}
		}

		select {
		case <-ctx.Done():
			running = false

		case vr, ok := <-s.verifyLogChan:
			if !ok {
				running = false
				break
			}

			batch = append(batch, vr)

			if len(batch) >= verifyBatchSize {
				if err := s.timeSeries.WriteVerifyLogBatch(ctx, batch); err == nil {
					batch = []*common.VerifyRecord{}
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				if err := s.timeSeries.WriteVerifyLogBatch(ctx, batch); err == nil {
					batch = []*common.VerifyRecord{}
				}
			}
		}
	}

	slog.InfoContext(ctx, "Finished processing verify log")
}
