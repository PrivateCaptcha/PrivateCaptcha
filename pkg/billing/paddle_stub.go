package billing

import "context"

type StubPaddleClient struct {
	CustomerInfo *CustomerInfo
	URLs         *ManagementURLs
	Prices       map[string]int
}

var _ PaddleAPI = (*StubPaddleClient)(nil)

func (pc *StubPaddleClient) GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error) {
	return pc.CustomerInfo, nil
}

func (pc *StubPaddleClient) GetManagementURLs(ctx context.Context, subscriptionID string) (*ManagementURLs, error) {
	return pc.URLs, nil
}

func (pc *StubPaddleClient) GetPrices(ctx context.Context, productIDs []string) (Prices, error) {
	return pc.Prices, nil
}

func (pc *StubPaddleClient) PreviewChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) (*ChangePreview, error) {
	return &ChangePreview{}, nil
}

func (pc *StubPaddleClient) ChangeSubscription(ctx context.Context, subscriptionID string, priceID string, quantity int) error {
	return nil
}
