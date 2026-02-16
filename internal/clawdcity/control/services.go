package control

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"ClawdCity/internal/clawdcity/execution"
)

func RegisterBuiltinFactories(m *Manager) {
	m.RegisterFactory("echo.v1", func() execution.Service { return &echoService{} })
	m.RegisterFactory("counter.v1", func() execution.Service { return newCounterService() })
	m.RegisterFactory("appmarket.v1", func() execution.Service { return &appMarketService{} })
}

type echoService struct{}

func (s *echoService) ID() string                    { return "echo" }
func (s *echoService) Start(_ context.Context) error { return nil }
func (s *echoService) Stop(_ context.Context) error  { return nil }
func (s *echoService) Health(_ context.Context) (map[string]any, error) {
	return map[string]any{"service": "echo", "status": "ok"}, nil
}
func (s *echoService) Invoke(_ context.Context, method string, params map[string]any) (any, error) {
	switch method {
	case "ping":
		return map[string]any{"pong": true, "params": params, "at": time.Now().UTC()}, nil
	case "echo":
		return map[string]any{"echo": params}, nil
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}
func (s *echoService) RequiredCapability(method string) string {
	switch method {
	case "ping", "echo":
		return "invoke"
	default:
		return "invoke"
	}
}

type counterService struct {
	mu   sync.Mutex
	data map[string]int64
}

func newCounterService() *counterService {
	return &counterService{data: map[string]int64{}}
}

func (s *counterService) ID() string                    { return "counter" }
func (s *counterService) Start(_ context.Context) error { return nil }
func (s *counterService) Stop(_ context.Context) error  { return nil }
func (s *counterService) Health(_ context.Context) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"service": "counter", "players": len(s.data)}, nil
}

func (s *counterService) Invoke(_ context.Context, method string, params map[string]any) (any, error) {
	player, _ := params["player"].(string)
	if player == "" {
		player = "guest"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch method {
	case "inc":
		s.data[player]++
		return map[string]any{"player": player, "value": s.data[player]}, nil
	case "get":
		return map[string]any{"player": player, "value": s.data[player]}, nil
	case "reset":
		s.data[player] = 0
		return map[string]any{"player": player, "value": 0}, nil
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

func (s *counterService) RequiredCapability(method string) string {
	switch method {
	case "get":
		return "read"
	case "inc", "reset":
		return "write"
	default:
		return "invoke"
	}
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
