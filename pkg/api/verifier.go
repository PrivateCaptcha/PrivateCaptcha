package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/blake2b"
)

var (
	errUninitialized = errors.New("not initialized")
)

type Verifier struct {
	Salt               *puzzleSalt
	UserFingerprintKey *userFingerprintKey
	Store              db.Implementor
	TestPuzzle         puzzle.Puzzle
	TestPuzzleData     *puzzle.PuzzlePayload
	TestSolutions      puzzle.SolutionPayload
}

var _ puzzle.Engine = (*Verifier)(nil)

func NewVerifier(cfg common.ConfigStore, store db.Implementor) *Verifier {
	testPuzzle := puzzle.NewComputePuzzle(0 /*puzzle ID*/, db.TestPropertyUUID.Bytes, 0 /*difficulty*/)
	return &Verifier{
		Salt:               NewPuzzleSalt(cfg.Get(common.APISaltKey)),
		UserFingerprintKey: NewUserFingerprintKey(cfg.Get(common.UserFingerprintIVKey)),
		Store:              store,
		TestPuzzle:         testPuzzle,
		TestSolutions:      puzzle.NewStubPayload(testPuzzle),
	}
}

func (v *Verifier) Update(ctx context.Context) error {
	if err := v.Salt.Update(); err != nil {
		slog.ErrorContext(ctx, "Failed to update puzzle salt", common.ErrAttr(err))
		return err
	}

	var err error
	v.TestPuzzleData, err = v.TestPuzzle.Serialize(ctx, v.Salt.Value(), nil /*property salt*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize test puzzle", common.ErrAttr(err))
		return err
	}

	if err := v.UserFingerprintKey.Update(); err != nil {
		slog.ErrorContext(ctx, "Failed to update user fingerprint key", common.ErrAttr(err))
		return err
	}

	return nil
}

func (v *Verifier) WriteTestPuzzle(w io.Writer) error {
	if v.TestPuzzleData == nil {
		return errUninitialized
	}

	return v.TestPuzzleData.Write(w)
}

func (v *Verifier) Create(puzzleID uint64, propertyID [puzzle.PropertyIDSize]byte, difficulty uint8) puzzle.Puzzle {
	return puzzle.NewComputePuzzle(puzzleID, propertyID, difficulty)
}

func (v *Verifier) Write(ctx context.Context, p puzzle.Puzzle, extraSalt []byte, w http.ResponseWriter) error {
	payload, err := p.Serialize(ctx, v.Salt.Value(), extraSalt)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return err
	}

	common.WriteHeaders(w, common.NoCacheHeaders)
	common.WriteHeaders(w, headersContentPlain)
	return payload.Write(w)
}

func (v *Verifier) ParseSolutionPayload(ctx context.Context, data []byte) (puzzle.SolutionPayload, error) {
	// this is faster than doing base64 decoding and parsing of zero puzzle
	if v.TestPuzzleData.IsSuffixFor(data) {
		// lazy roughly check solutions (without "dot" and puzzle)
		solutionsBase64Size := len(data) - v.TestPuzzleData.Size() - 1
		slog.Log(ctx, common.LevelTrace, "Detected test puzzle suffix in verify payload", "remaining", solutionsBase64Size)
		solutionsMaxSize := base64.StdEncoding.DecodedLen(solutionsBase64Size)
		if solutionsMaxSize < v.TestPuzzle.SolutionsCount()*puzzle.SolutionLength {
			return nil, errTestSolutions
		}
		return v.TestSolutions, nil
	}

	return puzzle.ParseVerifyPayload[puzzle.ComputePuzzle](ctx, data)
}

func (v *Verifier) verifyPuzzleValid(ctx context.Context, payload puzzle.SolutionPayload, tnow time.Time) (puzzle.Puzzle, *dbgen.Property, puzzle.VerifyError) {
	p := payload.Puzzle()
	plog := slog.With("puzzleID", p.PuzzleID())

	propertyID := p.PropertyID()
	if p.IsZero() && bytes.Equal(propertyID[:], db.TestPropertyUUID.Bytes[:]) {
		plog.Log(ctx, common.LevelTrace, "Verifying test puzzle")
		return p, nil, puzzle.TestPropertyError
	}

	if expiration := p.Expiration(); !tnow.Before(expiration) {
		plog.WarnContext(ctx, "Puzzle is expired", "expiration", expiration, "now", tnow)
		return p, nil, puzzle.PuzzleExpiredError
	}

	// "else" branch is handled below _after_ we fetch the property from DB
	if !payload.NeedsExtraSalt() {
		if serr := payload.VerifySignature(ctx, v.Salt.Value(), nil /*extra salt*/); serr != nil {
			return p, nil, puzzle.IntegrityError
		}
	}

	// the reason we delay accessing DB for API key and not for sitekey is that sitekey comes from a signed puzzle payload
	// and API key is a rather random string in HTTP header so has a higher chance of misuse
	sitekey := db.UUIDToSiteKey(pgtype.UUID{Valid: true, Bytes: propertyID})
	property, err := v.Store.Impl().RetrievePropertyBySitekey(ctx, sitekey)
	if err != nil {
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

	var maxCount uint32 = 1
	if (property != nil) && (property.MaxReplayCount > 0) {
		maxCount = uint32(property.MaxReplayCount)
	}

	if v.Store.CheckVerifiedPuzzle(ctx, p, maxCount) {
		plog.WarnContext(ctx, "Puzzle is already cached", "count", maxCount)
		return p, nil, puzzle.VerifiedBeforeError
	}

	if payload.NeedsExtraSalt() {
		if serr := payload.VerifySignature(ctx, v.Salt.Value(), property.Salt); serr != nil {
			return p, nil, puzzle.IntegrityError
		}
	}

	return p, property, puzzle.VerifyNoError
}

