package billing

import (
	"context"
	"errors"

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

type PaddleAPI interface {
	GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error)
	GetManagementURLs(ctx context.Context, subscriptionID string) (*ManagementURLs, error)
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
