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
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

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
)

type server struct {
	businessDB *db.BusinessStore
	levels     *difficulty.Levels
	uaKey      [64]byte
	salt       []byte
}

func NewServer(store *db.BusinessStore, levels *difficulty.Levels, getenv func(string) string) *server {
	s := &server{
		businessDB: store,
		levels:     levels,
		salt:       []byte(getenv("API_SALT")),
	}

	if byteArray, err := hex.DecodeString(getenv("UA_KEY")); (err == nil) && (len(byteArray) == 64) {
		copy(s.uaKey[:], byteArray[:])
	} else {
		slog.Error("Error initializing UA key for server", common.ErrAttr(err), "size", len(byteArray))
	}

	return s
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

func (s *server) Setup(router *http.ServeMux, prefix string, auth *AuthMiddleware) {
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	s.setupWithPrefix(prefix, router, auth)
}

func (s *server) setupWithPrefix(prefix string, router *http.ServeMux, auth *AuthMiddleware) {
	// TODO: Rate-limit Puzzle endpoint with reasonably high limit
	router.HandleFunc(http.MethodGet+" "+prefix+common.PuzzleEndpoint, auth.Sitekey(s.puzzle))
	router.HandleFunc(http.MethodPost+" "+prefix+common.VerifyEndpoint, common.Logged(common.SafeFormPost(auth.APIKey(s.verify), maxSolutionsBodySize)))
}

func (s *server) puzzleForRequest(r *http.Request) (*puzzle.Puzzle, error) {
	puzzle, err := puzzle.NewPuzzle()
	if err != nil {
		return nil, err
	}

	ctx := r.Context()
	propertyAndOrg := ctx.Value(common.PropertyAndOrgContextKey).(*dbgen.PropertyAndOrgByExternalIDRow)
	property, org := propertyAndOrg.Property, propertyAndOrg.Organization

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
		// TODO: Add property domain here when it will be available
		// hash.Write(property.Domain)
		hmac := hash.Sum(nil)
		truncatedHmac := hmac[:8]
		fingerprint = binary.BigEndian.Uint64(truncatedHmac)
	}

	puzzle.Difficulty = s.levels.Difficulty(fingerprint, &property, org.UserID.Int32)

	slog.DebugContext(ctx, "Prepared new puzzle", "propertyID", property.ID)

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
	p, verr := s.verifyPuzzleValid(ctx, puzzleBytes, tnow)
	if verr != verifyNoError {
		s.sendVerifyErrors(ctx, w, verr)
		return
	}

	if verr := s.verifySolutionsValid(ctx, p, puzzleBytes, solutionsData); verr != verifyNoError {
		s.sendVerifyErrors(ctx, w, verr)
		return
	}

	if cerr := s.businessDB.CachePuzzle(ctx, p, tnow); cerr != nil {
		slog.ErrorContext(ctx, "Failed to cache puzzle", common.ErrAttr(cerr))
	}

	common.SendJSONResponse(ctx, w, &verifyResponse{Success: true}, map[string]string{})
}

func (s *server) verifyPuzzleValid(ctx context.Context, puzzleBytes []byte, tnow time.Time) (*puzzle.Puzzle, verifyError) {
	p := new(puzzle.Puzzle)

	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", common.ErrAttr(uerr))
		return nil, parseResponseError
	}

	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Puzzle is expired", "expiration", p.Expiration, "now", tnow)
		return p, puzzleExpiredError
	}

	if s.businessDB.CheckPuzzleCached(ctx, p) {
		return p, verifiedBeforeError
	}

	sitekey := db.UUIDToSiteKey(pgtype.UUID{Valid: true, Bytes: p.PropertyID})
	propertyAndOrg, err := s.businessDB.RetrievePropertyAndOrg(ctx, sitekey)
	_, org := propertyAndOrg.Property, propertyAndOrg.Organization

	if err != nil {
		if (err == db.ErrNegativeCacheHit) || (err == db.ErrRecordNotFound) {
			return p, invalidPropertyError
		}

		slog.ErrorContext(ctx, "Failed to find property by sitekey", "sitekey", sitekey, common.ErrAttr(err))
		return p, verifyErrorOther
	}

	apiKey := ctx.Value(common.APIKeyContextKey).(*dbgen.APIKey)

	if org.UserID != apiKey.UserID {
		slog.WarnContext(ctx, "Org owner does not match API key owner", "api_key_user", apiKey.UserID.Int32,
			"org_user", org.UserID.Int32)
		return p, wrongOwnerError
	}

	return p, verifyNoError
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

func catchAll(w http.ResponseWriter, r *http.Request) {
	slog.Error("Inside catchall handler", "path", r.URL.Path, "method", r.Method)

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		slog.Error("Failed to handle the request", "path", r.URL.Path)

		return
	}
}
