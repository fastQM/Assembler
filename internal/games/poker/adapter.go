package poker

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"ClawdCity/internal/runtime"
)

const (
	PhaseWaiting  = "waiting"
	PhaseCommit   = "commit"
	PhaseReveal   = "reveal"
	PhasePreFlop  = "preflop"
	PhaseFlop     = "flop"
	PhaseTurn     = "turn"
	PhaseRiver    = "river"
	PhaseShowdown = "showdown"
	PhaseSettled  = "settled"
)

type Card struct {
	Rank int    `json:"rank"`
	Suit string `json:"suit"`
}

type State struct {
	TableID        string            `json:"table_id"`
	SmallBlind     int64             `json:"small_blind"`
	BigBlind       int64             `json:"big_blind"`
	MaxPlayers     int               `json:"max_players"`
	Phase          string            `json:"phase"`
	HandID         int64             `json:"hand_id"`
	Players        []string          `json:"players"`
	Stacks         map[string]int64  `json:"stacks"`
	InHand         map[string]bool   `json:"in_hand"`
	Folded         map[string]bool   `json:"folded"`
	Bets           map[string]int64  `json:"bets"`
	Committed      map[string]string `json:"committed"`
	Revealed       map[string]string `json:"revealed"`
	HoleCards      map[string][]Card `json:"hole_cards"`
	Board          []Card            `json:"board"`
	Pot            int64             `json:"pot"`
	CurrentBet     int64             `json:"current_bet"`
	DealerPos      int               `json:"dealer_pos"`
	TurnPos        int               `json:"turn_pos"`
	RoundActed     map[string]bool   `json:"round_acted"`
	LastActionAtMs int64             `json:"last_action_at_ms"`
	Deck           []Card            `json:"-"`
}

type Adapter struct{}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) ID() string { return "poker" }

func (a *Adapter) Init(params map[string]any) (any, error) {
	sb := int64FromParams(params, "small_blind", 10)
	bb := int64FromParams(params, "big_blind", 20)
	maxPlayers := int(int64FromParams(params, "max_players", 6))
	if sb <= 0 || bb <= 0 || sb >= bb {
		return nil, errors.New("invalid blinds")
	}
	if maxPlayers < 2 || maxPlayers > 10 {
		return nil, errors.New("invalid max players")
	}
	return &State{
		TableID:    "",
		SmallBlind: sb,
		BigBlind:   bb,
		MaxPlayers: maxPlayers,
		Phase:      PhaseWaiting,
		DealerPos:  -1,
		Stacks:     map[string]int64{},
		InHand:     map[string]bool{},
		Folded:     map[string]bool{},
		Bets:       map[string]int64{},
		Committed:  map[string]string{},
		Revealed:   map[string]string{},
		HoleCards:  map[string][]Card{},
		RoundActed: map[string]bool{},
	}, nil
}

