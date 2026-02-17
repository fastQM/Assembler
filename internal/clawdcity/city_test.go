package clawdcity

import (
	"context"
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
	out, err := city.Invoke(context.Background(), "appmarket", "about", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type: %T", out)
	}
	if _, hasName := res["name"]; !hasName {
		t.Fatalf("missing name in response: %+v", res)
	}
}
