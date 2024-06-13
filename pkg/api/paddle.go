package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PaddleHQ/paddle-go-sdk/pkg/paddlenotification"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func findProductID(ctx context.Context, items []paddlenotification.SubscriptionItem, stage string) int {
	if len(items) == 0 {
		return -1
	}
	j := -1
	for i, subscr := range items {
		if _, err := billing.FindPlanByPaddlePrice(subscr.Price.ProductID, subscr.Price.ID, stage); err == nil {
			j = i
			break
		}
	}

	if j == -1 {
		slog.ErrorContext(ctx, "Failed to find a known plan from subscription")
		if len(items) == 1 {
			j = 0
		} else {
			slog.ErrorContext(ctx, "Unexpected number of subscription items", "count", len(items))
			return -1
		}
	}

	return j
}

func (s *server) newCreateSubscriptionParams(ctx context.Context, evt *paddle.SubscriptionCreatedEvent) (*dbgen.CreateSubscriptionParams, error) {
	j := findProductID(ctx, evt.Data.Items, s.stage)
	if j == -1 {
		return nil, errProductNotFound
	}

	subscr := evt.Data.Items[j]

	params := &dbgen.CreateSubscriptionParams{
		PaddlePriceID:        subscr.Price.ID,
		PaddleProductID:      subscr.Price.ProductID,
		PaddleSubscriptionID: evt.Data.ID,
		PaddleCustomerID:     evt.Data.CustomerID,
		Status:               evt.Data.Status,
	}

	if subscr.TrialDates != nil {
		if trialEndTime, err := time.Parse(time.RFC3339, subscr.TrialDates.EndsAt); err == nil {
			params.TrialEndsAt = db.Timestampz(trialEndTime)
		} else {
			slog.ErrorContext(ctx, "Failed to parse trial end time", "time", subscr.TrialDates.EndsAt, common.ErrAttr(err))
		}
	}

	if subscr.NextBilledAt != nil {
		if nextBillTime, err := time.Parse(time.RFC3339, *subscr.NextBilledAt); err == nil {
			params.NextBilledAt = db.Timestampz(nextBillTime)
		} else {
			slog.ErrorContext(ctx, "Failed to parse next bill time", "time", *subscr.NextBilledAt, common.ErrAttr(err))
		}
	}

	return params, nil
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
		slog.ErrorContext(ctx, "Failed to parse subscription created event", common.ErrAttr(err))
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

	// TODO: Handle case when user has account without subscription
	// we're already passing "privateCaptchaUserID" in CustomData to Paddle (in settings.html)
	if _, _, err = s.businessDB.CreateNewAccount(ctx, subscrParams, customer.Email, customer.Name, orgName); (err != nil) && (err != db.ErrDuplicateAccount) {
		elog.ErrorContext(ctx, "Failed to create a new account", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) newUpdateSubscriptionParams(ctx context.Context, data *paddlenotification.SubscriptionNotification) (*dbgen.UpdateSubscriptionParams, error) {
	j := findProductID(ctx, data.Items, s.stage)
	if j == -1 {
		return nil, errProductNotFound
	}

	subscr := data.Items[j]

	params := &dbgen.UpdateSubscriptionParams{
		PaddleProductID:      subscr.Price.ProductID,
		PaddleSubscriptionID: data.ID,
		Status:               data.Status,
	}

	if subscr.NextBilledAt != nil {
		if nextBillTime, err := time.Parse(time.RFC3339, *subscr.NextBilledAt); err == nil {
			params.NextBilledAt = db.Timestampz(nextBillTime)
		} else {
			slog.ErrorContext(ctx, "Failed to parse next bill time", "time", *subscr.NextBilledAt, common.ErrAttr(err))
		}
	}

	return params, nil
}

func (s *server) subscriptionUpdated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	evt := &paddle.SubscriptionUpdatedEvent{}
	if err := json.Unmarshal(body, evt); err != nil {
		slog.ErrorContext(ctx, "Failed to parse subscription updated request", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	elog := slog.With("eventID", evt.EventID, "subscriptionID", evt.Data.ID)
	elog.DebugContext(ctx, "Handling subscription updated event")

	subscrParams, err := s.newUpdateSubscriptionParams(ctx, &evt.Data)
	if err != nil {
		elog.ErrorContext(ctx, "Failed to process paddle event", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err = s.businessDB.UpdateSubscription(ctx, subscrParams); err != nil {
		elog.ErrorContext(ctx, "Failed to update the subscription", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) subscriptionCancelled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	evt := &paddle.SubscriptionCanceledEvent{}
	if err := json.Unmarshal(body, evt); err != nil {
		slog.ErrorContext(ctx, "Failed to parse subscription cancelled request", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	elog := slog.With("eventID", evt.EventID, "subscriptionID", evt.Data.ID)
	elog.DebugContext(ctx, "Handling subscription cancelled event")

	subscrParams, err := s.newUpdateSubscriptionParams(ctx, &evt.Data)
	if err != nil {
		elog.ErrorContext(ctx, "Failed to process paddle event", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err = s.businessDB.UpdateSubscription(ctx, subscrParams); err != nil {
		elog.ErrorContext(ctx, "Failed to update the subscription", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
