package clawdcity

import (
	"context"
	"errors"

	"ClawdCity/internal/clawdcity/control"
	"ClawdCity/internal/clawdcity/execution"
	"ClawdCity/internal/clawdcity/market"
	"ClawdCity/internal/core/network"
)

var ErrMarketAppNotFound = errors.New("app not found in market")

type City struct {
	control *control.Manager
	market  *market.Manager
}

func New(pubsub network.PubSub) (*City, error) {
	ctrl := control.NewManager()
	control.RegisterBuiltinFactories(ctrl)
	mkt := market.NewManager(pubsub)
	c := &City{control: ctrl, market: mkt}
	if err := c.seedDefaults(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *City) seedDefaults() error {
	defaults := []market.Listing{
		{
			AppID:     "appmarket",
			Publisher: "clawdcity",
			Manifest: control.Manifest{
				AppID:       "appmarket",
				Name:        "AppMarket",
				Version:     "0.1.0",
				Kind:        "service",
				Description: "Default app catalog and subscription entrypoint",
				Tags:        []string{"market", "default"},
				Factory:     "appmarket.v1",
			},
			DefaultPolicy: execution.SandboxPolicy{Capabilities: []string{"read", "invoke"}, MaxCallsPerMinute: 240, MaxPayloadBytes: 8192},
		},
		{
			AppID:     "echo-demo",
			Publisher: "clawdcity",
			Manifest: control.Manifest{
				AppID:       "echo-demo",
				Name:        "Echo Demo",
				Version:     "0.1.0",
				Kind:        "service",
				Description: "Debug utility service for invoke/health checks",
				Tags:        []string{"utility", "demo"},
				Factory:     "echo.v1",
			},
			DefaultPolicy: execution.SandboxPolicy{Capabilities: []string{"invoke"}, MaxCallsPerMinute: 300, MaxPayloadBytes: 8192},
		},
		{
			AppID:     "counter-game",
			Publisher: "clawdcity",
			Manifest: control.Manifest{
				AppID:       "counter-game",
				Name:        "Counter Game",
				Version:     "0.1.0",
				Kind:        "game",
				Description: "Minimal multi-player counter game for pipeline testing",
				Tags:        []string{"game", "demo"},
				Factory:     "counter.v1",
			},
			DefaultPolicy: execution.SandboxPolicy{Capabilities: []string{"read", "write"}, MaxCallsPerMinute: 240, MaxPayloadBytes: 8192},
		},
	}
	for _, app := range defaults {
		if _, err := c.market.Publish(app); err != nil {
			return err
		}
	}
	if err := c.InstallFromMarket("appmarket", execution.SandboxPolicy{}); err != nil {
		return err
	}
	return c.Start(context.Background(), "appmarket")
}

func (c *City) Publish(listing market.Listing) (market.Listing, error) {
	return c.market.Publish(listing)
}

func (c *City) ListMarket(kind, tag string) []market.Listing {
	return c.market.List(kind, tag)
}

func (c *City) SubscribeMarket() (<-chan network.Message, func(), error) {
	return c.market.Subscribe()
}

func (c *City) InstallFromMarket(appID string, policy execution.SandboxPolicy) error {
	listing, ok := c.market.Get(appID)
	if !ok {
		return ErrMarketAppNotFound
	}
	if len(policy.Capabilities) == 0 && policy.MaxCallsPerMinute == 0 && policy.MaxPayloadBytes == 0 {
		policy = listing.DefaultPolicy
	}
	return c.control.Install(listing.Manifest, policy)
}

func (c *City) Start(ctx context.Context, appID string) error { return c.control.Start(ctx, appID) }
func (c *City) Stop(ctx context.Context, appID string) error  { return c.control.Stop(ctx, appID) }

func (c *City) Invoke(ctx context.Context, appID string, method string, params map[string]any) (any, error) {
	return c.control.Invoke(ctx, appID, method, params)
}

func (c *City) Health(ctx context.Context, appID string) (map[string]any, error) {
	return c.control.Health(ctx, appID)
}

func (c *City) ListInstalled() []control.InstalledInfo {
	return c.control.ListInstalled()
}
