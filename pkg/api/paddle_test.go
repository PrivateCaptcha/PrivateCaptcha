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
					ID:                 "",
					ProductID:          "123456",
					Description:        "",
					Type:               "",
					Name:               new(string),
					BillingCycle:       &paddlenotification.Duration{},
					TrialPeriod:        &paddlenotification.Duration{},
					TaxMode:            "",
					UnitPrice:          paddlenotification.Money{},
					UnitPriceOverrides: []paddlenotification.UnitPriceOverride{},
					Quantity:           paddlenotification.PriceQuantity{},
					Status:             "",
					CustomData:         map[string]any{},
					ImportMeta:         &paddlenotification.ImportMeta{},
					CreatedAt:          "",
					UpdatedAt:          "",
				},
			}},
			CustomData: map[string]any{},
			ImportMeta: &paddlenotification.ImportMeta{},
		},
	}

	ci := &billing.CustomerInfo{Email: t.Name() + "@example.com", Name: t.Name()}
	s.paddleAPI.(*billing.StubPaddleClient).CustomerInfo = ci

	resp, err := subscriptionCreatedSuite(evt, auth.privateAPIKey)
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

func subscriptionCreatedSuite(evt *paddle.SubscriptionCreatedEvent, token string) (*http.Response, error) {
	srv := http.NewServeMux()
	s.Setup(srv, "", auth)

	data, _ := json.Marshal(evt)
	req, err := http.NewRequest(http.MethodPost, "/"+common.PaddleSubscriptionCreated, bytes.NewReader(data))
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
