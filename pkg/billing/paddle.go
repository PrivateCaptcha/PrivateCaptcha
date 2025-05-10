package billing

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v3"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/jpillora/backoff"
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
	Environment() string
	ClientToken() string
	GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error)
	GetManagementURLs(ctx context.Context, subscriptionID string) (*ManagementURLs, error)
	GetPrices(ctx context.Context, productIDs []string) (Prices, error)
	PreviewChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) (*ChangePreview, error)
	ChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) error
	CancelSubscription(ctx context.Context, subscriptionID string) error
}

type paddleClient struct {
	sdk         *paddle.SDK
	timeout     time.Duration
	environment common.ConfigItem
	clientToken common.ConfigItem
}

var _ PaddleAPI = (*paddleClient)(nil)

func NewPaddleAPI(cfg common.ConfigStore) (PaddleAPI, error) {
	paddleURL := cfg.Get(common.PaddleBaseURLKey).Value()
	if len(paddleURL) == 0 {
		stage := cfg.Get(common.StageKey).Value()

		paddleURL = paddle.SandboxBaseURL
		if stage == common.StageProd {
			paddleURL = paddle.ProductionBaseURL
		}
	}

	apiKey := cfg.Get(common.PaddleAPIKeyKey)
	pc, err := paddle.New(apiKey.Value(), paddle.WithBaseURL(paddleURL))
	if err != nil {
		return nil, err
	}

	return &retryPaddleClient{
		paddleAPI: &paddleClient{
			sdk:         pc,
			environment: cfg.Get(common.PaddleEnvironmentKey),
			clientToken: cfg.Get(common.PaddleClientTokenKey),
			timeout:     10 * time.Second,
		},
		attempts:   5,
		minBackoff: 500 * time.Millisecond,
		maxBackoff: 4 * time.Second,
	}, nil
}

func (pc *paddleClient) Environment() string {
	return pc.environment.Value()
}

func (pc *paddleClient) ClientToken() string {
	return pc.clientToken.Value()
}

func (pc *paddleClient) paddleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, pc.timeout)
}

func (pc *paddleClient) GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error) {
	if len(customerID) == 0 {
		return nil, ErrInvalidArgument
	}

	ctx, cancel := pc.paddleContext(ctx)
	defer cancel()

	slog.DebugContext(ctx, "About to query customer info", "customerID", customerID)
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
		return nil, ErrInvalidArgument
	}

	ctx, cancel := pc.paddleContext(ctx)
	defer cancel()

	slog.DebugContext(ctx, "About to fetch management URLs", "subscriptionID", subscriptionID)
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
	if len(productIDs) == 0 {
		return nil, ErrInvalidArgument
	}

	ctx, cancel := pc.paddleContext(ctx)
	defer cancel()

	slog.DebugContext(ctx, "About to fetch product prices", "products", len(productIDs))
	prices, err := pc.sdk.ListPrices(ctx, &paddle.ListPricesRequest{
		ProductID: productIDs,
	})
	if err != nil {
		return map[string]int{}, err
	}

	result := make(map[string]int)

	err = prices.Iter(ctx, func(v *paddle.Price) (bool, error) {
		amountStr := v.UnitPrice.Amount
		if cents, cerr := strconv.Atoi(amountStr); cerr == nil {
			result[v.ID] = cents / 100
		}
		return true, nil
	})

	if err != nil {
		return result, err
	}

	slog.DebugContext(ctx, "Fetched Paddle prices", "prices", len(result), "products", len(productIDs))

	return result, nil
}