func (a *Adapter) ValidateAction(state any, action runtime.Action) error {
	s := state.(*State)
	if action.PlayerID == "" {
		return errors.New("missing player_id")
	}
	switch action.Type {
	case "join":
		if s.Phase != PhaseWaiting && s.Phase != PhaseSettled {
			return errors.New("join allowed only in waiting/settled")
		}
		if len(s.Players) >= s.MaxPlayers {
			return errors.New("table full")
		}
		if action.Amount <= 0 {
			return errors.New("buy in must be positive")
		}
		if _, ok := s.Stacks[action.PlayerID]; ok {
			return errors.New("player already seated")
		}
	case "start_hand":
		if s.Phase != PhaseWaiting && s.Phase != PhaseSettled {
			return errors.New("hand already active")
		}
		if len(s.Players) < 2 {
			return errors.New("need at least 2 players")
		}
	case "commit":
		if s.Phase != PhaseCommit {
			return errors.New("not in commit phase")
		}
		if _, ok := s.InHand[action.PlayerID]; !ok {
			return errors.New("player not in hand")
		}
		if s.Committed[action.PlayerID] != "" {
			return errors.New("commit already submitted")
		}
		h := stringData(action.Data, "hash")
		if h == "" {
			return errors.New("missing commit hash")
		}
	case "reveal":
		if s.Phase != PhaseReveal {
			return errors.New("not in reveal phase")
		}
		if _, ok := s.InHand[action.PlayerID]; !ok {
			return errors.New("player not in hand")
		}
		if s.Revealed[action.PlayerID] != "" {
			return errors.New("reveal already submitted")
		}
		seed := stringData(action.Data, "seed")
		if seed == "" {
			return errors.New("missing reveal seed")
		}
	case "fold", "check", "call", "raise":
		if !isBettingPhase(s.Phase) {
			return errors.New("not in betting phase")
		}
		if !s.InHand[action.PlayerID] || s.Folded[action.PlayerID] {
			return errors.New("player not active")
		}
		if s.Players[s.TurnPos] != action.PlayerID {
			return errors.New("not player turn")
		}
		if action.Type == "check" && s.Bets[action.PlayerID] != s.CurrentBet {
			return errors.New("cannot check when behind current bet")
		}
		if action.Type == "call" && s.Bets[action.PlayerID] >= s.CurrentBet {
			return errors.New("nothing to call")
		}
		if action.Type == "raise" {
			if action.Amount <= 0 {
				return errors.New("raise amount must be positive")
			}
			if action.Amount < s.BigBlind {
				return errors.New("raise must be at least big blind")
			}
		}
	default:
		return fmt.Errorf("unsupported action: %s", action.Type)
	}
	return nil
}

func (a *Adapter) ApplyAction(state any, action runtime.Action) (any, []runtime.Event, error) {
	s := cloneState(state.(*State))
	now := action.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.LastActionAtMs = now.UnixMilli()
	events := make([]runtime.Event, 0, 4)

	switch action.Type {
	case "join":
		s.Players = append(s.Players, action.PlayerID)
		s.Stacks[action.PlayerID] = action.Amount
		events = append(events, evt("player_joined", action.PlayerID, map[string]any{"buy_in": action.Amount}, now))
	case "start_hand":
		prepareNewHand(s)
		events = append(events, evt("hand_started", action.PlayerID, map[string]any{"hand_id": s.HandID}, now))
	case "commit":
		h := stringData(action.Data, "hash")
		s.Committed[action.PlayerID] = h
		events = append(events, evt("commit_received", action.PlayerID, map[string]any{"hash": h}, now))
		if len(s.Committed) == activeCount(s) {
			s.Phase = PhaseReveal
			events = append(events, evt("phase_changed", "", map[string]any{"phase": s.Phase}, now))
		}
	case "reveal":
		seed := stringData(action.Data, "seed")
		commit := s.Committed[action.PlayerID]
		if commit != hashString(seed) {
			return nil, nil, errors.New("reveal does not match commit")
		}
		s.Revealed[action.PlayerID] = seed
		events = append(events, evt("reveal_received", action.PlayerID, nil, now))
		if len(s.Revealed) == activeCount(s) {
			dealFromReveals(s)
			events = append(events,
				evt("phase_changed", "", map[string]any{"phase": s.Phase}, now),
				evt("cards_dealt", "", map[string]any{"players": activeCount(s)}, now),
			)
		}
	case "fold":
		s.Folded[action.PlayerID] = true
		s.RoundActed[action.PlayerID] = true
		events = append(events, evt("player_folded", action.PlayerID, nil, now))
		if winner, done := remainingWinner(s); done {
			pot := s.Pot
			settleSingleWinner(s, winner)
			events = append(events, evt("hand_settled", winner, map[string]any{"pot": pot}, now))
			break
		}
		advanceTurn(s)
		events = append(events, evt("turn_changed", s.Players[s.TurnPos], nil, now))
		if roundComplete(s) {
			events = append(events, advanceRound(s, now)...)
		}
	case "check":
		s.RoundActed[action.PlayerID] = true
		events = append(events, evt("player_checked", action.PlayerID, nil, now))
		advanceTurn(s)
		events = append(events, evt("turn_changed", s.Players[s.TurnPos], nil, now))
		if roundComplete(s) {
			events = append(events, advanceRound(s, now)...)
		}
	case "call":
		need := s.CurrentBet - s.Bets[action.PlayerID]
		if need > s.Stacks[action.PlayerID] {
			need = s.Stacks[action.PlayerID]
		}
		s.Stacks[action.PlayerID] -= need
		s.Bets[action.PlayerID] += need
		s.Pot += need
		s.RoundActed[action.PlayerID] = true
		events = append(events, evt("player_called", action.PlayerID, map[string]any{"amount": need}, now))
		advanceTurn(s)
		events = append(events, evt("turn_changed", s.Players[s.TurnPos], nil, now))
		if roundComplete(s) {
			events = append(events, advanceRound(s, now)...)
		}
	case "raise":
		need := s.CurrentBet - s.Bets[action.PlayerID]
		total := need + action.Amount
		if total > s.Stacks[action.PlayerID] {
			return nil, nil, errors.New("insufficient stack for raise")
		}
		s.Stacks[action.PlayerID] -= total
		s.Bets[action.PlayerID] += total
		s.Pot += total
		s.CurrentBet = s.Bets[action.PlayerID]
		for p := range s.RoundActed {
			if s.InHand[p] && !s.Folded[p] {
				s.RoundActed[p] = false
			}
		}
		s.RoundActed[action.PlayerID] = true
		events = append(events, evt("player_raised", action.PlayerID, map[string]any{"amount": action.Amount}, now))
		advanceTurn(s)
		events = append(events, evt("turn_changed", s.Players[s.TurnPos], nil, now))
		if roundComplete(s) {
			events = append(events, advanceRound(s, now)...)
		}
	}

	return s, events, nil
}

