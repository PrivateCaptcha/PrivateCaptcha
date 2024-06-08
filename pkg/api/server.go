package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk"
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
	maxSolutionsBodySize = 256 * 1024
	maxPaddleBodySize    = 10 * 1024
	verifyBatchSize      = 100
)

var (
	errProductNotFound = errors.New("product not found")
)

type server struct {
	stage           string
	businessDB      *db.BusinessStore
	timeSeries      *db.TimeSeriesStore
	levels          *difficulty.Levels
	uaKey           [64]byte
	salt            []byte
	verifyLogChan   chan *common.VerifyRecord
	verifyLogCancel context.CancelFunc
	paddleAPI       billing.PaddleAPI
}

func NewServer(store *db.BusinessStore,
	timeSeries *db.TimeSeriesStore,
	levels *difficulty.Levels,
	verifyFlushInterval time.Duration,
	paddleAPI billing.PaddleAPI,
	getenv func(string) string) *server {
	srv := &server{
		stage:         getenv("STAGE"),
		businessDB:    store,
		timeSeries:    timeSeries,
		levels:        levels,
		verifyLogChan: make(chan *common.VerifyRecord, 3*verifyBatchSize/2),
		salt:          []byte(getenv("API_SALT")),
		paddleAPI:     paddleAPI,
	}

	if byteArray, err := hex.DecodeString(getenv("UA_KEY")); (err == nil) && (len(byteArray) == 64) {
		copy(srv.uaKey[:], byteArray[:])
	} else {
		slog.Error("Error initializing UA key for server", common.ErrAttr(err), "size", len(byteArray))
	}

	var cancelCtx context.Context
	cancelCtx, srv.verifyLogCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "flush_verify_log"))
	go srv.flushVerifyLog(cancelCtx, verifyFlushInterval)

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

func parseRequestIP(r *http.Request) (string, error) {
	ip := r.Header.Get("X-REAL-IP")
	netIP := net.ParseIP(ip)
	if netIP != nil {
		return ip, nil
	}

	ips := r.Header.Get("X-FORWARDED-FOR")
	splitIps := strings.Split(ips, ",")
	for _, ip := range splitIps {
		netIP := net.ParseIP(ip)
		if netIP != nil {
			return ip, nil
		}
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", err
	}
	netIP = net.ParseIP(ip)
	if netIP != nil {
		return ip, nil
	}
	return "", errIPNotFound
}

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
	slog.Debug("Shutting down API server routines")
	close(s.verifyLogChan)
	s.verifyLogCancel()
}

func (s *server) setupWithPrefix(prefix string, router *http.ServeMux, auth *authMiddleware) {
	// TODO: Rate-limit Puzzle endpoint with reasonably high limit
	router.HandleFunc(http.MethodGet+" "+prefix+common.PuzzleEndpoint, auth.Sitekey(s.puzzle))
	router.HandleFunc(http.MethodPost+" "+prefix+common.VerifyEndpoint, common.Logged(common.SafeFormPost(auth.APIKey(s.verify), maxSolutionsBodySize)))
	// TODO: Add Paddle events handler here
	router.HandleFunc(http.MethodPost+" "+prefix+common.PaddleSubscriptionCreated, common.Logged(common.SafeFormPost(auth.Private(s.subscriptionCreated), maxPaddleBodySize)))
}

