package tetrisroom

import (
	"testing"

	"ClawdCity/internal/core/network"
)

func TestMatchAndControlSwitch(t *testing.T) {
	m := NewManager(network.NewMemoryPubSub())
	if _, err := m.RegisterPlayer("alice", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := m.RegisterPlayer("bob", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register bob: %v", err)
	}
	if _, err := m.SetReady("alice", 60); err != nil {
		t.Fatalf("alice ready: %v", err)
	}
	room, err := m.SetReady("bob", 30)
	if err != nil {
		t.Fatalf("bob ready: %v", err)
	}
	if room == nil {
		t.Fatal("expected room assigned")
	}
	if room.HostID != "bob" {
		t.Fatalf("expected lower ping player bob as host, got %s", room.HostID)
	}

	updated, err := m.ToggleControl(room.ID, "alice", ControlAgent, "agent-openclaw-1")
	if err != nil {
		t.Fatalf("toggle control: %v", err)
	}
	if updated.ControlMode != ControlAgent {
		t.Fatalf("expected agent mode, got %s", updated.ControlMode)
	}

	err = m.SubmitInput(room.ID, InputEvent{PlayerID: "alice", Source: SourceAgent, Action: "move_left"})
	if err != nil {
		t.Fatalf("agent input should pass: %v", err)
	}
	err = m.SubmitInput(room.ID, InputEvent{PlayerID: "alice", Source: SourceHuman, Action: "move_left"})
	if err == nil {
		t.Fatal("human input should be denied when agent mode is active")
	}
}
