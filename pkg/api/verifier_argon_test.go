package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	dbtests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"golang.org/x/sync/semaphore"
)

func TestArgon2IDAdmissionWiring(t *testing.T) {
	cfg := config.NewBaseConfig(testsConfigStore())
	cfg.Add(config.NewStaticValue(common.Argon2IDMemoryBudgetKey, "512"))
	verifier := NewVerifier(cfg, nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if verifier.verificationSemaphore == nil || verifier.verificationCapacityKiB != 512*1024 {
		t.Fatalf("invalid verification budget: %d KiB", verifier.verificationCapacityKiB)
	}
}

type budgetConfigItem struct{ value string }

func (item *budgetConfigItem) Key() common.ConfigKey { return common.Argon2IDMemoryBudgetKey }
func (item *budgetConfigItem) Value() string         { return item.value }

func TestArgon2IDMemoryBudgetUpdate(t *testing.T) {
	cfg := config.NewBaseConfig(testsConfigStore())
	item := &budgetConfigItem{value: "16"}
	cfg.Add(item)
	verifier := NewVerifier(cfg, nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if verifier.Argon2IDMemoryBudgetKey != item || verifier.verificationCapacityKiB != 16*1024 {
		t.Fatalf("initial capacity = %d KiB, want 16384", verifier.verificationCapacityKiB)
	}
	initial := verifier.verificationSemaphore
	item.value = "32"
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if verifier.verificationCapacityKiB != 32*1024 || verifier.verificationSemaphore == initial {
		t.Fatalf("updated capacity = %d KiB, want 32768", verifier.verificationCapacityKiB)
	}
	updated := verifier.verificationSemaphore
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if verifier.verificationSemaphore != updated {
		t.Fatal("unchanged capacity replaced an in-use semaphore")
	}
	item.value = "0"
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if verifier.verificationCapacityKiB != 512*1024 {
		t.Fatalf("fallback capacity = %d KiB, want 524288", verifier.verificationCapacityKiB)
	}
}

type admissionTestPayload struct {
	puzzle.SolutionPayload
	started chan struct{}
	release chan struct{}
	result  puzzle.VerifyError
	calls   int
}

func (p *admissionTestPayload) VerifySolutions(context.Context) (*puzzle.Metadata, puzzle.VerifyError) {
	p.calls++
	if p.started != nil {
		close(p.started)
		<-p.release
	}
	return nil, p.result
}

func TestVerificationAdmission(t *testing.T) {
	p, err := puzzle.NewComputePuzzleForChallenge(1, [puzzle.PropertyIDSize]byte{}, 24, puzzle.ChallengeArgon2ID)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &Verifier{verificationSemaphore: semaphore.NewWeighted(int64(puzzle.Argon2IDMemoryKiB))}
	first := &admissionTestPayload{SolutionPayload: puzzle.NewStubPayload(p), started: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-first.release:
		default:
			close(first.release)
		}
	}()
	type admissionOutcome struct {
		result puzzle.VerifyError
		err    error
	}
	done := make(chan admissionOutcome, 1)
	go func() {
		_, result, err := verifier.verifyPayload(t.Context(), first)
		done <- admissionOutcome{result, err}
	}()
	<-first.started

	waiting := &admissionTestPayload{SolutionPayload: puzzle.NewStubPayload(p)}
	if _, result, err := verifier.verifyPayload(t.Context(), waiting); err != errVerificationBusy || result != puzzle.VerifyNoError || waiting.calls != 0 {
		t.Fatalf("full admission: result = %v, err = %v, calls = %d", result, err, waiting.calls)
	}
	close(first.release)
	if outcome := <-done; outcome.result != puzzle.VerifyNoError || outcome.err != nil {
		t.Fatalf("first admission result = %v, err = %v", outcome.result, outcome.err)
	}

	for _, expected := range []puzzle.VerifyError{puzzle.VerifyNoError, puzzle.DuplicateSolutionsError, puzzle.InvalidSolutionError} {
		payload := &admissionTestPayload{SolutionPayload: puzzle.NewStubPayload(p), result: expected}
		if _, result, err := verifier.verifyPayload(t.Context(), payload); result != expected || err != nil || payload.calls != 1 {
			t.Fatalf("released capacity after %v: result = %v, err = %v, calls = %d", expected, result, err, payload.calls)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, result, err := verifier.verifyPayload(ctx, waiting); err != context.Canceled || result != puzzle.VerifyNoError || waiting.calls != 0 {
		t.Fatalf("canceled context result = %v, err = %v, calls = %d", result, err, waiting.calls)
	}
	blake := &admissionTestPayload{SolutionPayload: puzzle.NewStubPayload(puzzle.NewComputePuzzle(2, [puzzle.PropertyIDSize]byte{}, 0))}
	if _, result, err := verifier.verifyPayload(ctx, blake); result != puzzle.VerifyNoError || err != nil || blake.calls != 1 {
		t.Fatalf("Blake verification result = %v, err = %v, calls = %d", result, err, blake.calls)
	}
}

func TestMemoryBudgetUpdateWaitsForActiveVerification(t *testing.T) {
	cfg := config.NewBaseConfig(testsConfigStore())
	item := &budgetConfigItem{value: "16"}
	cfg.Add(item)
	verifier := NewVerifier(cfg, nil, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	p, err := puzzle.NewComputePuzzleForChallenge(1, [puzzle.PropertyIDSize]byte{}, 24, puzzle.ChallengeArgon2ID)
	if err != nil {
		t.Fatal(err)
	}
	first := &admissionTestPayload{SolutionPayload: puzzle.NewStubPayload(p), started: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-first.release:
		default:
			close(first.release)
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, _, err := verifier.verifyPayload(t.Context(), first)
		done <- err
	}()
	<-first.started
	initial := verifier.verificationSemaphore
	item.value = "32"
	updated := make(chan struct{})
	go func() {
		verifier.UpdateMemoryBudget(t.Context())
		close(updated)
	}()
	select {
	case <-updated:
		t.Fatal("memory budget changed while an old verification was active")
	case <-time.After(50 * time.Millisecond):
	}
	close(first.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("memory budget update did not complete")
	}
	if verifier.verificationSemaphore == initial || verifier.verificationCapacityKiB != 32*1024 {
		t.Fatalf("updated capacity = %d KiB, want 32768", verifier.verificationCapacityKiB)
	}
}

type argonOwner struct{ id int32 }

func (owner argonOwner) OwnerID(context.Context, time.Time) (int32, *int32, error) {
	return owner.id, nil, nil
}

func TestArgon2IDAdmissionAfterValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	ctx := t.Context()
	user, org, err := dbtests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	property, _, err := store.Impl().CreateNewProperty(ctx, dbtests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewVerifier(testsConfigStore(), store, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if err := verifier.Update(ctx); err != nil {
		t.Fatal(err)
	}
	verifier.verificationSemaphore = semaphore.NewWeighted(int64(puzzle.Argon2IDMemoryKiB))
	verifier.verificationCapacityKiB = int64(puzzle.Argon2IDMemoryKiB)
	p, err := puzzle.NewComputePuzzleForChallenge(puzzle.NextPuzzleID(), property.ExternalID.Bytes, 0, puzzle.ChallengeArgon2ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(property.ValidityInterval); err != nil {
		t.Fatal(err)
	}
	solutions, err := (&puzzle.ComputeSolver{}).Solve(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := p.Serialize(ctx, verifier.Salt.Value(), property.Salt)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	encoded.WriteString(solutions.String())
	encoded.WriteByte('.')
	if err := signed.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	payload, err := verifier.ParseSolutionPayload(ctx, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if result, err := verifier.Verify(ctx, payload, argonOwner{user.ID}, time.Now().UTC()); err != nil || result.Error != puzzle.VerifyNoError {
		t.Fatalf("uncontended verification: result = %+v, err = %v", result, err)
	}

	if err := verifier.verificationSemaphore.Acquire(ctx, int64(puzzle.Argon2IDMemoryKiB)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { verifier.verificationSemaphore.Release(int64(puzzle.Argon2IDMemoryKiB)) })
	shortCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if result, err := verifier.Verify(shortCtx, payload, argonOwner{user.ID}, time.Now().UTC()); err != errVerificationBusy || result != nil {
		t.Fatalf("contended verification: result = %+v, err = %v", result, err)
	}
	if result, err := verifier.Verify(ctx, payload, argonOwner{-1}, time.Now().UTC()); err != nil || result.Error != puzzle.WrongOwnerError {
		t.Fatalf("wrong owner: result = %+v, err = %v", result, err)
	}
	parts := bytes.Split(encoded.Bytes(), []byte{'.'})
	if len(parts) != 3 {
		t.Fatal("invalid payload")
	}
	mutated, err := base64.StdEncoding.DecodeString(string(parts[1]))
	if err != nil {
		t.Fatal(err)
	}
	mutated[1] = byte(puzzle.ChallengeBlake2b)
	invalidPayload, err := verifier.ParseSolutionPayload(ctx, bytes.Join([][]byte{parts[0], []byte(base64.StdEncoding.EncodeToString(mutated)), parts[2]}, []byte{'.'}))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := verifier.Verify(ctx, invalidPayload, argonOwner{user.ID}, time.Now().UTC()); err != nil || result.Error != puzzle.IntegrityError {
		t.Fatalf("tampered signature: result = %+v, err = %v", result, err)
	}
	blake := puzzle.NewComputePuzzle(puzzle.NextPuzzleID(), property.ExternalID.Bytes, 0)
	if err := blake.Init(property.ValidityInterval); err != nil {
		t.Fatal(err)
	}
	blakeSolutions, err := (&puzzle.ComputeSolver{}).Solve(ctx, blake)
	if err != nil {
		t.Fatal(err)
	}
	blakeSigned, err := blake.Serialize(ctx, verifier.Salt.Value(), property.Salt)
	if err != nil {
		t.Fatal(err)
	}
	var blakeEncoded bytes.Buffer
	blakeEncoded.WriteString(blakeSolutions.String())
	blakeEncoded.WriteByte('.')
	if err := blakeSigned.Write(&blakeEncoded); err != nil {
		t.Fatal(err)
	}
	blakePayload, err := verifier.ParseSolutionPayload(ctx, blakeEncoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if result, err := verifier.Verify(ctx, blakePayload, argonOwner{user.ID}, time.Now().UTC()); err != nil || result.Error != puzzle.VerifyNoError {
		t.Fatalf("Blake verification with Argon capacity occupied: result = %+v, err = %v", result, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(parts[0]))
	if err != nil {
		t.Fatal(err)
	}
	for _, wrong := range [][]byte{decoded[:len(decoded)-puzzle.SolutionLength], append(bytes.Clone(decoded), make([]byte, puzzle.SolutionLength)...)} {
		encodedWrong := []byte(base64.StdEncoding.EncodeToString(wrong))
		if _, err := verifier.ParseSolutionPayload(ctx, bytes.Join([][]byte{encodedWrong, parts[1], parts[2]}, []byte{'.'})); err == nil {
			t.Fatal("wrong-count Argon payload parsed")
		}
	}
}
