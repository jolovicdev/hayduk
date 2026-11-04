// Package store persists the parts of a campaign that must outlive the
// process: the event/audit log, the module-rank cache, the listener registry
// and saved connection profiles.
//
// Everything is plain files under one directory (JSONL for the append-only
// event log, atomically replaced JSON for the small snapshots). That keeps the
// single-binary, CGO-free deployment intact and makes the state inspectable
// with ordinary tools.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

const (
	eventsFileName    = "events.jsonl"
	rotatedEventsName = "events.jsonl.1"
	ranksFileName     = "ranks.json"
	listenersFileName = "listeners.json"
	profilesFileName  = "profiles.json"

	// maxProfiles bounds the saved-connection list; the newest wins.
	maxProfiles = 10
)

// Store is a directory-backed persistence handle. All methods are safe for
// concurrent use; the event log appends asynchronously so request handlers
// never wait on a disk flush.
type Store struct {
	dir    string
	events *eventLog

	mu sync.Mutex // guards the JSON snapshots and the directory
}

// Open creates the data directory if needed and starts the event writer.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty data directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create %s: %w", dir, err)
	}
	events, err := openEventLog(filepath.Join(dir, eventsFileName))
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir, events: events}, nil
}

// Dir reports the directory backing the store.
func (s *Store) Dir() string { return s.dir }

// Close flushes and stops the event writer. The store is unusable after it.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return s.events.close()
}

// AppendEvent queues one audit entry. It never blocks: a stalled disk must
// not stall the engine's broadcast path. Events beyond the writer's queue are
// dropped and counted rather than waited for.
func (s *Store) AppendEvent(e *protocol.EventEntry) {
	if s == nil || e == nil {
		return
	}
	s.events.append(e)
}

// LoadEvents returns up to limit of the newest persisted events, oldest
// first. Malformed lines are skipped so a torn write does not poison the
// campaign history.
func (s *Store) LoadEvents(limit int) ([]*protocol.EventEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []*protocol.EventEntry
	for _, name := range []string{rotatedEventsName, eventsFileName} {
		entries, err := readEvents(filepath.Join(s.dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// Flush waits until every queued event reached the file. Tests use it; the
// engine relies on Close.
func (s *Store) Flush() error {
	if s == nil {
		return nil
	}
	return s.events.flushNow()
}

// --- small JSON snapshots -------------------------------------------------

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// SaveRanks replaces the rank cache. The map is copied so a concurrent
// engine mutation cannot race the encoder.
func (s *Store) SaveRanks(ranks map[string]string) error {
	if s == nil {
		return nil
	}
	copied := make(map[string]string, len(ranks))
	for k, v := range ranks {
		copied[k] = v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONAtomic(s.path(ranksFileName), copied)
}

// LoadRanks returns the cached ranks; an absent or corrupt cache is empty,
// never an error the engine has to handle mid-startup.
func (s *Store) LoadRanks() map[string]string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var ranks map[string]string
	if err := readJSON(s.path(ranksFileName), &ranks); err != nil {
		return nil
	}
	return ranks
}

// SaveListeners replaces the listener registry.
func (s *Store) SaveListeners(listeners []*protocol.ListenerState) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONAtomic(s.path(listenersFileName), listeners)
}

// LoadListeners returns the persisted registry, empty when absent.
func (s *Store) LoadListeners() []*protocol.ListenerState {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var listeners []*protocol.ListenerState
	if err := readJSON(s.path(listenersFileName), &listeners); err != nil {
		return nil
	}
	return listeners
}

// SaveProfile records a successful connection (never its password) and
// returns the resulting list. The newest entry leads and the list is capped.
func (s *Store) SaveProfile(p protocol.ProfilePayload) ([]protocol.ProfilePayload, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var profiles []protocol.ProfilePayload
	_ = readJSON(s.path(profilesFileName), &profiles)
	next := make([]protocol.ProfilePayload, 0, len(profiles)+1)
	next = append(next, p)
	for _, old := range profiles {
		if old.Host == p.Host && old.Port == p.Port && old.Username == p.Username && old.SSL == p.SSL {
			continue
		}
		next = append(next, old)
	}
	if len(next) > maxProfiles {
		next = next[:maxProfiles]
	}
	if err := writeJSONAtomic(s.path(profilesFileName), next); err != nil {
		return nil, err
	}
	return next, nil
}

// ListProfiles returns the saved connections, newest first.
func (s *Store) ListProfiles() []protocol.ProfilePayload {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var profiles []protocol.ProfilePayload
	if err := readJSON(s.path(profilesFileName), &profiles); err != nil {
		return nil
	}
	return profiles
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
