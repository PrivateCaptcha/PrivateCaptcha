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
	"strconv"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/medama-io/go-useragent"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/sync/semaphore"
)

var (
	errUninitialized            = errors.New("not initialized")
	errInvalidPropertyChallenge = errors.New("invalid property challenge")
	errVerificationBusy         = errors.New("verification capacity exhausted")
)

type Verifier struct {
	Salt                    *puzzleSalt
	UserFingerprintKey      *userFingerprintKey
	FingerprintHeaderKey    common.ConfigItem
	Argon2IDMemoryBudgetKey common.ConfigItem
	FingerprintHeader       string
	UAParser                *useragent.Parser
	Store                   db.Implementor
	TestPuzzle              puzzle.Puzzle
	TestPuzzleData          *puzzle.PuzzlePayload
	TestSolutions           puzzle.SolutionPayload
	PropertyStats           *leakybucket.CircularBucketManager
	verificationMu          sync.RWMutex
	verificationSemaphore   *semaphore.Weighted
	verificationCapacityKiB int64
}

var _ puzzle.Engine = (*Verifier)(nil)

func NewVerifier(cfg common.ConfigStore, store db.Implementor, fingerprintHeaderKey common.ConfigItem, uaParser *useragent.Parser) *Verifier {
	if uaParser == nil {
		uaParser = useragent.NewParser()
	}

	testPuzzle := puzzle.NewComputePuzzle(0 /*puzzle ID*/, db.TestPropertyUUID.Bytes, 0 /*difficulty*/)
	budgetKey := cfg.Get(common.Argon2IDMemoryBudgetKey)
	capacity := config.Argon2IDMemoryBudgetKiB(context.Background(), budgetKey, int64(puzzle.Argon2IDMemoryKiB))
	return &Verifier{
		Salt:                    NewPuzzleSalt(cfg.Get(common.APISaltKey)),
		UserFingerprintKey:      NewUserFingerprintKey(cfg.Get(common.UserFingerprintIVKey)),
		FingerprintHeaderKey:    fingerprintHeaderKey,
		Argon2IDMemoryBudgetKey: budgetKey,
		FingerprintHeader:       fingerprintHeaderKey.Value(),
		UAParser:                uaParser,
		Store:                   store,
		TestPuzzle:              testPuzzle,
		TestSolutions:           puzzle.NewStubPayload(testPuzzle),
		PropertyStats:           leakybucket.NewCircularBucketManager(PropertyBucketSize),
		verificationSemaphore:   semaphore.NewWeighted(capacity),
		verificationCapacityKiB: capacity,
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

	v.FingerprintHeader = v.FingerprintHeaderKey.Value()
	if len(v.FingerprintHeader) > 0 {
		slog.DebugContext(ctx, "Using fingerprint header", "header", v.FingerprintHeader)
	}

	v.UpdateMemoryBudget(ctx)

	return nil
}

// UpdateMemoryBudget applies the current budget to future verifications.
func (v *Verifier) UpdateMemoryBudget(ctx context.Context) {
	capacity := config.Argon2IDMemoryBudgetKiB(ctx, v.Argon2IDMemoryBudgetKey, int64(puzzle.Argon2IDMemoryKiB))
	v.verificationMu.Lock()
	defer v.verificationMu.Unlock()
	if v.verificationSemaphore == nil || capacity != v.verificationCapacityKiB {
		v.verificationSemaphore = semaphore.NewWeighted(capacity)
		v.verificationCapacityKiB = capacity
	}
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

	propertyID := p.PropertyID()
	if p.IsZero() && bytes.Equal(propertyID[:], db.TestPropertyUUID.Bytes[:]) {
		slog.Log(ctx, common.LevelTrace, "Verifying test puzzle", common.PuzzleIDAttr(p.PuzzleID()))
		return p, nil, puzzle.TestPropertyError
	}

	if expiration := p.Expiration(); !tnow.Before(expiration) {
		slog.WarnContext(ctx, "Puzzle is expired", "expiration", expiration, "now", tnow, common.PuzzleIDAttr(p.PuzzleID()))
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
		case db.ErrDisabled:
			return p, nil, puzzle.VerifyErrorOther
		case db.ErrMaintenance:
			if payload.NeedsExtraSalt() {
				// Cannot verify signature without property salt - reject to prevent forgery attacks
				return p, nil, puzzle.IntegrityError
			}
			if v.Store.CheckVerifiedPuzzle(ctx, p, 1 /*maxCount*/) {
				slog.WarnContext(ctx, "Puzzle is already cached", "count", 1, common.PuzzleIDAttr(p.PuzzleID()))
				return p, nil, puzzle.VerifiedBeforeError
			}
			return p, nil, puzzle.MaintenanceModeError
		default:
			slog.ErrorContext(ctx, "Failed to find property by sitekey", "sitekey", sitekey, common.PuzzleIDAttr(p.PuzzleID()), common.ErrAttr(err))
			return p, nil, puzzle.VerifyErrorOther
		}
	}

	var maxCount uint32 = 1
	if (property != nil) && (property.MaxReplayCount > 0) {
		maxCount = uint32(property.MaxReplayCount)
	}

	if v.Store.CheckVerifiedPuzzle(ctx, p, maxCount) {
		slog.WarnContext(ctx, "Puzzle is already cached", "count", maxCount, common.PuzzleIDAttr(p.PuzzleID()))
		return p, nil, puzzle.VerifiedBeforeError
	}
	if payload.NeedsExtraSalt() {
		if serr := payload.VerifySignature(ctx, v.Salt.Value(), property.Salt); serr != nil {
			return p, nil, puzzle.IntegrityError
		}
	}

	return p, property, puzzle.VerifyNoError
}

