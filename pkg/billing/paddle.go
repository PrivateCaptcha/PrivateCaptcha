package billing

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	paddle "github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errInvalidArgument = errors.New("invalid argument")
)

type CustomerInfo struct {
	Email string
	Name  string
}

type ManagementURLs struct {
	CancelURL string
	UpdateURL string
}

type ChangePreview struct {
	CreditAmount   int
	CreditCurrency string
	ChargeAmount   int
	ChargeCurrency string
}

type Prices map[string]int

type PaddleAPI interface {
	GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error)
	GetManagementURLs(ctx context.Context, subscriptionID string) (*ManagementURLs, error)
	GetPrices(ctx context.Context, productIDs []string) (Prices, error)
	PreviewChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) (*ChangePreview, error)
	ChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) error
	CancelSubscription(ctx context.Context, subscriptionID string) error
}

type paddleClient struct {
	sdk *paddle.SDK
}

var _ PaddleAPI = (*paddleClient)(nil)

func NewPaddleAPI(getenv func(string) string) (PaddleAPI, error) {
	paddleURL := getenv("PADDLE_BASE_URL")
	if len(paddleURL) == 0 {
		stage := getenv("STAGE")

		paddleURL = paddle.SandboxBaseURL
		if stage == common.StageProd {
			paddleURL = paddle.ProductionBaseURL
		}
	}

	pc, err := paddle.New(getenv("PADDLE_API_KEY"), paddle.WithBaseURL(paddleURL))
	if err != nil {
		return nil, err
	}

	return &paddleClient{sdk: pc}, nil
}

func (pc *paddleClient) GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error) {
	if len(customerID) == 0 {
		return nil, errInvalidArgument
	}

	// TODO: Add retry with exponential backoff for paddle calls
	if customer, err := pc.sdk.GetCustomer(ctx, &paddle.GetCustomerRequest{CustomerID: customerID}); err == nil {
		return &CustomerInfo{
			Name:  *customer.Name,
			Email: customer.Email,
		}, nil
	} else {
		return nil, err
	}
}

func (pc *paddleClient) GetManagementURLs(ctx context.Context, subscriptionID string) (*ManagementURLs, error) {
	if len(subscriptionID) == 0 {
		return nil, errInvalidArgument
	}

	// NOTE: we should NOT cache URL responses per Paddle doc
	if subscription, err := pc.sdk.GetSubscription(ctx, &paddle.GetSubscriptionRequest{
		SubscriptionID: subscriptionID,
	}); err == nil {
		urls := &ManagementURLs{
			CancelURL: subscription.ManagementURLs.Cancel,
		}
		if subscription.ManagementURLs.UpdatePaymentMethod != nil {
			urls.UpdateURL = *subscription.ManagementURLs.UpdatePaymentMethod
		}
		return urls, nil
	} else {
		return nil, err
	}
}

func (pc *paddleClient) GetPrices(ctx context.Context, productIDs []string) (Prices, error) {
	prices, err := pc.sdk.ListPrices(ctx, &paddle.ListPricesRequest{
		ProductID: productIDs,
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list Paddle prices", common.ErrAttr(err))
		return map[string]int{}, err
	}

	result := make(map[string]int)

	err = prices.Iter(ctx, func(v *paddle.PriceIncludes) (bool, error) {
		amountStr := v.UnitPrice.Amount
		if cents, cerr := strconv.Atoi(amountStr); cerr == nil {
			result[v.ID] = cents / 100
		}
		return true, nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to iterate the prices", common.ErrAttr(err))
		return result, err
	}

	slog.DebugContext(ctx, "Fetched Paddle prices", "prices", len(result), "products", len(productIDs))

	return result, nil
}

func (pc *paddleClient) PreviewChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) (*ChangePreview, error) {
	if (len(subscriptionID) == 0) || (len(priceID) == 0) {
		return nil, errInvalidArgument
	}

	prorationMode := paddle.ProrationBillingModeProratedImmediately

	response, err := pc.sdk.PreviewSubscription(ctx, &paddle.PreviewSubscriptionRequest{
		SubscriptionID: subscriptionID,
		Items: []paddle.SubscriptionsCatalogItem{{
			PriceID:  priceID,
			Quantity: quantity,
		}},
		ProrationBillingMode: &prorationMode,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to preview Paddle subscription update", common.ErrAttr(err))
		return nil, err
	}

	chargeAmount, _ := strconv.Atoi(response.UpdateSummary.Charge.Amount)
	creditAmount, _ := strconv.Atoi(response.UpdateSummary.Credit.Amount)

	return &ChangePreview{
		CreditAmount:   creditAmount,
		CreditCurrency: response.UpdateSummary.Credit.CurrencyCode,
		ChargeAmount:   chargeAmount,
		ChargeCurrency: response.UpdateSummary.Charge.CurrencyCode,
	}, nil
}

func (pc *paddleClient) ChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) error {
	if (len(subscriptionID) == 0) || (len(priceID) == 0) {
		return errInvalidArgument
	}

	// NOTE: we currently prefer subscription_updated handler to be a single point to update subscription data
	_, err := pc.sdk.UpdateSubscription(ctx, &paddle.UpdateSubscriptionRequest{
		SubscriptionID: subscriptionID,
		Items: paddle.NewPatchField([]paddle.SubscriptionsCatalogItem{{
			PriceID:  priceID,
			Quantity: quantity,
		}}),
		ProrationBillingMode: paddle.NewPatchField(paddle.ProrationBillingModeProratedImmediately),
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update Paddle subscription", "subscriptionID", subscriptionID, "priceID", priceID,
			common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Changed Paddle subscription", "subscriptionID", subscriptionID, "priceID", priceID)

	return nil
}

func (pc *paddleClient) CancelSubscription(ctx context.Context, subscriptionID string) error {
	if len(subscriptionID) == 0 {
		return errInvalidArgument
	}

	_, err := pc.sdk.CancelSubscription(ctx, &paddle.CancelSubscriptionRequest{
		SubscriptionID: subscriptionID,
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to cancel Paddle subscription", "subscriptionID", subscriptionID, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Cancelled Paddle subscription", "subscriptionID", subscriptionID)

	return nil
}
