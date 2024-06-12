package billing

import "context"

type StubPaddleClient struct {
	CustomerInfo *CustomerInfo
	URLs         *ManagementURLs
}

var _ PaddleAPI = (*StubPaddleClient)(nil)

func (pc *StubPaddleClient) GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error) {
	return pc.CustomerInfo, nil
}

func (pc *StubPaddleClient) GetManagementURLs(ctx context.Context, subscriptionID string) (*ManagementURLs, error) {
	return pc.URLs, nil
}