func (v *Verifier) checkUserPermissions(ctx context.Context, property *dbgen.Property, userID int32) bool {
	// TODO: User should only access property that belongs to active subscriber
	// currently we just allow all access and rely on userLimiter logic in APIs but we should somehow check
	// this here as well. So if user has inactive subscription, they shouldn't access their own properties
	// but they can access properties from other ("valid") orgs where they are a member
	if (property.OrgOwnerID.Int32 == userID) || (property.CreatorID.Int32 == userID) {
		return true
	}

	slog.DebugContext(ctx, "Org owner does not match expected owner", "expectedOwner", userID,
		"orgOwner", property.OrgOwnerID.Int32, "propertyCreator", property.CreatorID.Int32)

	// at this point we know user is a legit user (due to OwnerIDSource found someone) and we only need to check if
	// they are the org member, because currently they are NOT an org/property owner

	if v.Store.CheckUserPropertyAccess(ctx, property, userID) {
		return true
	}

	slog.WarnContext(ctx, "User does not have permissions to access property", "userID", userID, "propID", property.ID)

	return false
}

func (v *Verifier) Verify(ctx context.Context, verifyPayload puzzle.SolutionPayload, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.VerifyResult, error) {
	return v.verify(ctx, verifyPayload, expectedOwner, tnow, false /*skip memory semaphore*/)
}

// VerifyUnsafe skips Argon2id memory admission. Callers must bound concurrent verifications themselves.
func (v *Verifier) VerifyUnsafe(ctx context.Context, verifyPayload puzzle.SolutionPayload, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.VerifyResult, error) {
	return v.verify(ctx, verifyPayload, expectedOwner, tnow, true /*skip memory semaphore*/)
}

func (v *Verifier) verify(ctx context.Context, verifyPayload puzzle.SolutionPayload, expectedOwner puzzle.OwnerIDSource, tnow time.Time, skipMemorySemaphore bool) (*puzzle.VerifyResult, error) {
	puzzleObject, property, perr := v.verifyPuzzleValid(ctx, verifyPayload, tnow)
	result := puzzle.NewVerifyResult(perr, puzzleObject, tnow)
	if puzzleObject != nil && !puzzleObject.IsZero() {
		result.PuzzleID = puzzleObject.PuzzleID()
		// The puzzle bytes are untrusted until their signature is verified (and we parse expiration/creation time from bytes)
		// Zero CreatedAt means the result is not reportable so we protect DB from attacker-controlled rows (and previously
		// we used result.Valid() which checks IDs and CreatedAt, but not the verification error)
		// So we want to establish fact of: "Non-zero analytics timestamps came from a signature-validated puzzle"
		if perr == puzzle.VerifyNoError || perr == puzzle.MaintenanceModeError {
			result.ExpiresAt = puzzleObject.Expiration().UTC()
			validityPeriod := puzzle.DefaultValidityPeriod
			if property != nil && !puzzleObject.IsStub() {
				// NOTE: user could have changed property validity interval of course in between but it should be an edge-case
				// and it does not affect verification as we rely on expiration rather than creation
				validityPeriod = property.ValidityInterval
			}
			result.CreatedAt = result.ExpiresAt.Add(-min(validityPeriod, puzzle.MaxValidityPeriod))
		}
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
		if ownerID, ownerOrgID, err := expectedOwner.OwnerID(ctx, tnow); err == nil {
			if !v.checkUserPermissions(ctx, property, ownerID) {
				result.SetError(puzzle.WrongOwnerError)
				return result, nil
			}

			// for scoped API keys, we want to take org ID into account
			if (ownerOrgID != nil) && property.OrgID.Valid && (property.OrgID.Int32 != *ownerOrgID) {
				slog.WarnContext(ctx, "Owner org scope does not match property org", "propertyOrgID", property.OrgID.Int32, "ownerOrgID", *ownerOrgID)
				result.SetError(puzzle.OrgScopeError)
				return result, nil
			}
		} else {
			slog.ErrorContext(ctx, "Failed to fetch valid owner ID", "puzzleID", puzzleObject.PuzzleID(), common.ErrAttr(err))
			return nil, errPuzzleOwner
		}
	}

	metadata, verr, err := v.verifyPayload(ctx, verifyPayload, skipMemorySemaphore)
	if err != nil {
		return nil, err
	}
	if verr != puzzle.VerifyNoError {
		// NOTE: unlike solutions/puzzle, diagnostics bytes can be totally tampered
		vlog := slog.With("result", verr.String(), "clientError", metadata.ErrorCode(), "elapsedMillis", metadata.ElapsedMillis(), "puzzleID", puzzleObject.PuzzleID())
		if property != nil {
			vlog = vlog.With("userID", property.OrgOwnerID.Int32, "propID", property.ID)
		}
		vlog.WarnContext(ctx, "Failed to verify solutions")

		result.SetError(verr)
		return result, nil
	}

	return result, nil
}

