package localrpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type MessageRecord struct {
	ID        string            `json:"id"`
	Topic     string            `json:"topic"`
	AppID     string            `json:"app_id"`
	Payload   []byte            `json:"payload"`
	Headers   map[string]string `json:"headers,omitempty"`
	Source    string            `json:"source"`
	CreatedAt time.Time         `json:"created_at"`
	Offset    int64             `json:"offset"`
}

type historyStore struct {
	mu          sync.RWMutex
	recordsPath string
	cursorPath  string
	records     map[string][]MessageRecord
	nextOffset  map[string]int64
	cursors     map[string]int64
}

func newHistoryStore(recordsPath, cursorPath string) (*historyStore, error) {
	s := &historyStore{
		recordsPath: recordsPath,
		cursorPath:  cursorPath,
		records:     make(map[string][]MessageRecord),
		nextOffset:  make(map[string]int64),
		cursors:     make(map[string]int64),
	}
	if err := s.loadRecords(); err != nil {
		return nil, err
	}
	if err := s.loadCursors(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *historyStore) append(topic, appID string, payload []byte, headers map[string]string, source string) (MessageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	offset := s.nextOffset[topic] + 1
	s.nextOffset[topic] = offset

	rec := MessageRecord{
		ID:        fmt.Sprintf("%d-%d", time.Now().UnixNano(), offset),
		Topic:     topic,
		AppID:     appID,
		Payload:   append([]byte(nil), payload...),
		Headers:   cloneHeaders(headers),
		Source:    source,
		CreatedAt: time.Now().UTC(),
		Offset:    offset,
	}
	s.records[topic] = append(s.records[topic], rec)
	if err := s.appendRecordToDisk(rec); err != nil {
		return MessageRecord{}, err
	}
	return rec, nil
}

func (s *historyStore) list(topic string, fromOffset int64, limit int) []MessageRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	items := s.records[topic]
	out := make([]MessageRecord, 0, min(limit, len(items)))
	for _, rec := range items {
		if rec.Offset <= fromOffset {
			continue
		}
		out = append(out, rec)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *historyStore) saveCursor(appID, subscriptionID, topic string, offset int64) error {
	if appID == "" || subscriptionID == "" || topic == "" {
		return errors.New("cursor key fields must be non-empty")
	}
	key := cursorKey(appID, subscriptionID, topic)
	s.mu.Lock()
	s.cursors[key] = offset
	s.mu.Unlock()
	return s.persistCursors()
}

func (s *historyStore) getCursor(appID, subscriptionID, topic string) int64 {
	key := cursorKey(appID, subscriptionID, topic)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cursors[key]
}

func cursorKey(appID, subscriptionID, topic string) string {
	return appID + "|" + subscriptionID + "|" + topic
}

func (s *historyStore) loadRecords() error {
	f, err := os.Open(s.recordsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec MessageRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		s.records[rec.Topic] = append(s.records[rec.Topic], rec)
		if rec.Offset > s.nextOffset[rec.Topic] {
			s.nextOffset[rec.Topic] = rec.Offset
		}
	}
	return scan.Err()
}

func (s *historyStore) appendRecordToDisk(rec MessageRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.recordsPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.recordsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *historyStore) loadCursors() error {
	b, err := os.ReadFile(s.cursorPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	var cursors map[string]int64
	if err := json.Unmarshal(b, &cursors); err != nil {
		return nil
	}
	s.cursors = cursors
	return nil
}

func (s *historyStore) persistCursors() error {
	s.mu.RLock()
	copyMap := make(map[string]int64, len(s.cursors))
	for k, v := range s.cursors {
		copyMap[k] = v
	}
	s.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(s.cursorPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(copyMap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.cursorPath, b, 0o644)
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