func (pc *paddleClient) PreviewChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) (*ChangePreview, error) {
	if (len(subscriptionID) == 0) || (len(priceID) == 0) {
		return nil, ErrInvalidArgument
	}

	ctx, cancel := pc.paddleContext(ctx)
	defer cancel()

	prorationMode := paddle.ProrationBillingModeProratedImmediately

	slog.DebugContext(ctx, "About to preview change subscription", "subscriptionID", subscriptionID)
	response, err := pc.sdk.PreviewSubscriptionUpdate(ctx, &paddle.PreviewSubscriptionUpdateRequest{
		SubscriptionID: subscriptionID,
		Items: paddle.NewPatchField([]paddle.PreviewSubscriptionUpdateItems{
			*paddle.NewPreviewSubscriptionUpdateItemsSubscriptionUpdateItemFromCatalog(&paddle.SubscriptionUpdateItemFromCatalog{
				PriceID:  priceID,
				Quantity: quantity,
			}),
		}),
		ProrationBillingMode: paddle.NewPatchField(prorationMode),
	})

	if err != nil {
		return nil, err
	}

	chargeAmount, _ := strconv.Atoi(response.UpdateSummary.Charge.Amount)
	creditAmount, _ := strconv.Atoi(response.UpdateSummary.Credit.Amount)

	return &ChangePreview{
		CreditAmount:   creditAmount,
		CreditCurrency: string(response.UpdateSummary.Credit.CurrencyCode),
		ChargeAmount:   chargeAmount,
		ChargeCurrency: string(response.UpdateSummary.Charge.CurrencyCode),
	}, nil
}

func (pc *paddleClient) ChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) error {
	if (len(subscriptionID) == 0) || (len(priceID) == 0) {
		return ErrInvalidArgument
	}

	ctx, cancel := pc.paddleContext(ctx)
	defer cancel()

	slog.DebugContext(ctx, "About to change subscription", "subscriptionID", subscriptionID, "priceID", priceID)
	// NOTE: we currently prefer subscription_updated handler to be a single point to update subscription data
	_, err := pc.sdk.UpdateSubscription(ctx, &paddle.UpdateSubscriptionRequest{
		SubscriptionID: subscriptionID,
		Items: paddle.NewPatchField([]paddle.UpdateSubscriptionItems{
			*paddle.NewUpdateSubscriptionItemsSubscriptionUpdateItemFromCatalog(&paddle.SubscriptionUpdateItemFromCatalog{
				PriceID:  priceID,
				Quantity: quantity,
			}),
		}),
		ProrationBillingMode: paddle.NewPatchField(paddle.ProrationBillingModeProratedImmediately),
	})

	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "Changed Paddle subscription", "subscriptionID", subscriptionID, "priceID", priceID)

	return nil
}

func (pc *paddleClient) CancelSubscription(ctx context.Context, subscriptionID string) error {
	if len(subscriptionID) == 0 {
		return ErrInvalidArgument
	}

	ctx, cancel := pc.paddleContext(ctx)
	defer cancel()

	slog.DebugContext(ctx, "About to cancel subscription", "subscriptionID", subscriptionID)
	_, err := pc.sdk.CancelSubscription(ctx, &paddle.CancelSubscriptionRequest{
		SubscriptionID: subscriptionID,
	})

	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "Cancelled Paddle subscription", "subscriptionID", subscriptionID)

	return nil
}

type retryPaddleClient struct {
	paddleAPI  PaddleAPI
	attempts   int
	minBackoff time.Duration
	maxBackoff time.Duration
}

var _ PaddleAPI = (*retryPaddleClient)(nil)

func (rpc *retryPaddleClient) Environment() string {
	return rpc.paddleAPI.Environment()
}

func (rpc *retryPaddleClient) ClientToken() string {
	return rpc.paddleAPI.ClientToken()
}

func (rpc *retryPaddleClient) GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error) {
	var ci *CustomerInfo
	var err error

	b := &backoff.Backoff{
		Min:    rpc.minBackoff,
		Max:    rpc.maxBackoff,
		Factor: 2,
		Jitter: true,
	}

	for i := 0; i < rpc.attempts; i++ {
		ci, err = rpc.paddleAPI.GetCustomerInfo(ctx, customerID)
		if err == nil {
			break
		} else {
			slog.WarnContext(ctx, "Failed to get customer info", "attempt", i, "customerID", customerID, common.ErrAttr(err))
			time.Sleep(b.Duration())
		}
	}

	return ci, err
}

