package control

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"ClawdCity/internal/assembler/execution"
)

var (
	ErrFactoryNotFound   = errors.New("service factory not found")
	ErrInstanceNotFound  = errors.New("instance not found")
	ErrAlreadyInstalled  = errors.New("app already installed")
	ErrManifestAppIDZero = errors.New("app id required")
)

type Manifest struct {
	AppID       string   `json:"app_id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Factory     string   `json:"factory"`
	LaunchURL   string   `json:"launch_url,omitempty"`
}

type InstalledInfo struct {
	Manifest Manifest                `json:"manifest"`
	Running  bool                    `json:"running"`
	Policy   execution.SandboxPolicy `json:"policy"`
}

type ServiceFactory func() execution.Service

type instance struct {
	manifest Manifest
	runner   *execution.SandboxedService
	running  bool
}

type Manager struct {
	mu        sync.RWMutex
	factories map[string]ServiceFactory
	apps      map[string]*instance
}

func NewManager() *Manager {
	return &Manager{
		factories: make(map[string]ServiceFactory),
		apps:      make(map[string]*instance),
	}
}

func (m *Manager) RegisterFactory(name string, fn ServiceFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[name] = fn
}

func (m *Manager) Install(manifest Manifest, policy execution.SandboxPolicy) error {
	if manifest.AppID == "" {
		return ErrManifestAppIDZero
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.apps[manifest.AppID]; ok {
		return ErrAlreadyInstalled
	}
	factory, ok := m.factories[manifest.Factory]
	if !ok {
		return fmt.Errorf("%w: %s", ErrFactoryNotFound, manifest.Factory)
	}
	runner := execution.NewSandboxedService(factory(), policy)
	m.apps[manifest.AppID] = &instance{manifest: manifest, runner: runner, running: false}
	return nil
}

func (m *Manager) Start(ctx context.Context, appID string) error {
	inst, err := m.get(appID)
	if err != nil {
		return err
	}
	if err := inst.runner.Start(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	inst.running = true
	m.mu.Unlock()
	return nil
}

func (m *Manager) Stop(ctx context.Context, appID string) error {
	inst, err := m.get(appID)
	if err != nil {
		return err
	}
	if err := inst.runner.Stop(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	inst.running = false
	m.mu.Unlock()
	return nil
}

func (m *Manager) Invoke(ctx context.Context, appID, method string, params map[string]any) (any, error) {
	inst, err := m.get(appID)
	if err != nil {
		return nil, err
	}
	return inst.runner.Invoke(ctx, method, params)
}

func (m *Manager) Health(ctx context.Context, appID string) (map[string]any, error) {
	inst, err := m.get(appID)
	if err != nil {
		return nil, err
	}
	h, err := inst.runner.Health(ctx)
	if err != nil {
		return nil, err
	}
	h["app_id"] = appID
	h["manifest"] = inst.manifest
	h["running"] = inst.running
	return h, nil
}

func (m *Manager) ListInstalled() []InstalledInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]InstalledInfo, 0, len(m.apps))
	for _, inst := range m.apps {
		out = append(out, InstalledInfo{
			Manifest: inst.manifest,
			Running:  inst.running,
			Policy:   inst.runner.Policy(),
		})
	}
	return out
}

func (m *Manager) get(appID string) (*instance, error) {
	m.mu.RLock()
	inst, ok := m.apps[appID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrInstanceNotFound
	}
	return inst, nil
}