func (s *server) puzzleForRequest(r *http.Request) (*puzzle.Puzzle, error) {
	puzzle, err := puzzle.NewPuzzle()
	if err != nil {
		return nil, err
	}

	ctx := r.Context()
	property, ok := ctx.Value(common.PropertyContextKey).(*dbgen.Property)
	// property will not be cached for auth.backfillDelay and we return an "average" puzzle instead
	// this is done in order to not check the DB on the hot path (decrease attach surface)
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
		if ip, err := parseRequestIP(r); err == nil {
			hash.Write([]byte(ip))
		}
		hash.Write([]byte(property.Domain))
		hmac := hash.Sum(nil)
		truncatedHmac := hmac[:8]
		fingerprint = binary.BigEndian.Uint64(truncatedHmac)
	}

	puzzle.Difficulty = s.levels.Difficulty(fingerprint, property)

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
		slog.ErrorContext(ctx, "Failed to create puzzle", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	puzzleBytes, err := puzzle.MarshalBinary()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize puzzle", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hasher := hmac.New(sha1.New, s.salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", "error", werr)
	}

	hash := hasher.Sum(nil)
	encodedPuzzle := base64.StdEncoding.EncodeToString(puzzleBytes)
	encodedHash := base64.StdEncoding.EncodeToString(hash)
	response := []byte(fmt.Sprintf("%s.%s", encodedPuzzle, encodedHash))

	w.WriteHeader(http.StatusOK)
	w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
	w.Header().Set(common.HeaderContentLength, strconv.Itoa(len(response)))
	if _, werr := w.Write(response); werr != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle response", "error", werr)
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
	slog.DebugContext(ctx, "Processing verify log", "interval", delay)

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
					slog.DebugContext(ctx, "Inserted batch of verify records", "size", len(batch))
					batch = []*common.VerifyRecord{}
				} else {
					slog.ErrorContext(ctx, "Failed to process batch", common.ErrAttr(err))
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				if err := s.timeSeries.WriteVerifyLogBatch(ctx, batch); err == nil {
					slog.DebugContext(ctx, "Inserted batch of access records after delay", "size", len(batch))
					batch = []*common.VerifyRecord{}
				} else {
					slog.ErrorContext(ctx, "Failed to process batch", common.ErrAttr(err))
				}
			}
		}
	}

	slog.InfoContext(ctx, "Finished processing verify log")
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	slog.Error("Inside catchall handler", "path", r.URL.Path, "method", r.Method)

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		slog.Error("Failed to handle the request", "path", r.URL.Path)

		return
	}
}

func (s *server) newCreateSubscriptionParams(ctx context.Context, evt *paddle.SubscriptionCreatedEvent) (*dbgen.CreateSubscriptionParams, error) {
	var productID string
	var trialEndsAt time.Time
	var nextBilledAt time.Time
	j := -1

	for i, subscr := range evt.Data.Items {
		if _, err := billing.FindPlanByProductID(subscr.Price.ProductID, s.stage); err == nil {
			j = i
			break
		}
	}

	if j == -1 {
		slog.ErrorContext(ctx, "Failed to find a known plan from subscription")
		if len(evt.Data.Items) == 1 {
			j = 0
		} else {
			slog.ErrorContext(ctx, "Unexpected number of subscription items", "count", len(evt.Data.Items))
			return nil, errProductNotFound
		}
	}

	subscr := evt.Data.Items[j]
	productID = subscr.Price.ProductID

	if subscr.TrialDates != nil {
		if trialEndTime, err := time.Parse(time.RFC3339, subscr.TrialDates.EndsAt); err == nil {
			trialEndsAt = trialEndTime
		} else {
			slog.ErrorContext(ctx, "Failed to parse trial end time", "time", subscr.TrialDates.EndsAt, common.ErrAttr(err))
			trialEndsAt = time.Now().UTC().AddDate(0, 1, 0)
		}
	}

	if subscr.NextBilledAt != nil {
		if nextBillTime, err := time.Parse(time.RFC3339, *subscr.NextBilledAt); err == nil {
			nextBilledAt = nextBillTime
		} else {
			slog.ErrorContext(ctx, "Failed to parse next bill time", "time", *subscr.NextBilledAt, common.ErrAttr(err))
		}
	}

	return &dbgen.CreateSubscriptionParams{
		PaddleProductID:      productID,
		PaddleSubscriptionID: evt.Data.ID,
		PaddleCustomerID:     evt.Data.CustomerID,
		Status:               evt.Data.Status,
		TrialEndsAt:          db.Timestampz(trialEndsAt),
		NextBilledAt:         db.Timestampz(nextBilledAt),
	}, nil
}

func (s *server) subscriptionCreated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	evt := &paddle.SubscriptionCreatedEvent{}
	if err := json.Unmarshal(body, evt); err != nil {
		slog.ErrorContext(ctx, "Failed to parse request", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	elog := slog.With("eventID", evt.EventID, "subscriptionID", evt.Data.ID)
	elog.DebugContext(ctx, "Handling subscription created event")

	customer, err := s.paddleAPI.GetCustomerInfo(ctx, evt.Data.CustomerID)
	if err != nil {
		elog.ErrorContext(ctx, "Failed to fetch customer data from Paddle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	subscrParams, err := s.newCreateSubscriptionParams(ctx, evt)
	if err != nil {
		elog.ErrorContext(ctx, "Failed to process paddle event", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	orgName := common.OrgNameFromName(customer.Name)

	if _, _, err = s.businessDB.CreateNewAccount(ctx, subscrParams, customer.Email, customer.Name, orgName); (err != nil) && (err != db.ErrDuplicateAccount) {
		elog.ErrorContext(ctx, "Failed to create a new account", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