func (a *Adapter) View(state any, playerID string) (any, error) {
	s := state.(*State)
	view := map[string]any{
		"phase":       s.Phase,
		"hand_id":     s.HandID,
		"players":     append([]string(nil), s.Players...),
		"stacks":      s.Stacks,
		"bets":        s.Bets,
		"board":       s.Board,
		"pot":         s.Pot,
		"current_bet": s.CurrentBet,
	}
	if s.TurnPos >= 0 && s.TurnPos < len(s.Players) {
		view["turn_player"] = s.Players[s.TurnPos]
	}
	if cards, ok := s.HoleCards[playerID]; ok {
		view["hole_cards"] = cards
	}
	return view, nil
}

func prepareNewHand(s *State) {
	s.HandID++
	if s.DealerPos < 0 {
		s.DealerPos = 0
	} else {
		s.DealerPos = (s.DealerPos + 1) % len(s.Players)
	}
	s.Phase = PhaseCommit
	s.InHand = map[string]bool{}
	s.Folded = map[string]bool{}
	s.Bets = map[string]int64{}
	s.Committed = map[string]string{}
	s.Revealed = map[string]string{}
	s.HoleCards = map[string][]Card{}
	s.Board = nil
	s.Pot = 0
	s.CurrentBet = 0
	s.RoundActed = map[string]bool{}

	for _, p := range s.Players {
		if s.Stacks[p] > 0 {
			s.InHand[p] = true
			s.Folded[p] = false
			s.Bets[p] = 0
			s.RoundActed[p] = false
		}
	}
}

func dealFromReveals(s *State) {
	s.Phase = PhasePreFlop
	s.Deck = fullDeck()
	seed := seedFromReveals(s)
	r := rand.New(rand.NewSource(seed))
	shuffleDeck(r, s.Deck)

	idx := 0
	order := activePlayersInSeatOrder(s)
	for _, p := range order {
		s.HoleCards[p] = []Card{s.Deck[idx], s.Deck[idx+1]}
		idx += 2
	}
	s.Deck = s.Deck[idx:]

	sbPos := nextActivePos(s, s.DealerPos)
	bbPos := nextActivePos(s, sbPos)
	postBlind(s, s.Players[sbPos], s.SmallBlind)
	postBlind(s, s.Players[bbPos], s.BigBlind)
	s.CurrentBet = s.BigBlind
	s.TurnPos = nextActivePos(s, bbPos)
	for _, p := range order {
		s.RoundActed[p] = false
	}
}

