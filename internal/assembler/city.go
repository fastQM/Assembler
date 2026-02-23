package assembler

import (
	"context"
	"errors"

	"ClawdCity/internal/assembler/control"
	"ClawdCity/internal/assembler/execution"
	"ClawdCity/internal/assembler/market"
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
			AppID:     "social-web",
			Publisher: "assembler-apps",
			Manifest: control.Manifest{
				AppID:       "social-web",
				Name:        "ClawdCity Social",
				Version:     "0.1.0",
				Kind:        "social",
				Description: "P2P social app with profile setup, discovery, encrypted friend requests and direct messaging",
				Tags:        []string{"social", "p2p", "chat"},
				Factory:     "external-link.v1",
				LaunchURL:   "http://{hostname}:8090/apps/social-web/web/index.html",
			},
			DefaultPolicy: execution.SandboxPolicy{Capabilities: []string{"read"}, MaxCallsPerMinute: 180, MaxPayloadBytes: 16384},
		},
	}
	for _, app := range defaults {
		if _, err := c.market.Publish(app); err != nil {
			return err
		}
	}
	return nil
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
