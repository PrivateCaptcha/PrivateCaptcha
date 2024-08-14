package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PaddleHQ/paddle-go-sdk/pkg/paddlenotification"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// this field is set in the javascript of the billing page (currently in settings/scripts.html)
const pcUserPaddlePassthroughKey = "privateCaptchaUserID"

func findProductID(ctx context.Context, items []paddlenotification.SubscriptionItem, stage string) int {
	if len(items) == 0 {
		return -1
	}
	j := -1
	for i, subscr := range items {
		if _, err := billing.FindPlanByPriceAndProduct(subscr.Price.ProductID, subscr.Price.ID, stage); err == nil {
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

func (s *server) newCreateSubscriptionParams(ctx context.Context, evt *paddle.SubscriptionCreatedEvent) (*dbgen.CreateSubscriptionParams, int32, error) {
	j := findProductID(ctx, evt.Data.Items, s.stage)
	if j == -1 {
		return nil, -1, errProductNotFound
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

	userID := -1

	if data, ok := evt.Data.CustomData[pcUserPaddlePassthroughKey]; ok {
		if userIDStr, ok := data.(string); ok {
			if value, err := strconv.Atoi(userIDStr); err == nil {
				userID = value
			} else {
				slog.ErrorContext(ctx, "userID custom data present, but not valid", "userID", userIDStr, common.ErrAttr(err))
			}
		} else {
			slog.ErrorContext(ctx, "userID custom data present, but not string", "userID", userIDStr)
		}
	}

	return params, int32(userID), nil
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

	subscrParams, existingUserID, err := s.newCreateSubscriptionParams(ctx, evt)
	if err != nil {
		elog.ErrorContext(ctx, "Failed to process paddle event", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	orgName := common.OrgNameFromName(customer.Name)

	user, _, err := s.businessDB.CreateNewAccount(ctx, subscrParams, customer.Email, customer.Name, orgName, existingUserID)
	if (err != nil) && (err != db.ErrDuplicateAccount) {
		elog.ErrorContext(ctx, "Failed to create a new account", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if plan, err := billing.FindPlanByProductID(subscrParams.PaddleProductID, s.stage); err == nil {
		_ = s.timeSeries.UpdateUserLimits(ctx, map[int32]int64{user.ID: plan.RequestsLimit})
		s.auth.UnblockUserIfNeeded(ctx, user.ID, plan.RequestsLimit, subscrParams.Status)
	} else {
		elog.ErrorContext(ctx, "Failed to find Paddle plan", "productID", subscrParams.PaddleProductID, common.ErrAttr(err))
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

	if data.ScheduledChange != nil {
		if data.ScheduledChange.Action == paddle.ScheduledChangeActionCancel {
			if cancelTime, err := time.Parse(time.RFC3339, data.ScheduledChange.EffectiveAt); err == nil {
				params.CancelFrom = db.Timestampz(cancelTime)
			} else {
				slog.ErrorContext(ctx, "Failed to parse cancel time", "time", data.ScheduledChange.EffectiveAt, common.ErrAttr(err))
			}
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
	elog.InfoContext(ctx, "Handling subscription updated event")

	subscrParams, err := s.newUpdateSubscriptionParams(ctx, &evt.Data)
	if err != nil {
		elog.ErrorContext(ctx, "Failed to process paddle event", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	subscription, err := s.businessDB.UpdateSubscription(ctx, subscrParams)
	if err != nil {
		elog.ErrorContext(ctx, "Failed to update the subscription", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if plan, err := billing.FindPlanByProductID(subscrParams.PaddleProductID, s.stage); err == nil {
		if user, err := s.businessDB.FindUserBySubscriptionID(ctx, subscription.ID); err == nil {
			_ = s.timeSeries.UpdateUserLimits(ctx, map[int32]int64{user.ID: plan.RequestsLimit})
			s.auth.UnblockUserIfNeeded(ctx, user.ID, plan.RequestsLimit, subscription.Status)
		}
	} else {
		elog.ErrorContext(ctx, "Failed to find Paddle plan", "productID", subscrParams.PaddleProductID, common.ErrAttr(err))
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

	if subscription, err := s.businessDB.UpdateSubscription(ctx, subscrParams); err == nil {
		if user, err := s.businessDB.FindUserBySubscriptionID(ctx, subscription.ID); err == nil {
			s.auth.BlockUser(ctx, user.ID, 0 /*limit*/, subscription.Status)
		}
	} else {
		elog.ErrorContext(ctx, "Failed to update the subscription", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
