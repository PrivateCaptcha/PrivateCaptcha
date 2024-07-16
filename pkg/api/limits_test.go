package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func findUserViolation(violations []*common.UserTimeCount, userID int32) (*common.UserTimeCount, bool) {
	for _, v := range violations {
		if v.UserID == uint32(userID) {
			slog.Debug("User violation found", "userID", userID)
			return v, true
		}
	}

	return nil, false
}

func TestDetectUsageViolations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()
	tnow := time.Now()
	const requests = 1000

	user, org, err := db_test.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, err := db_test.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < requests+1; i++ {
		s.levels.Difficulty(common.RandomFingerprint(), property, tnow.Add(time.Duration(i)*time.Microsecond))
	}

	if err := timeSeries.UpdateUserLimits(ctx, map[int32]int64{user.ID: requests}); err != nil {
		t.Fatal(err)
	}

	// we need to wait for the timeout in the ProcessAccessLog()
	time.Sleep(1 * time.Second)

	job := &UsageLimitsJob{
		MaxUsers:   10,
		BusinessDB: store,
		TimeSeries: timeSeries,
		From:       tnow,
	}

	for attempt := 0; attempt < 5; attempt++ {
		violations, err := job.findViolations(ctx)
		if err != nil {
			t.Error(err)
		}

		if _, ok := findUserViolation(violations, user.ID); ok {
			break
		}

		slog.Debug("Violations not yet detected")
		time.Sleep(1 * time.Second)
	}

	violations, err := job.findViolations(ctx)
	if err != nil {
		t.Fatal(err)
	}

	v, ok := findUserViolation(violations, user.ID)
	if !ok {
		t.Fatal("Violations not detected")
	}

	if v.Count != requests+1 {
		t.Errorf("Unexpected requests count: %v", v.Count)
	}
}