func (v *Verifier) verifyPayload(ctx context.Context, payload puzzle.SolutionPayload, skipMemorySemaphore bool) (*puzzle.Metadata, puzzle.VerifyError, error) {
	if payload.Puzzle().Challenge() != puzzle.ChallengeArgon2ID {
		metadata, result := payload.VerifySolutions(ctx)
		return metadata, result, nil
	}
	if skipMemorySemaphore {
		if err := ctx.Err(); err != nil {
			return nil, puzzle.VerifyNoError, err
		}
		metadata, result := payload.VerifySolutions(ctx)
		return metadata, result, nil
	}
	v.verificationMu.RLock()
	defer v.verificationMu.RUnlock()
	if v.verificationSemaphore == nil {
		return nil, puzzle.VerifyNoError, errUninitialized
	}
	weight := int64(puzzle.Argon2IDMemoryKiB)
	if ctx.Err() != nil {
		return nil, puzzle.VerifyNoError, ctx.Err()
	}
	if !v.verificationSemaphore.TryAcquire(weight) {
		slog.WarnContext(ctx, "Verification capacity exhausted", "weightKiB", weight, "capacityKiB", v.verificationCapacityKiB)
		return nil, puzzle.VerifyNoError, errVerificationBusy
	}
	defer v.verificationSemaphore.Release(weight)
	if ctx.Err() != nil {
		return nil, puzzle.VerifyNoError, ctx.Err()
	}
	metadata, result := payload.VerifySolutions(ctx)
	return metadata, result, nil
}

func (v *Verifier) CacheVerification(ctx context.Context, vr *puzzle.VerifyResult) {
	if vr == nil {
		return
	}

	puzzle := vr.Puzzle()
	if puzzle == nil {
		return
	}

	v.Store.CacheVerifiedPuzzle(ctx, puzzle, vr.VerificationTime())
}

func (v *Verifier) recordPuzzleStats(property *dbgen.Property, tnow time.Time) {
	if property != nil {
		v.PropertyStats.RecordPuzzles(property.ID, 1, tnow)
	}
}

func (v *Verifier) recordVerificationStats(result *puzzle.VerifyResult, tnow time.Time) {
	if result == nil || result.PropertyID == 0 || result.Error == puzzle.WrongOwnerError || result.Error == puzzle.OrgScopeError {
		return
	}
	if result.PuzzleID == 0 {
		v.PropertyStats.RecordPuzzles(result.PropertyID, 1, tnow)
	}
	// NOTE: we don't filter out `result.Success()`, like in other places, because here we are interested in ALL attempts
	v.PropertyStats.RecordVerifications(result.PropertyID, 1, tnow)
}

func selectPropertyChallenge(selected dbgen.ChallengeType, logical uint8, captchaVersion string) (challenge puzzle.Challenge, wireDifficulty uint8, fallback bool, err error) {
	switch selected {
	case "", dbgen.ChallengeTypeBlake2b:
		return puzzle.ChallengeBlake2b, logical, false, nil
	case dbgen.ChallengeTypeArgon2ID:
		if captchaVersion == "1" {
			return puzzle.ChallengeBlake2b, logical, true, nil
		}
		if wire, ok := puzzle.Argon2IDWireDifficulty(logical); ok {
			return puzzle.ChallengeArgon2ID, wire, false, nil
		}
		return puzzle.ChallengeBlake2b, logical, true, nil
	default:
		return 0, 0, false, errInvalidPropertyChallenge
	}
}

