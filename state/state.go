package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.Mutex
	data Data
}

type Data struct {
	Sources  map[string]SourceState  `json:"sources,omitempty"`
	Failures map[string]FailureState `json:"failures,omitempty"`
}

type SourceState struct {
	UIDValidity      uint32 `json:"uid_validity"`
	LastProcessedUID uint32 `json:"last_processed_uid"`
	Initialized      bool   `json:"initialized"`
}

type FailureState struct {
	Attempts      int       `json:"attempts"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	LastError     string    `json:"last_error,omitempty"`
}

func Open(path string) (*Store, error) {
	s := &Store{
		path: path,
		data: Data{
			Sources:  make(map[string]SourceState),
			Failures: make(map[string]FailureState),
		},
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.data.Sources == nil {
		s.data.Sources = make(map[string]SourceState)
	}
	if s.data.Failures == nil {
		s.data.Failures = make(map[string]FailureState)
	}
	return s, nil
}

func SourceKey(source, mailbox string) string {
	return source + "|" + mailbox
}

func MessageKey(source, mailbox string, uidValidity, uid uint32) string {
	return fmt.Sprintf("%s|%s|%d|%d", source, mailbox, uidValidity, uid)
}

func (s *Store) GetSource(key string) (SourceState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data.Sources[key]
	return v, ok
}

func (s *Store) SetSource(key string, value SourceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Sources[key] = value
	return s.saveLocked()
}

func (s *Store) GetFailure(key string) (FailureState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data.Failures[key]
	return v, ok
}

func (s *Store) RecordFailure(key string, err error, backoff []time.Duration, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f := s.data.Failures[key]
	f.Attempts++
	f.LastError = err.Error()
	delay := retryDelay(f.Attempts, backoff)
	f.NextAttemptAt = now.Add(delay)
	s.data.Failures[key] = f
	return s.saveLocked()
}

func (s *Store) ClearFailure(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Failures, key)
	return s.saveLocked()
}

func retryDelay(attempts int, backoff []time.Duration) time.Duration {
	if len(backoff) == 0 {
		return 5 * time.Minute
	}
	idx := attempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(backoff) {
		idx = len(backoff) - 1
	}
	return backoff[idx]
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write state temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
