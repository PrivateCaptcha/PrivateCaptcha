package billing

import "context"

type StubPaddleClient struct {
	CustomerInfo *CustomerInfo
}

var _ PaddleAPI = (*StubPaddleClient)(nil)

func (pc *StubPaddleClient) GetCustomerInfo(ctx context.Context, customerID string) (*CustomerInfo, error) {
	return pc.CustomerInfo, nil
}
