package runtime

import (
	"testing"

	"ClawdCity/internal/core/network"
)

type fakeAdapter struct{}

func (f *fakeAdapter) ID() string                                    { return "fake" }
func (f *fakeAdapter) Init(params map[string]any) (any, error)       { return map[string]int{"n": 0}, nil }
func (f *fakeAdapter) ValidateAction(state any, action Action) error { return nil }
func (f *fakeAdapter) ApplyAction(state any, action Action) (any, []Event, error) {
	return state, []Event{{Type: "ok", PlayerID: action.PlayerID}}, nil
}
func (f *fakeAdapter) View(state any, playerID string) (any, error) { return state, nil }

func TestEngineCreateAndSubmit(t *testing.T) {
	e := NewEngine(network.NewMemoryPubSub())
	e.RegisterAdapter(&fakeAdapter{})

	sid, err := e.CreateSession("fake", nil)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	events, err := e.SubmitAction(sid, Action{PlayerID: "p1", Type: "noop"})
	if err != nil {
		t.Fatalf("submit action: %v", err)
	}
	if len(events) != 1 || events[0].Type != "ok" {
		t.Fatalf("unexpected events: %+v", events)
	}

	sessions := e.ListSessions()
	if len(sessions) != 1 || sessions[0].GameID != "fake" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
}
