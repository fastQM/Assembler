package poker

import (
	"testing"

	"ClawdCity/internal/runtime"
)

func TestPokerCommitRevealAndFoldSettle(t *testing.T) {
	a := NewAdapter()
	stateAny, err := a.Init(map[string]any{"small_blind": 10.0, "big_blind": 20.0, "max_players": 6.0})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	state := stateAny

	mustApply := func(act runtime.Action) {
		t.Helper()
		if err := a.ValidateAction(state, act); err != nil {
			t.Fatalf("validate %s: %v", act.Type, err)
		}
		next, _, err := a.ApplyAction(state, act)
		if err != nil {
			t.Fatalf("apply %s: %v", act.Type, err)
		}
		state = next
	}

	mustApply(runtime.Action{PlayerID: "alice", Type: "join", Amount: 1000})
	mustApply(runtime.Action{PlayerID: "bob", Type: "join", Amount: 1000})
	mustApply(runtime.Action{PlayerID: "alice", Type: "start_hand"})

	s := state.(*State)
	if s.Phase != PhaseCommit {
		t.Fatalf("expected commit phase, got %s", s.Phase)
	}

	aliceSeed := "alice-seed"
	bobSeed := "bob-seed"
	mustApply(runtime.Action{PlayerID: "alice", Type: "commit", Data: map[string]any{"hash": hashString(aliceSeed)}})
	mustApply(runtime.Action{PlayerID: "bob", Type: "commit", Data: map[string]any{"hash": hashString(bobSeed)}})
	mustApply(runtime.Action{PlayerID: "alice", Type: "reveal", Data: map[string]any{"seed": aliceSeed}})
	mustApply(runtime.Action{PlayerID: "bob", Type: "reveal", Data: map[string]any{"seed": bobSeed}})

	s = state.(*State)
	if s.Phase != PhasePreFlop {
		t.Fatalf("expected preflop after reveal, got %s", s.Phase)
	}
	if len(s.HoleCards["alice"]) != 2 || len(s.HoleCards["bob"]) != 2 {
		t.Fatalf("expected hole cards dealt")
	}

	turn := s.Players[s.TurnPos]
	mustApply(runtime.Action{PlayerID: turn, Type: "fold"})

	s = state.(*State)
	if s.Phase != PhaseSettled {
		t.Fatalf("expected settled after fold to single winner, got %s", s.Phase)
	}
}
