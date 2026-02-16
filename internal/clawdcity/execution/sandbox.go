package execution

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var (
	ErrCapabilityDenied = errors.New("capability denied")
	ErrRateLimited      = errors.New("sandbox rate limit exceeded")
	ErrPayloadTooLarge  = errors.New("sandbox payload too large")
	ErrNotRunning       = errors.New("service not running")
)

// Service is a sandboxed unit that can be managed by the control layer.
type Service interface {
	ID() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) (map[string]any, error)
	Invoke(ctx context.Context, method string, params map[string]any) (any, error)
	RequiredCapability(method string) string
}

// SandboxPolicy defines security and resource limits for a service instance.
type SandboxPolicy struct {
	Capabilities      []string `json:"capabilities"`
	MaxCallsPerMinute int      `json:"max_calls_per_minute"`
	MaxPayloadBytes   int      `json:"max_payload_bytes"`
}

// SandboxedService enforces capability and quota policies before delegating to the service.
type SandboxedService struct {
	service Service
	policy  SandboxPolicy

	mu         sync.Mutex
	running    bool
	callWindow []time.Time
	caps       map[string]bool
}

func NewSandboxedService(service Service, policy SandboxPolicy) *SandboxedService {
	if policy.MaxCallsPerMinute <= 0 {
		policy.MaxCallsPerMinute = 120
	}
	if policy.MaxPayloadBytes <= 0 {
		policy.MaxPayloadBytes = 8192
	}
	caps := make(map[string]bool, len(policy.Capabilities))
	for _, c := range policy.Capabilities {
		caps[c] = true
	}
	return &SandboxedService{
		service:    service,
		policy:     policy,
		callWindow: make([]time.Time, 0, policy.MaxCallsPerMinute),
		caps:       caps,
	}
}

func (s *SandboxedService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	if err := s.service.Start(ctx); err != nil {
		return err
	}
	s.running = true
	return nil
}

func (s *SandboxedService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	if err := s.service.Stop(ctx); err != nil {
		return err
	}
	s.running = false
	s.callWindow = s.callWindow[:0]
	return nil
}

func (s *SandboxedService) Health(ctx context.Context) (map[string]any, error) {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()

	serviceHealth, err := s.service.Health(ctx)
	if err != nil {
		return nil, err
	}
	serviceHealth["sandbox_running"] = running
	serviceHealth["sandbox_policy"] = s.policy
	return serviceHealth, nil
}

func (s *SandboxedService) Invoke(ctx context.Context, method string, params map[string]any) (any, error) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil, ErrNotRunning
	}
	if required := s.service.RequiredCapability(method); required != "" {
		if !s.caps[required] {
			s.mu.Unlock()
			return nil, ErrCapabilityDenied
		}
	}
	if !s.allowCallLocked() {
		s.mu.Unlock()
		return nil, ErrRateLimited
	}
	s.mu.Unlock()

	payload, _ := json.Marshal(params)
	if len(payload) > s.policy.MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}
	return s.service.Invoke(ctx, method, params)
}

func (s *SandboxedService) allowCallLocked() bool {
	now := time.Now().UTC()
	cut := now.Add(-time.Minute)
	j := 0
	for _, t := range s.callWindow {
		if t.After(cut) {
			s.callWindow[j] = t
			j++
		}
	}
	s.callWindow = s.callWindow[:j]
	if len(s.callWindow) >= s.policy.MaxCallsPerMinute {
		return false
	}
	s.callWindow = append(s.callWindow, now)
	return true
}

func (s *SandboxedService) Policy() SandboxPolicy {
	return s.policy
}
