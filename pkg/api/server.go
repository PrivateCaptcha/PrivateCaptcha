package api

import (
	"bytes"
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
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/blake2b"
)

const (
	maxSolutionsBodySize  = 256 * 1024
	maxPaddleBodySize     = 10 * 1024
	verifyBatchSize       = 100
	propertyBucketSize    = 5 * time.Minute
	levelsBatchSize       = 100
	updateLimitsBatchSize = 100
)

var (
	errProductNotFound = errors.New("product not found")
	errNoop            = errors.New("nothing to do")
)

type server struct {
	stage             string
	businessDB        *db.BusinessStore
	timeSeries        *db.TimeSeriesStore
	levels            *difficulty.Levels
	uaKey             [64]byte
	salt              []byte
	verifyLogChan     chan *common.VerifyRecord
	verifyLogCancel   context.CancelFunc
	paddleAPI         billing.PaddleAPI
	maintenanceCancel context.CancelFunc
}

func NewServer(store *db.BusinessStore,
	timeSeries *db.TimeSeriesStore,
	verifyFlushInterval time.Duration,
	paddleAPI billing.PaddleAPI,
	getenv func(string) string) *server {
	srv := &server{
		stage:         getenv("STAGE"),
		businessDB:    store,
		timeSeries:    timeSeries,
		verifyLogChan: make(chan *common.VerifyRecord, 3*verifyBatchSize/2),
		salt:          []byte(getenv("API_SALT")),
		paddleAPI:     paddleAPI,
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

type verifyError int

const (
	verifyNoError           verifyError = 0
	verifyErrorOther        verifyError = 1
	duplicateSolutionsError verifyError = 2
	invalidSolutionError    verifyError = 3
	parseResponseError      verifyError = 4
	signatureHashMismatch   verifyError = 5
	puzzleExpiredError      verifyError = 6
	invalidPropertyError    verifyError = 7
	wrongOwnerError         verifyError = 8
	verifiedBeforeError     verifyError = 9
)

var (
	errIPNotFound = errors.New("no valid IP address found")
)

type verifyResponse struct {
	Success    bool          `json:"success"`
	ErrorCodes []verifyError `json:"error-codes,omitempty"`
	// other fields from Recaptcha - unclear if we need them
	// ChallengeTS common.JSONTime       `json:"challenge_ts"`
	// Hostname    string                `json:"hostname"`
}

func (s *server) Setup(router *http.ServeMux, prefix string, auth *authMiddleware) {
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	s.setupWithPrefix(prefix, router, auth)
}

func (s *server) Shutdown() {
	s.levels.Shutdown()

	slog.Debug("Shutting down API server routines")
	s.verifyLogCancel()
	close(s.verifyLogChan)

	if s.maintenanceCancel != nil {
		s.maintenanceCancel()
	}
}

func (s *server) setupWithPrefix(prefix string, router *http.ServeMux, auth *authMiddleware) {
	chain := common.NewMiddleWareChain(common.NoCache, common.Recovered)
	// NOTE: auth middleware provides rate limiting internally
	router.HandleFunc(http.MethodGet+" "+prefix+common.PuzzleEndpoint, chain.Build(auth.Sitekey(s.puzzle)))
	router.HandleFunc(http.MethodPost+" "+prefix+common.VerifyEndpoint, chain.Build(auth.APIKey(common.Logged(common.MaxBytesHandler(s.verify, maxSolutionsBodySize)))))
	router.HandleFunc(http.MethodPost+" "+prefix+common.PaddleSubscriptionCreated, common.Recovered(auth.Private(common.Logged(common.MaxBytesHandler(s.subscriptionCreated, maxPaddleBodySize)))))
	router.HandleFunc(http.MethodPost+" "+prefix+common.PaddleSubscriptionUpdated, common.Recovered(auth.Private(common.Logged(common.MaxBytesHandler(s.subscriptionUpdated, maxPaddleBodySize)))))
	router.HandleFunc(http.MethodPost+" "+prefix+common.PaddleSubscriptionCancelled, common.Recovered(auth.Private(common.Logged(common.MaxBytesHandler(s.subscriptionCancelled, maxPaddleBodySize)))))
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
		slog.WarnContext(ctx, "Returning stub puzzle before auth is backfilled", "sitekey", sitekey)
		uuid := db.UUIDFromSiteKey(sitekey)
		// if it's a legit request, then puzzle will be also legit (verifiable) with this PropertyID
		puzzle.PropertyID = uuid.Bytes
		puzzle.Difficulty = difficulty.LevelMedium
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

func (s *server) puzzle(w http.ResponseWriter, r *http.Request) {
	if (r.Method != http.MethodGet) && (r.Method != http.MethodOptions) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// TODO: Set CORS for the domain, associated with property
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "x-pc-captcha-version, Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

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

	w.WriteHeader(http.StatusOK)
	w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
	if _, werr := w.Write(response); werr != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle response", common.ErrAttr(werr))
	}
}

func (s *server) sendVerifyErrors(ctx context.Context, w http.ResponseWriter, errors ...verifyError) {
	response := &verifyResponse{
		Success:    false,
		ErrorCodes: errors,
	}

	common.SendJSONResponse(ctx, w, response, map[string]string{})
}

func (s *server) verify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := r.FormValue(common.ParamResponse)
	parts := strings.Split(data, ".")
	if len(parts) != 3 {
		slog.ErrorContext(ctx, "Wrong number of parts", "count", len(parts))
		s.sendVerifyErrors(ctx, w, parseResponseError)
		return
	}

	solutionsData, puzzleData, signature := parts[0], parts[1], parts[2]

	puzzleBytes, err := base64.StdEncoding.DecodeString(puzzleData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode puzzle data", common.ErrAttr(err))
		s.sendVerifyErrors(ctx, w, parseResponseError)
		return
	}

	hasher := hmac.New(sha1.New, s.salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hash := hasher.Sum(nil)

	requestHash, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode signature bytes", common.ErrAttr(err))
		s.sendVerifyErrors(ctx, w, parseResponseError)
		return
	}

	if !bytes.Equal(hash, requestHash) {
		slog.ErrorContext(ctx, "Puzzle hash is not equal", common.ErrAttr(err))
		s.sendVerifyErrors(ctx, w, signatureHashMismatch)
		return
	}

	tnow := time.Now().UTC()
	puzzle, propertyAndOrg, verr := s.verifyPuzzleValid(ctx, puzzleBytes, tnow)
	if verr != verifyNoError {
		s.sendVerifyErrors(ctx, w, verr)
		return
	}

	if verr := s.verifySolutionsValid(ctx, puzzle, puzzleBytes, solutionsData); verr != verifyNoError {
		s.sendVerifyErrors(ctx, w, verr)
		return
	}

	if cerr := s.businessDB.CachePuzzle(ctx, puzzle, tnow); cerr != nil {
		slog.ErrorContext(ctx, "Failed to cache puzzle", common.ErrAttr(cerr))
	}

	s.addVerifyRecord(ctx, puzzle, propertyAndOrg)

	common.SendJSONResponse(ctx, w, &verifyResponse{Success: true}, map[string]string{})
}

func (s *server) addVerifyRecord(ctx context.Context, p *puzzle.Puzzle, property *dbgen.Property) {
	vr := &common.VerifyRecord{
		UserID:     property.OrgOwnerID.Int32,
		OrgID:      property.OrgID.Int32,
		PropertyID: property.ID,
		PuzzleID:   p.PuzzleID,
		Timestamp:  time.Now().UTC(),
	}

	s.verifyLogChan <- vr
}

func (s *server) verifyPuzzleValid(ctx context.Context, puzzleBytes []byte, tnow time.Time) (*puzzle.Puzzle, *dbgen.Property, verifyError) {
	p := new(puzzle.Puzzle)

	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", common.ErrAttr(uerr))
		return nil, nil, parseResponseError
	}

	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Puzzle is expired", "expiration", p.Expiration, "now", tnow)
		return p, nil, puzzleExpiredError
	}

	if s.businessDB.CheckPuzzleCached(ctx, p) {
		return p, nil, verifiedBeforeError
	}

	sitekey := db.UUIDToSiteKey(pgtype.UUID{Valid: true, Bytes: p.PropertyID})
	properties, err := s.businessDB.RetrievePropertiesBySitekey(ctx, []string{sitekey})
	if (err != nil) || (len(properties) != 1) {
		if (err == db.ErrNegativeCacheHit) || (err == db.ErrRecordNotFound) || (err == db.ErrSoftDeleted) {
			return p, nil, invalidPropertyError
		}

		slog.ErrorContext(ctx, "Failed to find property by sitekey", "sitekey", sitekey, common.ErrAttr(err))
		return p, nil, verifyErrorOther
	}

	property := properties[0]
	apiKey := ctx.Value(common.APIKeyContextKey).(*dbgen.APIKey)

	if property.OrgOwnerID != apiKey.UserID {
		slog.WarnContext(ctx, "Org owner does not match API key owner", "api_key_user", apiKey.UserID.Int32,
			"org_user", property.OrgOwnerID.Int32)
		return p, property, wrongOwnerError
	}

	return p, property, verifyNoError
}

