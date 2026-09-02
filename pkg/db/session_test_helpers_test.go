package db

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

type sessionMetricsStub struct{}

func (s *sessionMetricsStub) ObserveEventDropped(eventType common.MetricEventType) {}
func (s *sessionMetricsStub) ObservePanic()                                        {}
