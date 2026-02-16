package clawdcity

import (
	"context"
	"testing"

	"ClawdCity/internal/clawdcity/execution"
	"ClawdCity/internal/core/network"
)

func TestCityDefaultFlow(t *testing.T) {
	city, err := New(network.NewMemoryPubSub())
	if err != nil {
		t.Fatalf("new city: %v", err)
	}
	apps := city.ListMarket("", "")
	if len(apps) < 3 {
		t.Fatalf("expected default market apps, got %d", len(apps))
	}
	if err := city.InstallFromMarket("echo-demo", execution.SandboxPolicy{}); err != nil {
		t.Fatalf("install echo: %v", err)
	}
	if err := city.Start(context.Background(), "echo-demo"); err != nil {
		t.Fatalf("start echo: %v", err)
	}
	out, err := city.Invoke(context.Background(), "echo-demo", "ping", map[string]any{"hello": "world"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type: %T", out)
	}
	if _, hasPong := res["pong"]; !hasPong {
		t.Fatalf("missing pong in response: %+v", res)
	}
}
