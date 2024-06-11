package billing

import (
	"context"

	paddle "github.com/PaddleHQ/paddle-go-sdk"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type CustomerInfo struct {
	Email string
	Name  string
}

type PaddleAPI interface {
	GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error)
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