func (v *Verifier) Verify(ctx context.Context, verifyPayload puzzle.SolutionPayload, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.VerifyResult, error) {
	result := &puzzle.VerifyResult{}
	puzzleObject, property, perr := v.verifyPuzzleValid(ctx, verifyPayload, tnow)
	result.SetError(perr)
	if puzzleObject != nil && !puzzleObject.IsZero() {
		result.PuzzleID = puzzleObject.PuzzleID()
		validityPeriod := puzzle.DefaultValidityPeriod
		if property != nil {
			// NOTE: user could have changed property validity interval of course in between but it should be an edge-case
			// and it does not affect verification as we rely on expiration rather than creation
			validityPeriod = property.ValidityInterval
		}
		result.CreatedAt = puzzleObject.Expiration().Add(-validityPeriod)
	}
	if property != nil {
		result.UserID = property.OrgOwnerID.Int32
		result.OrgID = property.OrgID.Int32
		result.PropertyID = property.ID
		result.Domain = property.Domain
	}
	if perr != puzzle.VerifyNoError && perr != puzzle.MaintenanceModeError {
		return result, nil
	}

	if property != nil {
		// position in code where expected owner is checked is a tradeoff between compute for verifying solutions (below)
		// and IO for accessing DB of potentially malicious request (in case not-yet-checked API key turns out invalid)
		if ownerID, err := expectedOwner.OwnerID(ctx, tnow); err == nil {
			if (property.OrgOwnerID.Int32 != ownerID) && (property.CreatorID.Int32 != ownerID) {
				slog.WarnContext(ctx, "Org owner does not match expected owner", "expectedOwner", ownerID,
					"orgOwner", property.OrgOwnerID.Int32, "propertyCreator", property.CreatorID.Int32)
				result.SetError(puzzle.WrongOwnerError)
				return result, nil
			}
		} else {
			slog.ErrorContext(ctx, "Failed to fetch owner ID", "puzzleID", puzzleObject.PuzzleID(), common.ErrAttr(err))
			return nil, errPuzzleOwner
		}
	}

	if metadata, verr := verifyPayload.VerifySolutions(ctx); verr != puzzle.VerifyNoError {
		// NOTE: unlike solutions/puzzle, diagnostics bytes can be totally tampered
		vlog := slog.With("result", verr.String(), "clientError", metadata.ErrorCode(), "elapsedMillis", metadata.ElapsedMillis(), "puzzleID", puzzleObject.PuzzleID())
		if property != nil {
			vlog = vlog.With("userID", property.OrgOwnerID.Int32, "propID", property.ID)
		}
		vlog.WarnContext(ctx, "Failed to verify solutions")

		result.SetError(verr)
		return result, nil
	}

	if (puzzleObject != nil) && (property != nil) && (property.MaxReplayCount > 0) {
		v.Store.CacheVerifiedPuzzle(ctx, puzzleObject, tnow)
	} else if puzzleObject != nil {
		slog.Log(ctx, common.LevelTrace, "Skipping caching puzzle", "puzzleID", puzzleObject.PuzzleID())
	}

	return result, nil
}

