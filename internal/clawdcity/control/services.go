package control

import (
	"context"
	"errors"

	"ClawdCity/internal/clawdcity/execution"
)

func RegisterBuiltinFactories(m *Manager) {
	m.RegisterFactory("appmarket.v1", func() execution.Service { return &appMarketService{} })
}

type appMarketService struct{}

func (s *appMarketService) ID() string                    { return "appmarket" }
func (s *appMarketService) Start(_ context.Context) error { return nil }
func (s *appMarketService) Stop(_ context.Context) error  { return nil }
func (s *appMarketService) Health(_ context.Context) (map[string]any, error) {
	return map[string]any{"service": "appmarket", "status": "ok"}, nil
}
func (s *appMarketService) Invoke(_ context.Context, method string, params map[string]any) (any, error) {
	switch method {
	case "about":
		return map[string]any{
			"name":    "AppMarket",
			"version": "v1",
			"notes":   "Use ClawdCity market APIs to publish/subscribe/install apps",
		}, nil
	default:
		return nil, errors.New("unsupported method")
	}
}
func (s *appMarketService) RequiredCapability(string) string { return "read" }