func (v *Verifier) PuzzleForRequest(r *http.Request, levels *difficulty.Levels, rulesPair *rules.RulesPair, ri *rules.RequestInfo) (puzzle.Puzzle, *dbgen.Property, error) {
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
		written := false
		if len(v.FingerprintHeader) > 0 {
			if headerVal := r.Header.Get(v.FingerprintHeader); len(headerVal) > 0 && len(headerVal) <= 256 {
				hash.Write([]byte(headerVal))
				written = true
			}
		}
		if !written {
			if ip, ok := contextIP.(netip.Addr); ok {
				hash.Write(ip.AsSlice())
			} else {
				slog.ErrorContext(ctx, "Rate limit context key type mismatch", "ip", contextIP)
				hash.Write([]byte(r.RemoteAddr))
			}
		}
		hmac := hash.Sum(nil)
		truncatedHmac := hmac[:8]
		fingerprint = binary.BigEndian.Uint64(truncatedHmac)
	}

	tnow := time.Now()

	var difficultyProperty difficulty.Property = difficulty.NewDBProperty(property)
	if rulesPair != nil {
		difficultyProperty = rulesPair.Apply(ri, difficultyProperty)
	}

	verificationRate := v.PropertyStats.VerificationRate(property.ID, tnow)
	puzzleDifficulty, _, err := levels.DifficultyEx(ctx, fingerprint, difficultyProperty, tnow, verificationRate)
	challenge, wireDifficulty, fallback, challengeErr := selectPropertyChallenge(property.Challenge, puzzleDifficulty, r.Header.Get(common.HeaderCaptchaVersion))
	if challengeErr != nil {
		slog.ErrorContext(ctx, "Invalid property challenge", "propID", property.ID, "challenge", property.Challenge, common.ErrAttr(challengeErr))
		return nil, property, challengeErr
	}

	puzzleID := puzzle.NextPuzzleID()
	result, challengeErr := puzzle.NewComputePuzzleForChallenge(puzzleID, property.ExternalID.Bytes, wireDifficulty, challenge)
	if challengeErr != nil {
		return nil, property, challengeErr
	}
	if err := result.Init(property.ValidityInterval); err != nil {
		slog.ErrorContext(ctx, "Failed to init puzzle", common.ErrAttr(err))
	}

	accessRecord := v.createPuzzleAccess(difficultyProperty, fingerprint, result, tnow, ri)
	if accessErr := levels.RecordAccess(ctx, accessRecord); err == nil {
		err = accessErr
	}

	slog.Log(ctx, common.LevelTrace, "Prepared new puzzle", "propID", property.ID, "requestedChallenge", property.Challenge,
		"challenge", result.Challenge(), "logicalDifficulty", puzzleDifficulty, "wireDifficulty", wireDifficulty,
		"blakeFallback", fallback,
		"puzzleID", result.PuzzleID(), "userID", property.OrgOwnerID.Int32)

	return result, property, err
}

func (v *Verifier) createPuzzleAccess(difficultyProperty difficulty.Property, fingerprint common.TFingerprint, result puzzle.Puzzle, tnow time.Time, ri *rules.RequestInfo) *common.AccessRecord {
	accessRecord := &common.AccessRecord{
		Fingerprint: fingerprint,
		UserID:      difficultyProperty.OwnerID(),
		OrgID:       difficultyProperty.OrgID(),
		PropertyID:  difficultyProperty.ID(),
		RuleID:      difficultyProperty.RuleID(),
		Timestamp:   tnow,
		PuzzleID:    result.PuzzleID(),
		ExpiresAt:   result.Expiration(),
	}
	if ri != nil {
		parsedUA := ri.ParsedUserAgent(v.UAParser)
		accessRecord.Browser = parsedUA.Browser().String()
		accessRecord.OS = parsedUA.OS().String()
		accessRecord.Device = parsedUA.Device().String()
		if major, err := strconv.ParseUint(parsedUA.BrowserVersionMajor(), 10, 16); err == nil {
			accessRecord.BrowserMajor = uint16(major)
		}
		accessRecord.IPFamily, accessRecord.IPPrefix = common.MaskedIPPrefix(ri.IPAddr())
	}

	return accessRecord
}
