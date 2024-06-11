package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PaddleHQ/paddle-go-sdk/pkg/paddlenotification"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/rs/xid"
)

func TestSubscriptionCreated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	evt := &paddle.SubscriptionCreatedEvent{
		GenericEvent: paddle.GenericEvent{},
		Data: paddlenotification.SubscriptionCreatedNotification{
			ID:             xid.New().String(),
			TransactionID:  xid.New().String(),
			Status:         "trialing",
			CustomerID:     xid.New().String(),
			AddressID:      xid.New().String(),
			BusinessID:     new(string),
			CurrencyCode:   "EUR",
			CreatedAt:      common.JSONTimeNow().String(),
			UpdatedAt:      common.JSONTimeNow().String(),
			StartedAt:      new(string),
			FirstBilledAt:  new(string),
			NextBilledAt:   new(string),
			PausedAt:       new(string),
			CanceledAt:     new(string),
			Discount:       &paddlenotification.SubscriptionDiscount{},
			CollectionMode: "automatic",
			BillingDetails: &paddlenotification.BillingDetails{},
			CurrentBillingPeriod: &paddlenotification.TimePeriod{
				StartsAt: common.JSONTimeNow().String(),
				EndsAt:   common.JSONTimeNowAdd(30 * 24 * time.Hour).String(),
			},
			BillingCycle:    paddlenotification.Duration{},
			ScheduledChange: &paddlenotification.SubscriptionScheduledChange{},
			Items: []paddlenotification.SubscriptionItem{{
				Status:             "trialing",
				Quantity:           1,
				Recurring:          true,
				CreatedAt:          common.JSONTimeNow().String(),
				UpdatedAt:          common.JSONTimeNow().String(),
				PreviouslyBilledAt: new(string),
				NextBilledAt:       new(string),
				TrialDates: &paddlenotification.TimePeriod{
					StartsAt: common.JSONTimeNow().String(),
					EndsAt:   common.JSONTimeNowAdd(30 * 24 * time.Hour).String(),
				},
				Price: paddlenotification.Price{
					ID:           xid.New().String(),
					ProductID:    "123456",
					Name:         new(string),
					BillingCycle: &paddlenotification.Duration{},
					TrialPeriod:  &paddlenotification.Duration{},
					TaxMode:      "",
					Quantity:     paddlenotification.PriceQuantity{},
					Status:       paddle.SubscriptionStatusActive,
				},
			}},
		},
	}

	ci := &billing.CustomerInfo{Email: t.Name() + "@example.com", Name: t.Name()}
	s.paddleAPI.(*billing.StubPaddleClient).CustomerInfo = ci

	resp, err := paddleSuite(evt, common.PaddleSubscriptionCreated, auth.privateAPIKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected response code: %v", resp.StatusCode)
	}

	if user, err := store.FindUser(context.TODO(), ci.Email); err != nil || (user.Email != ci.Email) {
		t.Errorf("User was not created in the DB")
	}
}

func paddleSuite(evt any, endpoint, token string) (*http.Response, error) {
	srv := http.NewServeMux()
	s.Setup(srv, "", auth)

	data, _ := json.Marshal(evt)
	req, err := http.NewRequest(http.MethodPost, "/"+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderContentType, common.ContentTypeJSON)
	req.Header.Add(common.HeaderContentLength, strconv.Itoa(len(data)))
	req.Header.Set(common.HeaderAuthorization, "Bearer "+token)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func TestSubscriptionUpdated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create new account: %v", err)
	}

	subscription, err := store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		t.Fatalf("Failed to retrieve user subscription: %v", err)
	}

	pausedAt := common.JSONTimeNow().String()

	evt := &paddle.SubscriptionUpdatedEvent{
		GenericEvent: paddle.GenericEvent{},
		Data: paddlenotification.SubscriptionNotification{
			ID:             subscription.PaddleSubscriptionID,
			Status:         paddle.SubscriptionStatusPaused,
			UpdatedAt:      common.JSONTimeNow().String(),
			PausedAt:       &pausedAt,
			CanceledAt:     new(string),
			CollectionMode: "automatic",
			CurrentBillingPeriod: &paddlenotification.TimePeriod{
				StartsAt: common.JSONTimeNow().String(),
				EndsAt:   common.JSONTimeNowAdd(30 * 24 * time.Hour).String(),
			},
			Items: []paddlenotification.SubscriptionItem{{
				Status:             paddle.SubscriptionStatusPaused,
				Quantity:           1,
				Recurring:          true,
				CreatedAt:          common.JSONTimeNow().String(),
				UpdatedAt:          common.JSONTimeNow().String(),
				PreviouslyBilledAt: new(string),
				NextBilledAt:       new(string),
				TrialDates: &paddlenotification.TimePeriod{
					StartsAt: common.JSONTimeNow().String(),
					EndsAt:   common.JSONTimeNowAdd(30 * 24 * time.Hour).String(),
				},
				Price: paddlenotification.Price{
					ID:           xid.New().String(),
					ProductID:    "123456",
					Name:         new(string),
					BillingCycle: &paddlenotification.Duration{},
					TrialPeriod:  &paddlenotification.Duration{},
					TaxMode:      "",
					Quantity:     paddlenotification.PriceQuantity{},
					Status:       paddle.SubscriptionStatusActive,
				},
			}},
		},
	}

	resp, err := paddleSuite(evt, common.PaddleSubscriptionUpdated, auth.privateAPIKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected response code: %v", resp.StatusCode)
	}

	subscription, err = store.RetrieveSubscription(context.TODO(), user.SubscriptionID.Int32)
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Status != paddle.SubscriptionStatusPaused {
		t.Errorf("Unexpected subscription status: %v", subscription.Status)
	}
}
