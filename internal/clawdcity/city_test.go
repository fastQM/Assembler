package clawdcity

import (
	"testing"

	"ClawdCity/internal/core/network"
)

func TestCityDefaultFlow(t *testing.T) {
	city, err := New(network.NewMemoryPubSub())
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	apps := city.ListMarket("", "")
	if len(apps) < 1 {
		t.Fatalf("expected default market apps, got %d", len(apps))
	}
	if len(apps) != 1 || apps[0].AppID != "social-web" {
		t.Fatalf("expected only social-web in market, got %+v", apps)
	}
}
