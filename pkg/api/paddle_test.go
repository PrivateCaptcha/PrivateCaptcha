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

	"github.com/PaddleHQ/paddle-go-sdk/v3/pkg/paddlenotification"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/rs/xid"
)

func stubSubscriptionCreatedEvent() *paddlenotification.SubscriptionCreated {
	return &paddlenotification.SubscriptionCreated{
		GenericNotificationEvent: paddlenotification.GenericNotificationEvent{},
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
			Discount:       &paddlenotification.SubscriptionDiscountTimePeriod{},
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
					Status:       paddlenotification.StatusActive,
				},
			}},
			CustomData: paddlenotification.CustomData{},
		},
	}
}

func subscriptionCreatedSuite(ctx context.Context, evt *paddlenotification.SubscriptionCreated, email string, t *testing.T) {
	resp, err := paddleSuite(evt, common.PaddleSubscriptionCreated, s.Auth.privateAPIKey.Value())
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected response code: %v", resp.StatusCode)
	}

	user, err := store.FindUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}

	if !user.SubscriptionID.Valid {
		t.Fatal("User subscription is still not valid")
	}

	subscription, err := store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		t.Errorf("Subscription was not found in the DB: %v", err)
	}

	if subscription.PaddleSubscriptionID.String != evt.Data.ID {
		t.Errorf("Unexpected Paddle subscription ID in the DB: %v (expected %v", subscription.PaddleSubscriptionID, evt.Data.ID)
	}
}

func TestSubscriptionCreated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	evt := stubSubscriptionCreatedEvent()

	ci := &billing.CustomerInfo{Email: t.Name() + "@example.com", Name: t.Name()}
	s.PaddleAPI.(*billing.StubPaddleClient).CustomerInfo = ci

	subscriptionCreatedSuite(context.TODO(), evt, ci.Email, t)
}

func TestSubscriptionCreatedWithExisting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	user, _, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	evt := stubSubscriptionCreatedEvent()
	evt.Data.CustomData[pcUserPaddlePassthroughKey] = strconv.Itoa(int(user.ID))

	ci := &billing.CustomerInfo{Email: user.Email, Name: t.Name()}
	s.PaddleAPI.(*billing.StubPaddleClient).CustomerInfo = ci

	subscriptionCreatedSuite(ctx, evt, ci.Email, t)
}

func paddleSuite(evt any, endpoint, token string) (*http.Response, error) {
	srv := http.NewServeMux()
	s.Setup(srv, "", true /*verbose*/, common.NoopMiddleware)

	data, _ := json.Marshal(evt)
	req, err := http.NewRequest(http.MethodPost, "/"+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set(common.HeaderContentType, common.ContentTypeJSON)
	req.Header.Add(common.HeaderContentLength, strconv.Itoa(len(data)))
	req.Header.Set(common.HeaderAuthorization, "Bearer "+token)
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), generateRandomIPv4())

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

	evt := &paddlenotification.SubscriptionUpdated{
		GenericNotificationEvent: paddlenotification.GenericNotificationEvent{},
		Data: paddlenotification.SubscriptionNotification{
			ID:             subscription.PaddleSubscriptionID.String,
			Status:         paddlenotification.SubscriptionStatusPaused,
			UpdatedAt:      common.JSONTimeNow().String(),
			PausedAt:       &pausedAt,
			CanceledAt:     new(string),
			CollectionMode: "automatic",
			CurrentBillingPeriod: &paddlenotification.TimePeriod{
				StartsAt: common.JSONTimeNow().String(),
				EndsAt:   common.JSONTimeNowAdd(30 * 24 * time.Hour).String(),
			},
			Items: []paddlenotification.SubscriptionItem{{
				Status:             paddlenotification.SubscriptionItemStatusInactive,
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
					Status:       paddlenotification.StatusActive,
				},
			}},
		},
	}

	resp, err := paddleSuite(evt, common.PaddleSubscriptionUpdated, s.Auth.privateAPIKey.Value())
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
	if subscription.Status != string(paddlenotification.SubscriptionStatusPaused) {
		t.Errorf("Unexpected subscription status: %v", subscription.Status)
	}
}
