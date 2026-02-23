package control

import (
	"context"
	"errors"

	"Assembler/internal/assembler/execution"
)

func RegisterBuiltinFactories(m *Manager) {
	m.RegisterFactory("appmarket.v1", func() execution.Service { return &appMarketService{} })
	m.RegisterFactory("external-link.v1", func() execution.Service { return &externalLinkService{} })
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
			"notes":   "Use Assembler market APIs to publish/subscribe/install apps",
		}, nil
	default:
		return nil, errors.New("unsupported method")
	}
}
func (s *appMarketService) RequiredCapability(string) string { return "read" }

type externalLinkService struct{}

func (s *externalLinkService) ID() string                    { return "external-link" }
func (s *externalLinkService) Start(_ context.Context) error { return nil }
func (s *externalLinkService) Stop(_ context.Context) error  { return nil }
func (s *externalLinkService) Health(_ context.Context) (map[string]any, error) {
	return map[string]any{"service": "external-link", "status": "ok"}, nil
}
func (s *externalLinkService) Invoke(_ context.Context, method string, _ map[string]any) (any, error) {
	if method == "about" {
		return map[string]any{"type": "external-link", "notes": "open via manifest.launch_url"}, nil
	}
	return nil, errors.New("unsupported method")
}
func (s *externalLinkService) RequiredCapability(string) string { return "read" }