func postBlind(s *State, p string, blind int64) {
	amount := blind
	if amount > s.Stacks[p] {
		amount = s.Stacks[p]
	}
	s.Stacks[p] -= amount
	s.Bets[p] += amount
	s.Pot += amount
}

func advanceTurn(s *State) {
	s.TurnPos = nextActivePos(s, s.TurnPos)
}

func advanceRound(s *State, now time.Time) []runtime.Event {
	events := make([]runtime.Event, 0, 4)
	if winner, done := remainingWinner(s); done {
		pot := s.Pot
		settleSingleWinner(s, winner)
		events = append(events, evt("hand_settled", winner, map[string]any{"pot": pot}, now))
		return events
	}

	clearRoundBets(s)
	for p := range s.RoundActed {
		if s.InHand[p] && !s.Folded[p] {
			s.RoundActed[p] = false
		}
	}
	s.TurnPos = nextActivePos(s, s.DealerPos)

	switch s.Phase {
	case PhasePreFlop:
		s.Phase = PhaseFlop
		s.Board = append(s.Board, s.Deck[0], s.Deck[1], s.Deck[2])
		s.Deck = s.Deck[3:]
		events = append(events, evt("board_dealt", "", map[string]any{"phase": s.Phase, "cards": s.Board}, now))
	case PhaseFlop:
		s.Phase = PhaseTurn
		s.Board = append(s.Board, s.Deck[0])
		s.Deck = s.Deck[1:]
		events = append(events, evt("board_dealt", "", map[string]any{"phase": s.Phase, "cards": s.Board}, now))
	case PhaseTurn:
		s.Phase = PhaseRiver
		s.Board = append(s.Board, s.Deck[0])
		s.Deck = s.Deck[1:]
		events = append(events, evt("board_dealt", "", map[string]any{"phase": s.Phase, "cards": s.Board}, now))
	case PhaseRiver:
		s.Phase = PhaseShowdown
		winner := showdownWinner(s)
		pot := s.Pot
		settleSingleWinner(s, winner)
		events = append(events, evt("showdown", winner, map[string]any{"pot": pot}, now))
	}
	events = append(events, evt("phase_changed", "", map[string]any{"phase": s.Phase}, now))
	return events
}

func roundComplete(s *State) bool {
	active := activePlayersInSeatOrder(s)
	if len(active) <= 1 {
		return true
	}
	for _, p := range active {
		if !s.RoundActed[p] {
			return false
		}
		if s.Bets[p] != s.CurrentBet {
			return false
		}
	}
	return true
}

func clearRoundBets(s *State) {
	s.CurrentBet = 0
	for p := range s.Bets {
		s.Bets[p] = 0
	}
}

func remainingWinner(s *State) (string, bool) {
	remaining := ""
	count := 0
	for _, p := range s.Players {
		if s.InHand[p] && !s.Folded[p] {
			remaining = p
			count++
		}
	}
	return remaining, count == 1
}

func settleSingleWinner(s *State, winner string) {
	s.Stacks[winner] += s.Pot
	s.Pot = 0
	s.Phase = PhaseSettled
}

func showdownWinner(s *State) string {
	best := ""
	bestScore := -1
	for _, p := range activePlayersInSeatOrder(s) {
		cards := append([]Card{}, s.HoleCards[p]...)
		cards = append(cards, s.Board...)
		score := score7(cards)
		if score > bestScore {
			bestScore = score
			best = p
		}
	}
	if best == "" {
		return activePlayersInSeatOrder(s)[0]
	}
	return best
}

