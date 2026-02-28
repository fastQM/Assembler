package social

import (
	"bytes"
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type stateStore interface {
	Load() (persistedState, bool, error)
	Save(state persistedState) error
}

type fileStateStore struct {
	mu   sync.Mutex
	path string
}

func newStateStore(path string) (stateStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &fileStateStore{path: path}, nil
}

func (s *fileStateStore) Load() (persistedState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := persistedState{
		KnownUsers:      map[string]KnownUser{},
		Requests:        map[string]FriendRequest{},
		Friends:         map[string]Friend{},
		DMs:             map[string][]DirectMessage{},
		UsedInviteNonce: map[string]time.Time{},
		Cursors:         map[string]int64{},
		SeenMessageIDs:  map[string]struct{}{},
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, false, nil
		}
		return out, false, err
	}
	dec := gob.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&out); err != nil {
		return out, false, err
	}
	if out.KnownUsers == nil {
		out.KnownUsers = map[string]KnownUser{}
	}
	if out.Requests == nil {
		out.Requests = map[string]FriendRequest{}
	}
	if out.Friends == nil {
		out.Friends = map[string]Friend{}
	}
	if out.DMs == nil {
		out.DMs = map[string][]DirectMessage{}
	}
	if out.UsedInviteNonce == nil {
		out.UsedInviteNonce = map[string]time.Time{}
	}
	if out.Cursors == nil {
		out.Cursors = map[string]int64{}
	}
	if out.SeenMessageIDs == nil {
		out.SeenMessageIDs = map[string]struct{}{}
	}
	return out, true, nil
}

func (s *fileStateStore) Save(state persistedState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(state); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