func (rpc *retryPaddleClient) GetManagementURLs(ctx context.Context, subscriptionID string) (*ManagementURLs, error) {
	var mu *ManagementURLs
	var err error

	b := &backoff.Backoff{
		Min:    rpc.minBackoff,
		Max:    rpc.maxBackoff,
		Factor: 2,
		Jitter: true,
	}

	for i := 0; i < rpc.attempts; i++ {
		mu, err = rpc.paddleAPI.GetManagementURLs(ctx, subscriptionID)
		if err == nil {
			break
		} else {
			slog.WarnContext(ctx, "Failed to get management URLs", "attempt", i, "subscriptionID", subscriptionID, common.ErrAttr(err))
			time.Sleep(b.Duration())
		}
	}

	return mu, err
}

func (rpc *retryPaddleClient) GetPrices(ctx context.Context, productIDs []string) (Prices, error) {
	var prices Prices
	var err error

	b := &backoff.Backoff{
		Min:    rpc.minBackoff,
		Max:    rpc.maxBackoff,
		Factor: 2,
		Jitter: true,
	}

	for i := 0; i < rpc.attempts; i++ {
		prices, err = rpc.paddleAPI.GetPrices(ctx, productIDs)
		if err == nil {
			break
		} else {
			slog.WarnContext(ctx, "Failed to get products prices", "attempt", i, "count", len(productIDs), common.ErrAttr(err))
			time.Sleep(b.Duration())
		}
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to list Paddle prices", common.ErrAttr(err))
	}

	return prices, err
}

func (rpc *retryPaddleClient) PreviewChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) (*ChangePreview, error) {
	var preview *ChangePreview
	var err error

	b := &backoff.Backoff{
		Min:    rpc.minBackoff,
		Max:    rpc.maxBackoff,
		Factor: 2,
		Jitter: true,
	}

	for i := 0; i < rpc.attempts; i++ {
		preview, err = rpc.paddleAPI.PreviewChangeSubscription(ctx, subscriptionID, priceID, quantity)
		if err == nil {
			break
		} else {
			slog.WarnContext(ctx, "Failed to get change preview", "attempt", i, "subscriptionID", subscriptionID, common.ErrAttr(err))
			time.Sleep(b.Duration())
		}
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to preview Paddle subscription update", common.ErrAttr(err))
	}

	return preview, err
}

func (rpc *retryPaddleClient) ChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) error {
	var err error

	b := &backoff.Backoff{
		Min:    rpc.minBackoff,
		Max:    rpc.maxBackoff,
		Factor: 2,
		Jitter: true,
	}

	for i := 0; i < rpc.attempts; i++ {
		err = rpc.paddleAPI.ChangeSubscription(ctx, subscriptionID, priceID, quantity)
		if err == nil {
			break
		} else {
			slog.WarnContext(ctx, "Failed to change subscription", "attempt", i, "subscriptionID", subscriptionID, common.ErrAttr(err))
			time.Sleep(b.Duration())
		}
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to update Paddle subscription", "subscriptionID", subscriptionID, "priceID", priceID,
			common.ErrAttr(err))
	}

	return err
}

func (rpc *retryPaddleClient) CancelSubscription(ctx context.Context, subscriptionID string) error {
	var err error

	b := &backoff.Backoff{
		Min:    rpc.minBackoff,
		Max:    rpc.maxBackoff,
		Factor: 2,
		Jitter: true,
	}

	for i := 0; i < rpc.attempts; i++ {
		err = rpc.paddleAPI.CancelSubscription(ctx, subscriptionID)
		if err == nil {
			break
		} else {
			slog.WarnContext(ctx, "Failed to cancel Paddle subscription", "attempt", i, "subscriptionID", subscriptionID, common.ErrAttr(err))
			time.Sleep(b.Duration())
		}
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to cancel Paddle subscription", "subscriptionID", subscriptionID, common.ErrAttr(err))
	}

	return err
}