func score7(cards []Card) int {
	// Lightweight evaluator for MVP: category (pairs/trips/quads) + kicker ranks.
	rankCounts := map[int]int{}
	for _, c := range cards {
		rankCounts[c.Rank]++
	}
	ranks := make([]int, 0, len(rankCounts))
	for r := range rankCounts {
		ranks = append(ranks, r)
	}
	sort.Slice(ranks, func(i, j int) bool {
		ci, cj := rankCounts[ranks[i]], rankCounts[ranks[j]]
		if ci == cj {
			return ranks[i] > ranks[j]
		}
		return ci > cj
	})

	score := 0
	mult := 1_000_000
	for _, r := range ranks {
		score += rankCounts[r] * mult
		score += r * (mult / 10)
		mult /= 10
		if mult <= 0 {
			break
		}
	}
	return score
}

func activePlayersInSeatOrder(s *State) []string {
	out := make([]string, 0, len(s.Players))
	for _, p := range s.Players {
		if s.InHand[p] && !s.Folded[p] {
			out = append(out, p)
		}
	}
	return out
}

func nextActivePos(s *State, from int) int {
	if len(s.Players) == 0 {
		return 0
	}
	for i := 1; i <= len(s.Players); i++ {
		idx := (from + i) % len(s.Players)
		p := s.Players[idx]
		if s.InHand[p] && !s.Folded[p] {
			return idx
		}
	}
	return from
}

func seedFromReveals(s *State) int64 {
	players := activePlayersInSeatOrder(s)
	sort.Strings(players)
	buf := strings.Builder{}
	for _, p := range players {
		buf.WriteString(s.Revealed[p])
		buf.WriteString("|")
	}
	buf.WriteString(fmt.Sprintf("%d", s.HandID))
	h := sha256.Sum256([]byte(buf.String()))
	return int64(binary.BigEndian.Uint64(h[:8]))
}

func fullDeck() []Card {
	suits := []string{"S", "H", "D", "C"}
	deck := make([]Card, 0, 52)
	for _, suit := range suits {
		for rank := 2; rank <= 14; rank++ {
			deck = append(deck, Card{Rank: rank, Suit: suit})
		}
	}
	return deck
}

func shuffleDeck(r *rand.Rand, deck []Card) {
	for i := len(deck) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
}

func activeCount(s *State) int {
	count := 0
	for _, in := range s.InHand {
		if in {
			count++
		}
	}
	return count
}

func cloneState(src *State) *State {
	dst := *src
	dst.Players = append([]string(nil), src.Players...)
	dst.Stacks = copyMapInt64(src.Stacks)
	dst.InHand = copyMapBool(src.InHand)
	dst.Folded = copyMapBool(src.Folded)
	dst.Bets = copyMapInt64(src.Bets)
	dst.Committed = copyMapString(src.Committed)
	dst.Revealed = copyMapString(src.Revealed)
	dst.HoleCards = copyMapCards(src.HoleCards)
	dst.Board = append([]Card(nil), src.Board...)
	dst.RoundActed = copyMapBool(src.RoundActed)
	dst.Deck = append([]Card(nil), src.Deck...)
	return &dst
}

func copyMapInt64(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyMapBool(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyMapString(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyMapCards(in map[string][]Card) map[string][]Card {
	out := make(map[string][]Card, len(in))
	for k, v := range in {
		out[k] = append([]Card(nil), v...)
	}
	return out
}

func evt(typ, player string, data map[string]any, at time.Time) runtime.Event {
	return runtime.Event{Type: typ, PlayerID: player, Data: data, At: at}
}

func int64FromParams(params map[string]any, key string, fallback int64) int64 {
	if params == nil {
		return fallback
	}
	v, ok := params[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return fallback
	}
}

func stringData(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:])
}

func isBettingPhase(phase string) bool {
	switch phase {
	case PhasePreFlop, PhaseFlop, PhaseTurn, PhaseRiver:
		return true
	default:
		return false
	}
}