func (s *server) verifySolutionsValid(ctx context.Context, p *puzzle.Puzzle, puzzleBytes []byte, solutionsData string) verifyError {
	solutions, err := puzzle.NewSolutions(solutionsData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode solutions bytes", common.ErrAttr(err))
		return parseResponseError
	}

	if uerr := solutions.CheckUnique(ctx); uerr != nil {
		slog.ErrorContext(ctx, "Solutions are not unique", common.ErrAttr(uerr))
		return duplicateSolutionsError
	}

	if len(puzzleBytes) < puzzle.PuzzleBytesLength {
		extendedPuzzleBytes := make([]byte, puzzle.PuzzleBytesLength)
		copy(extendedPuzzleBytes, puzzleBytes)
		puzzleBytes = extendedPuzzleBytes
	}

	solutionsCount, err := solutions.Verify(ctx, puzzleBytes, p.Difficulty)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to verify solutions", common.ErrAttr(err))
		return invalidSolutionError
	}

	if solutionsCount != int(p.SolutionsCount) {
		slog.WarnContext(ctx, "Invalid solutions count", "expected", p.SolutionsCount, "actual", solutionsCount)
		return invalidSolutionError
	}

	return verifyNoError
}

func (s *server) flushVerifyLog(ctx context.Context, delay time.Duration) {
	var batch []*common.VerifyRecord
	slog.DebugContext(ctx, "Processing verify log", "interval", delay.String())

	for running := true; running; {
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

func (s *server) StartMaintenanceJobs() {
	var maintenanceCtx context.Context
	maintenanceCtx, s.maintenanceCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "api_maintenance"))

	limitsJob := &db.UniquePeriodicJob{
		Store: s.businessDB,
		Job: &UsageLimitsJob{
			// it will be a truly great problem to have when these will be not enough
			MaxUsers:   200,
			From:       time.Now().UTC(),
			BusinessDB: s.businessDB,
			TimeSeries: s.timeSeries,
		},
	}

	go common.RunPeriodicJob(maintenanceCtx, limitsJob)
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	slog.Error("Inside catchall handler", "path", r.URL.Path, "method", r.Method)

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		slog.Error("Failed to handle the request", "path", r.URL.Path)

		return
	}
}