func (v *Verifier) baseDifficultyOverride(r *http.Request) uint8 {
	ua := r.UserAgent()
	if len(ua) == 0 {
		return uint8(common.DifficultyLevelHigh)
	}

	// curl/python-requests/?
	if len(ua) < 75 {
		return uint8(common.DifficultyLevelMedium)
	}

	if ver, ok := r.Header[common.HeaderPrivateCaptchaVersion]; !ok || len(ver) == 0 || ver[0] != "1" {
		return uint8(common.DifficultyLevelHigh)
	}

	return 0
}

func (v *Verifier) PuzzleForRequest(r *http.Request, levels *difficulty.Levels) (puzzle.Puzzle, *dbgen.Property, error) {
	ctx := r.Context()
	property, isProperty := ctx.Value(common.PropertyContextKey).(*dbgen.Property)
	contextIP := ctx.Value(common.RateLimitKeyContextKey)

	// property will not be cached for auth.backfillDelay and we return an "average" puzzle instead
	// this is done in order to not check the DB on the hot path (decrease attack surface)
	// and if IP address is missing from context, something is fishy
	if !isProperty || (property == nil) || (contextIP == nil) {
		sitekey, ok := ctx.Value(common.SitekeyContextKey).(string)
		if !ok || len(sitekey) == 0 {
			// this shouldn't happen as we sort this in Sitekey() auth middleware, but just in case
			return nil, nil, errInvalidArg
		}

		if sitekey == db.TestPropertySitekey {
			return nil, nil, db.ErrTestProperty
		}

		uuid := db.UUIDFromSiteKey(sitekey)
		// NOTE: we potentially can include user fingerprint stats into the calculation of difficulty
		// but it's besides the point of "quickly returning smth valid from public endpoint"
		// (all valid properties should be more or less aggressively cached all of the time anyways)
		stubPuzzle := v.Create(0 /*puzzle ID*/, uuid.Bytes, uint8(common.DifficultyLevelMedium))
		// if it's a legit request, then puzzle will be also legit (verifiable) with this PropertyID
		if err := stubPuzzle.Init(puzzle.DefaultValidityPeriod); err != nil {
			slog.ErrorContext(ctx, "Failed to init stub puzzle", common.ErrAttr(err))
		}

		slog.Log(ctx, common.LevelTrace, "Returning stub puzzle before auth is backfilled", "puzzleID", stubPuzzle.PuzzleID(),
			"sitekey", sitekey, "difficulty", stubPuzzle.Difficulty())
		return stubPuzzle, nil, nil
	}

	var fingerprint common.TFingerprint
	hash, err := blake2b.New256(v.UserFingerprintKey.Value())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create blake2b hmac", common.ErrAttr(err))
		fingerprint = common.RandomFingerprint()
	} else {
		// TODO: Check if we really need to take user agent into account here
		// or it should be accounted on the anomaly detection side (user-agent is trivial to spoof)
		// hash.Write([]byte(r.UserAgent()))
		if ip, ok := contextIP.(netip.Addr); ok {
			// if IP is not valid (empty), we do want for fingerprint to be the same as ths is fishy enough
			hash.Write(ip.AsSlice())
		} else {
			// this stays as "Error" because we shouldn't even end up here
			slog.ErrorContext(ctx, "Rate limit context key type mismatch", "ip", ip)
			hash.Write([]byte(r.RemoteAddr))
		}
		hmac := hash.Sum(nil)
		truncatedHmac := hmac[:8]
		fingerprint = binary.BigEndian.Uint64(truncatedHmac)
	}

	tnow := time.Now()
	baseDifficulty := v.baseDifficultyOverride(r)
	puzzleDifficulty, _ := levels.DifficultyEx(fingerprint, property, baseDifficulty, tnow)

	puzzleID := puzzle.NextPuzzleID()
	result := v.Create(puzzleID, property.ExternalID.Bytes, puzzleDifficulty)
	if err := result.Init(property.ValidityInterval); err != nil {
		slog.ErrorContext(ctx, "Failed to init puzzle", common.ErrAttr(err))
	}

	slog.Log(ctx, common.LevelTrace, "Prepared new puzzle", "propID", property.ID, "difficulty", result.Difficulty(),
		"puzzleID", result.PuzzleID(), "userID", property.OrgOwnerID.Int32)

	return result, property, nil
}
